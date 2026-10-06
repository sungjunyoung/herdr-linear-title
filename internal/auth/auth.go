// Package auth provides Linear credentials: personal API keys and OAuth 2.0
// authorization code with PKCE, without a client secret.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunyoung/herdr-linear-title/internal/fileutil"
)

// Linear OAuth endpoints.
const (
	AuthorizeURL = "https://linear.app/oauth/authorize"
	TokenURL     = "https://api.linear.app/oauth/token"
	RevokeURL    = "https://api.linear.app/oauth/revoke"
)

const (
	tokenFile = "oauth.json"
	lockFile  = "oauth.lock"
	// refreshMargin refreshes access tokens shortly before they expire so a
	// token cannot expire between the check and the API call.
	refreshMargin = time.Minute
	loginTimeout  = 5 * time.Minute
)

// ErrNotLoggedIn means there is no usable OAuth token and the user has to log
// in again through setup.
var ErrNotLoggedIn = errors.New("not logged in to Linear")

// APIKey authenticates with a Linear personal API key.
type APIKey string

// Authorization returns the API key; Linear expects personal keys without a
// "Bearer" prefix.
func (k APIKey) Authorization(context.Context) (string, error) {
	return string(k), nil
}

// Token is a persisted OAuth token pair.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// OAuth authenticates with OAuth tokens stored in StateDir.
//
// Linear rotates refresh tokens on every refresh, so all token reads and
// refreshes happen under a file lock shared by concurrent plugin processes.
type OAuth struct {
	ClientID     string
	RedirectPort int
	StateDir     string

	HTTP         *http.Client
	AuthorizeURL string
	TokenURL     string
	RevokeURL    string
	Now          func() time.Time
}

// NewOAuth returns an OAuth source for the public Linear endpoints.
func NewOAuth(clientID string, redirectPort int, stateDir string) *OAuth {
	return &OAuth{
		ClientID:     clientID,
		RedirectPort: redirectPort,
		StateDir:     stateDir,
		HTTP:         &http.Client{Timeout: 15 * time.Second},
		AuthorizeURL: AuthorizeURL,
		TokenURL:     TokenURL,
		RevokeURL:    RevokeURL,
		Now:          time.Now,
	}
}

// RedirectURI is the callback URL registered in the Linear OAuth app.
func (o *OAuth) RedirectURI() string {
	return "http://localhost:" + strconv.Itoa(o.RedirectPort) + "/callback"
}

// Authorization returns a bearer header, refreshing the access token first
// when it is about to expire. It returns an error wrapping ErrNotLoggedIn when
// no token exists or Linear rejects the refresh token.
func (o *OAuth) Authorization(ctx context.Context) (string, error) {
	unlock, err := fileutil.Lock(filepath.Join(o.StateDir, lockFile))
	if err != nil {
		return "", err
	}
	defer unlock()

	tok, err := o.load()
	if err != nil {
		return "", err
	}
	if o.Now().Before(tok.ExpiresAt.Add(-refreshMargin)) {
		return "Bearer " + tok.AccessToken, nil
	}
	tok, err = o.requestToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {o.ClientID},
	})
	// Linear answers a revoked or expired refresh token with
	// 401 invalid_client; the remedy for any 4xx rejection is a new login.
	var te *tokenError
	if errors.As(err, &te) && (te.Status == http.StatusBadRequest || te.Status == http.StatusUnauthorized) {
		_ = os.Remove(o.tokenPath())
		return "", fmt.Errorf("%w: refresh token rejected: %w", ErrNotLoggedIn, te)
	}
	if err != nil {
		return "", fmt.Errorf("refresh Linear token: %w", err)
	}
	if err := o.save(tok); err != nil {
		return "", err
	}
	return "Bearer " + tok.AccessToken, nil
}

