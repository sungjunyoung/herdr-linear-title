// Command herdr-linear-title is a herdr plugin that renames worktree
// workspaces to "<ISSUE-ID> <Linear issue title>".
//
// herdr runs it with a subcommand from herdr-plugin.toml and injects the
// plugin environment (HERDR_BIN_PATH, HERDR_PLUGIN_CONFIG_DIR, ...). The
// Herdr desktop app's mod runs "resolve" without that environment, so the
// config and state directories fall back to the ones herdr gives the plugin.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sungjunyoung/herdr-linear-title/internal/auth"
	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/config"
	"github.com/sungjunyoung/herdr-linear-title/internal/herdr"
	"github.com/sungjunyoung/herdr-linear-title/internal/labels"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
	"github.com/sungjunyoung/herdr-linear-title/internal/retitle"
)

const (
	notifyTitle     = "Linear Title"
	defaultPluginID = "sungjunyoung.linear-title"
	usage           = "usage: herdr-linear-title hook|refresh|refresh-all|open-setup|setup|logout|resolve <branch>"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-linear-title:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 2 && args[0] == "resolve" {
		a, err := newApp(os.Stderr)
		if err != nil {
			return err
		}
		return a.resolve(ctx, os.Stdout, args[1])
	}
	if len(args) != 1 {
		return errors.New(usage)
	}
	a, err := newApp(os.Stderr)
	if err != nil {
		return err
	}
	switch args[0] {
	case "hook", "refresh", "refresh-all", "open-setup":
		if a.herdr == nil {
			return fmt.Errorf("%s must be run by herdr as a plugin action (HERDR_BIN_PATH is not set)", args[0])
		}
	}
	switch args[0] {
	case "hook":
		return a.reportErr(ctx, a.hook(ctx, os.Getenv("HERDR_PLUGIN_EVENT_JSON")))
	case "refresh":
		return a.reportErr(ctx, a.refresh(ctx, os.Getenv("HERDR_WORKSPACE_ID")))
	case "refresh-all":
		return a.reportErr(ctx, a.refreshAll(ctx))
	case "open-setup":
		return a.reportErr(ctx, a.herdr.OpenPluginPane(ctx, a.pluginID, "setup"))
	case "setup":
		return a.setup(ctx, os.Stdin, os.Stdout)
	case "logout":
		return a.reportErr(ctx, a.logout(ctx))
	default:
		return fmt.Errorf("unknown subcommand %q; %s", args[0], usage)
	}
}

// app holds the plugin runtime environment injected by herdr.
type app struct {
	// herdr is nil outside herdr (the desktop app's mod, a shell).
	herdr     *herdr.Client
	pluginID  string
	configDir string
	stateDir  string
	log       io.Writer
}

func newApp(log io.Writer) (*app, error) {
	a := &app{
		pluginID:  cmp.Or(os.Getenv("HERDR_PLUGIN_ID"), defaultPluginID),
		configDir: os.Getenv("HERDR_PLUGIN_CONFIG_DIR"),
		stateDir:  os.Getenv("HERDR_PLUGIN_STATE_DIR"),
		log:       log,
	}
	if bin := os.Getenv("HERDR_BIN_PATH"); bin != "" {
		a.herdr = &herdr.Client{Bin: bin}
	}
	if a.configDir == "" || a.stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		// Same layout herdr uses for plugin dirs, so one login serves both.
		if a.configDir == "" {
			base := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
			a.configDir = filepath.Join(base, "herdr", "plugins", "config", a.pluginID)
		}
		if a.stateDir == "" {
			base := cmp.Or(os.Getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state"))
			a.stateDir = filepath.Join(base, "herdr", "plugins", a.pluginID)
		}
	}
	return a, nil
}

// runtime is everything needed to retitle workspaces with a loaded config.
type runtime struct {
	cfg      config.Config
	linear   *linear.Client
	retitler *retitle.Retitler
}

func (a *app) load() (*runtime, error) {
	cfg, err := config.Load(a.configDir)
	if err != nil {
		return nil, err
	}
	matcher, err := branch.NewMatcher(cfg.TeamKeys)
	if err != nil {
		return nil, err
	}
	return &runtime{
		cfg:    cfg,
		linear: linear.New(a.authorizer(cfg)),
		retitler: &retitle.Retitler{
			Workspaces: a.herdr,
			Matcher:    matcher,
			Labels:     labels.Store{Dir: a.stateDir},
			Format:     cfg.Title,
		},
	}, nil
}

func (a *app) authorizer(cfg config.Config) linear.Authorizer {
	if cfg.Auth.Method == config.AuthAPIKey {
		return auth.APIKey(cfg.Auth.APIKey)
	}
	return a.oauth(cfg)
}

func (a *app) oauth(cfg config.Config) *auth.OAuth {
	return auth.NewOAuth(cfg.Auth.OAuthClientID, cfg.Auth.OAuthRedirectPort, a.stateDir)
}

// notify shows msg as a herdr notification. herdr drops notifications when no
// client is attached, so msg is always written to the plugin log as well.
func (a *app) notify(ctx context.Context, msg string) {
	fmt.Fprintln(a.log, msg)
	if a.herdr == nil {
		return
	}
	if err := a.herdr.Notify(ctx, notifyTitle, msg); err != nil {
		fmt.Fprintln(a.log, "notification failed:", err)
	}
}

// reportErr notifies the user about err and returns it so the command exits
// non-zero and herdr records the failure in the plugin log.
func (a *app) reportErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	a.notify(ctx, userMessage(err))
	return err
}

// needsSetup reports whether err is fixed by running setup again.
func needsSetup(err error) bool {
	return errors.Is(err, config.ErrNotConfigured) || errors.Is(err, auth.ErrNotLoggedIn) || errors.Is(err, linear.ErrUnauthorized)
}

func userMessage(err error) string {
	const setupHint = " Run setup (\"Linear: setup / login\" in herdr, or herdr-linear-title setup)."
	switch {
	case errors.Is(err, config.ErrNotConfigured):
		return "Not configured." + setupHint
	case errors.Is(err, auth.ErrNotLoggedIn):
		return "Not logged in to Linear." + setupHint
	case errors.Is(err, linear.ErrUnauthorized):
		return "Linear rejected the credentials." + setupHint
	default:
		return err.Error()
	}
}
