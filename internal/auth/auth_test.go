package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testCode = "code-1"

// tokenServer emulates the Linear token endpoint: every refresh rotates the
// refresh token and the previous one stops working, and authorization codes
// are only exchanged for the verifier matching the authorize challenge.
type tokenServer struct {
	mu        sync.Mutex
	current   string // valid refresh token
	challenge string // code_challenge of the pending authorization
	refreshes int
	lastForm  url.Values
}

func (s *tokenServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastForm = r.PostForm
	invalid := func() {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"rejected"}`)
	}
	switch r.PostForm.Get("grant_type") {
	case "refresh_token":
		if r.PostForm.Get("refresh_token") != s.current {
			// Observed from Linear for a revoked refresh token.
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid_client","error_description":"Invalid refresh token"}`)
			return
		}
		s.refreshes++
		s.current = fmt.Sprintf("refresh-%d", s.refreshes)
		_, _ = fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":%q,"expires_in":86399}`, s.refreshes, s.current)
	case "authorization_code":
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != testCode || base64.RawURLEncoding.EncodeToString(sum[:]) != s.challenge {
			invalid()
			return
		}
		s.current = "refresh-login"
		_, _ = io.WriteString(w, `{"access_token":"access-login","refresh_token":"refresh-login","expires_in":86399}`)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func newTestOAuth(t *testing.T, ts *tokenServer) *OAuth {
	t.Helper()
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)
	o := NewOAuth("client", freePort(t), t.TempDir())
	o.TokenURL = srv.URL
	o.RevokeURL = srv.URL
	o.AuthorizeURL = "https://linear.invalid/oauth/authorize"
	return o
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func tokenExists(t *testing.T, o *OAuth) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(o.StateDir, tokenFile))
	return err == nil
}

func TestAuthorizationNotLoggedIn(t *testing.T) {
	o := newTestOAuth(t, &tokenServer{})
	if _, err := o.Authorization(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Authorization() error = %v, want ErrNotLoggedIn", err)
	}
}

func TestAuthorizationUsesValidToken(t *testing.T) {
	ts := &tokenServer{}
	o := newTestOAuth(t, ts)
	if err := o.save(Token{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got, err := o.Authorization(context.Background())
	if err != nil || got != "Bearer a" {
		t.Fatalf("Authorization() = %q, %v", got, err)
	}
	if ts.refreshes != 0 {
		t.Error("refreshed a token that was still valid")
	}
}

// Concurrent hooks share one token; only one of them may spend the rotating
// refresh token, the others must reuse its result.
func TestAuthorizationRefreshesOnceConcurrently(t *testing.T) {
	ts := &tokenServer{current: "r0"}
	o := newTestOAuth(t, ts)
	if err := o.save(Token{AccessToken: "old", RefreshToken: "r0", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() { results[i], errs[i] = o.Authorization(context.Background()) })
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil || results[i] != "Bearer access-1" {
			t.Errorf("call %d: %q, %v", i, results[i], errs[i])
		}
	}
	if ts.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", ts.refreshes)
	}
	if ts.lastForm.Get("client_id") != "client" || ts.lastForm.Has("client_secret") {
		t.Errorf("refresh form = %v, want client_id without client_secret", ts.lastForm)
	}
	tok, err := o.load()
	if err != nil || tok.RefreshToken != "refresh-1" {
		t.Errorf("stored token = %+v, %v; want rotated refresh token", tok, err)
	}
}

func TestAuthorizationRejectedRefreshRequiresLogin(t *testing.T) {
	o := newTestOAuth(t, &tokenServer{current: "other"})
	if err := o.save(Token{AccessToken: "a", RefreshToken: "revoked", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Authorization(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Authorization() error = %v, want ErrNotLoggedIn", err)
	}
	if tokenExists(t, o) {
		t.Error("rejected token file still exists")
	}
}

// approve returns an openBrowser func that plays the user approving access:
// Linear redirects the browser to the callback with a code and the state.
func approve(t *testing.T, o *OAuth, ts *tokenServer, mutate func(url.Values)) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != o.RedirectURI() || q.Get("scope") != "read" {
			t.Errorf("authorize query = %v", q)
		}
		ts.mu.Lock()
		ts.challenge = q.Get("code_challenge")
		ts.mu.Unlock()

		cb := url.Values{"code": {testCode}, "state": {q.Get("state")}}
		if mutate != nil {
			mutate(cb)
		}
		go func() {
			resp, err := http.Get(o.RedirectURI() + "?" + cb.Encode())
			if err != nil {
				t.Error(err)
				return
			}
			_ = resp.Body.Close()
		}()
	}
}

func TestLogin(t *testing.T) {
	ts := &tokenServer{}
	o := newTestOAuth(t, ts)
	if err := o.Login(context.Background(), approve(t, o, ts, nil)); err != nil {
		t.Fatal(err)
	}
	if ts.lastForm.Has("client_secret") {
		t.Error("PKCE exchange sent a client_secret")
	}
	info, err := os.Stat(filepath.Join(o.StateDir, tokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file permissions = %o, want 600", perm)
	}
	got, err := o.Authorization(context.Background())
	if err != nil || got != "Bearer access-login" {
		t.Errorf("Authorization() after login = %q, %v", got, err)
	}
}

func TestLoginRejectsCallback(t *testing.T) {
	tests := map[string]func(url.Values){
		"state mismatch": func(q url.Values) { q.Set("state", "forged") },
		"denied":         func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			ts := &tokenServer{}
			o := newTestOAuth(t, ts)
			if err := o.Login(context.Background(), approve(t, o, ts, mutate)); err == nil {
				t.Fatal("Login() succeeded, want error")
			}
			if tokenExists(t, o) {
				t.Error("token stored after rejected callback")
			}
		})
	}
}

func TestLoginPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	o := NewOAuth("client", ln.Addr().(*net.TCPAddr).Port, t.TempDir())
	if err := o.Login(context.Background(), func(string) { t.Error("browser opened despite listen failure") }); err == nil {
		t.Fatal("Login() succeeded, want listen error")
	}
}

func TestLogout(t *testing.T) {
	ts := &tokenServer{}
	o := newTestOAuth(t, ts)
	if err := o.save(Token{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// The fake server answers revoke requests with 400, like Linear does for
	// an already invalid token; the local token must still be removed.
	if err := o.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ts.lastForm.Get("token") != "r" {
		t.Errorf("revoke form = %v, want refresh token", ts.lastForm)
	}
	if tokenExists(t, o) {
		t.Error("token file still exists after logout")
	}
	if err := o.Logout(context.Background()); err != nil {
		t.Errorf("second Logout() = %v, want no-op", err)
	}
}
