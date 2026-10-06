// Package config loads and saves the plugin's config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/sungjunyoung/herdr-linear-title/internal/fileutil"
)

const (
	// FileName is the config file name inside HERDR_PLUGIN_CONFIG_DIR.
	FileName = "config.toml"
	// DefaultTitleFormat renders "HOMECO-2290 Issue title".
	DefaultTitleFormat = "{identifier} {title}"
	// DefaultRedirectPort is the default local OAuth callback port.
	DefaultRedirectPort = 53682
)

// AuthMethod selects how the plugin authenticates to Linear.
type AuthMethod string

// Supported authentication methods.
const (
	AuthOAuth  AuthMethod = "oauth"
	AuthAPIKey AuthMethod = "api_key"
)

// ErrNotConfigured is returned by Load when no config file exists.
var ErrNotConfigured = errors.New("linear title is not configured")

var teamKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// Config is the content of config.toml.
type Config struct {
	TeamKeys    []string `toml:"team_keys"`
	TitleFormat string   `toml:"title_format"`
	Auth        Auth     `toml:"auth"`
}

// Auth holds Linear credentials settings.
type Auth struct {
	Method            AuthMethod `toml:"method"`
	APIKey            string     `toml:"api_key,omitempty"`
	OAuthClientID     string     `toml:"oauth_client_id,omitempty"`
	OAuthRedirectPort int        `toml:"oauth_redirect_port,omitempty"`
}

// Path returns the config file path inside dir.
func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

// Load reads and validates the config in dir. It returns an error wrapping
// ErrNotConfigured when the file does not exist.
func Load(dir string) (Config, error) {
	path := Path(dir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("%w (%s missing)", ErrNotConfigured, path)
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Save validates c and writes it to dir with owner-only permissions, since it
// may contain an API key.
func Save(dir string, c Config) error {
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return fileutil.WriteAtomic(Path(dir), data, 0o600)
}

func (c *Config) applyDefaults() {
	if c.TitleFormat == "" {
		c.TitleFormat = DefaultTitleFormat
	}
	if c.Auth.Method == AuthOAuth && c.Auth.OAuthRedirectPort == 0 {
		c.Auth.OAuthRedirectPort = DefaultRedirectPort
	}
}

// Validate reports the first invalid setting.
func (c Config) Validate() error {
	if len(c.TeamKeys) == 0 {
		return errors.New("team_keys must not be empty")
	}
	for _, k := range c.TeamKeys {
		if !ValidTeamKey(k) {
			return fmt.Errorf("invalid team key %q: use letters and digits, starting with a letter", k)
		}
	}
	switch c.Auth.Method {
	case AuthAPIKey:
		if c.Auth.APIKey == "" {
			return errors.New("auth.api_key is required when auth.method = \"api_key\"")
		}
	case AuthOAuth:
		if c.Auth.OAuthClientID == "" {
			return errors.New("auth.oauth_client_id is required when auth.method = \"oauth\"")
		}
		if p := c.Auth.OAuthRedirectPort; p < 1 || p > 65535 {
			return fmt.Errorf("auth.oauth_redirect_port %d out of range", p)
		}
	default:
		return fmt.Errorf("auth.method must be %q or %q, got %q", AuthOAuth, AuthAPIKey, c.Auth.Method)
	}
	return nil
}

// ValidTeamKey reports whether k looks like a Linear team key.
func ValidTeamKey(k string) bool {
	return teamKeyPattern.MatchString(k)
}

// Title renders the workspace label for an issue. Runs of whitespace in the
// issue title are collapsed so the label stays on one line.
func (c Config) Title(identifier, title string) string {
	title = strings.Join(strings.Fields(title), " ")
	r := strings.NewReplacer("{identifier}", identifier, "{title}", title)
	return strings.TrimSpace(r.Replace(c.TitleFormat))
}