// Login runs the authorization code flow with PKCE. It listens on the
// redirect port, calls openBrowser with the authorization URL, waits for the
// callback, exchanges the code, and stores the token.
func (o *OAuth) Login(ctx context.Context, openBrowser func(authURL string)) error {
	verifier := randomString(32)
	challenge := sha256.Sum256([]byte(verifier))
	state := randomString(16)

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(o.RedirectPort))
	if err != nil {
		return fmt.Errorf("listen for OAuth callback: %w", err)
	}
	// The handler validates the callback itself so the browser gets a
	// meaningful page, then hands the outcome to Login.
	type callback struct {
		code string
		err  error
	}
	results := make(chan callback, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			var cb callback
			switch {
			case q.Get("error") != "":
				cb.err = fmt.Errorf("authorization denied: %s %s", q.Get("error"), q.Get("error_description"))
			case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1:
				cb.err = errors.New("OAuth state mismatch; discarding callback")
			case q.Get("code") == "":
				cb.err = errors.New("OAuth callback without code")
			default:
				cb.code = q.Get("code")
			}
			select {
			case results <- cb:
			default: // Only the first callback counts.
			}
			if cb.err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, "Linear Title: login failed: %v\n", cb.err)
				return
			}
			_, _ = io.WriteString(w, "Linear Title: authorization received. You can close this tab.\n")
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		// Graceful, so the callback response reaches the browser.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	authURL := o.AuthorizeURL + "?" + url.Values{
		"client_id":             {o.ClientID},
		"redirect_uri":          {o.RedirectURI()},
		"response_type":         {"code"},
		"scope":                 {"read"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}.Encode()
	openBrowser(authURL)

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	var cb callback
	select {
	case cb = <-results:
	case <-ctx.Done():
		return fmt.Errorf("waiting for OAuth callback: %w", ctx.Err())
	}
	if cb.err != nil {
		return cb.err
	}

	tok, err := o.requestToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {cb.code},
		"redirect_uri":  {o.RedirectURI()},
		"client_id":     {o.ClientID},
		"code_verifier": {verifier},
	})
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}

	unlock, err := fileutil.Lock(filepath.Join(o.StateDir, lockFile))
	if err != nil {
		return err
	}
	defer unlock()
	return o.save(tok)
}

// Logout revokes the stored refresh token and deletes it. It is a no-op when
// no token is stored.
func (o *OAuth) Logout(ctx context.Context) error {
	unlock, err := fileutil.Lock(filepath.Join(o.StateDir, lockFile))
	if err != nil {
		return err
	}
	defer unlock()

	tok, err := o.load()
	if errors.Is(err, ErrNotLoggedIn) {
		return nil
	}
	if err != nil {
		return err
	}
	form := url.Values{"token": {tok.RefreshToken}, "token_type_hint": {"refresh_token"}}
	resp, err := o.post(ctx, o.RevokeURL, form)
	if err != nil {
		return fmt.Errorf("revoke Linear token: %w", err)
	}
	_ = resp.Body.Close()
	// 400 means the token is already invalid; deleting it is still correct.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("revoke Linear token: HTTP %d", resp.StatusCode)
	}
	if err := os.Remove(o.tokenPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

type tokenError struct {
	Status      int
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *tokenError) Error() string {
	msg := "HTTP " + strconv.Itoa(e.Status)
	if e.Code != "" {
		msg += ": " + e.Code
	}
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}

func (o *OAuth) requestToken(ctx context.Context, form url.Values) (Token, error) {
	resp, err := o.post(ctx, o.TokenURL, form)
	if err != nil {
		return Token{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, err
	}
	if resp.StatusCode != http.StatusOK {
		te := &tokenError{Status: resp.StatusCode}
		_ = json.Unmarshal(raw, te)
		return Token{}, te
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Token{}, fmt.Errorf("decode token response: %w", err)
	}
	if body.AccessToken == "" || body.RefreshToken == "" {
		return Token{}, errors.New("token response is missing access_token or refresh_token")
	}
	return Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    o.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func (o *OAuth) post(ctx context.Context, endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return o.HTTP.Do(req)
}

func (o *OAuth) tokenPath() string {
	return filepath.Join(o.StateDir, tokenFile)
}

func (o *OAuth) load() (Token, error) {
	data, err := os.ReadFile(o.tokenPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Token{}, ErrNotLoggedIn
	}
	if err != nil {
		return Token{}, err
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return Token{}, fmt.Errorf("parse %s: %w", o.tokenPath(), err)
	}
	return tok, nil
}

func (o *OAuth) save(tok Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return fileutil.WriteAtomic(o.tokenPath(), data, 0o600)
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.
	return base64.RawURLEncoding.EncodeToString(b)
}
