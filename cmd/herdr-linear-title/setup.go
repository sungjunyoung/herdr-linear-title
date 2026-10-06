package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/sungjunyoung/herdr-linear-title/internal/auth"
	"github.com/sungjunyoung/herdr-linear-title/internal/browser"
	"github.com/sungjunyoung/herdr-linear-title/internal/config"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
)

// setup interactively writes config.toml, logs in, and verifies the
// credentials. It runs inside the herdr popup pane and waits for Enter before
// exiting so the result stays visible.
func (a *app) setup(ctx context.Context, in *os.File, out io.Writer) error {
	p := &prompter{in: bufio.NewReader(in), fd: int(in.Fd()), out: out}
	err := a.runSetup(ctx, p)
	if err != nil {
		fmt.Fprintf(out, "\nSetup failed: %v\n", err)
	}
	fmt.Fprint(out, "\nPress Enter to close.")
	_, _ = p.in.ReadString('\n')
	return err
}

func (a *app) runSetup(ctx context.Context, p *prompter) error {
	fmt.Fprintf(p.out, "Linear Title setup\nConfig: %s\n\n", config.Path(a.configDir))

	// Previous values are only defaults; a broken file is overwritten.
	prev, _ := config.Load(a.configDir)
	cfg := config.Config{TitleFormat: prev.TitleFormat}

	for {
		raw := p.line("Team keys, comma separated (e.g. HOMECO)", strings.Join(prev.TeamKeys, ","))
		cfg.TeamKeys = splitKeys(raw)
		if len(cfg.TeamKeys) > 0 && allValid(cfg.TeamKeys) {
			break
		}
		fmt.Fprintln(p.out, "  Enter one or more keys of letters and digits, starting with a letter.")
	}

	defMethod := "1"
	if prev.Auth.Method == config.AuthAPIKey {
		defMethod = "2"
	}
	switch p.line("Authentication: 1) OAuth  2) Personal API key", defMethod) {
	case "1":
		cfg.Auth.Method = config.AuthOAuth
	case "2":
		cfg.Auth.Method = config.AuthAPIKey
	default:
		return errors.New("choose 1 or 2")
	}

	var authorizer linear.Authorizer
	switch cfg.Auth.Method {
	case config.AuthOAuth:
		o, err := a.setupOAuth(ctx, p, prev, &cfg)
		if err != nil {
			return err
		}
		authorizer = o
	case config.AuthAPIKey:
		key := p.secret("Personal API key (Linear Settings > Security & access)", prev.Auth.APIKey)
		if key == "" {
			return errors.New("API key is required")
		}
		cfg.Auth.APIKey = key
		authorizer = auth.APIKey(key)
	}

	name, err := linear.New(authorizer).Viewer(ctx)
	if err != nil {
		return fmt.Errorf("verify credentials: %w", err)
	}
	if err := config.Save(a.configDir, cfg); err != nil {
		return err
	}
	fmt.Fprintf(p.out, "\nLogged in to Linear as %s. Saved %s\n", name, config.Path(a.configDir))
	return nil
}

func (a *app) setupOAuth(ctx context.Context, p *prompter, prev config.Config, cfg *config.Config) (*auth.OAuth, error) {
	cfg.Auth.OAuthClientID = p.line("OAuth client ID", prev.Auth.OAuthClientID)
	if cfg.Auth.OAuthClientID == "" {
		return nil, errors.New("OAuth client ID is required")
	}
	defPort := prev.Auth.OAuthRedirectPort
	if defPort == 0 {
		defPort = config.DefaultRedirectPort
	}
	port, err := strconv.Atoi(p.line("Callback port", strconv.Itoa(defPort)))
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("callback port must be a number between 1 and 65535")
	}
	cfg.Auth.OAuthRedirectPort = port

	o := a.oauth(*cfg)
	fmt.Fprintf(p.out, "\nThe Linear OAuth app must allow the callback URL %s\n", o.RedirectURI())
	err = o.Login(ctx, func(authURL string) {
		fmt.Fprintf(p.out, "Opening the browser. If it does not open, visit:\n\n%s\n\nWaiting for authorization...\n", authURL)
		if err := browser.Open(authURL); err != nil {
			fmt.Fprintf(p.out, "(could not open a browser: %v)\n", err)
		}
	})
	if err != nil {
		return nil, err
	}
	return o, nil
}

// logout revokes and deletes the stored OAuth token.
func (a *app) logout(ctx context.Context) error {
	cfg, err := config.Load(a.configDir)
	if err != nil {
		return err
	}
	if cfg.Auth.Method != config.AuthOAuth {
		a.notify(ctx, "Using a personal API key; nothing to log out. Change it with setup.")
		return nil
	}
	if err := a.oauth(cfg).Logout(ctx); err != nil {
		return err
	}
	a.notify(ctx, "Logged out of Linear.")
	return nil
}

type prompter struct {
	in  *bufio.Reader
	fd  int
	out io.Writer
}

// line prints label with a default and returns the trimmed answer, or def
// when the answer is empty.
func (p *prompter) line(label, def string) string {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", label)
	}
	s, _ := p.in.ReadString('\n')
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

// secret reads without echo when stdin is a terminal. An empty answer keeps
// def, which is never printed.
func (p *prompter) secret(label, def string) string {
	if def != "" {
		label += " [keep current]"
	}
	fmt.Fprintf(p.out, "%s: ", label)
	var s string
	if term.IsTerminal(p.fd) {
		b, err := term.ReadPassword(p.fd)
		fmt.Fprintln(p.out)
		if err == nil {
			s = string(b)
		}
	} else {
		s, _ = p.in.ReadString('\n')
	}
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

func splitKeys(raw string) []string {
	var keys []string
	for _, k := range strings.Split(raw, ",") {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func allValid(keys []string) bool {
	for _, k := range keys {
		if !config.ValidTeamKey(k) {
			return false
		}
	}
	return true
}
