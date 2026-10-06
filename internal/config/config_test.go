package config

import (
	"errors"
	"os"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load() error = %v, want ErrNotConfigured", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := Config{
		TeamKeys: []string{"HOMECO"},
		Auth:     Auth{Method: AuthOAuth, OAuthClientID: "client"},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %o, want 600", perm)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.TitleFormat != DefaultTitleFormat {
		t.Errorf("TitleFormat = %q, want default", got.TitleFormat)
	}
	if got.Auth.OAuthRedirectPort != DefaultRedirectPort {
		t.Errorf("OAuthRedirectPort = %d, want default", got.Auth.OAuthRedirectPort)
	}
	if got.Auth.OAuthClientID != "client" || got.TeamKeys[0] != "HOMECO" {
		t.Errorf("round trip lost values: %+v", got)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"unknown key":       "team_keys = [\"A\"]\nteam_key = \"A\"\n[auth]\nmethod = \"api_key\"\napi_key = \"k\"\n",
		"no team keys":      "team_keys = []\n[auth]\nmethod = \"api_key\"\napi_key = \"k\"\n",
		"bad team key":      "team_keys = [\"HOME-CO\"]\n[auth]\nmethod = \"api_key\"\napi_key = \"k\"\n",
		"missing api key":   "team_keys = [\"A\"]\n[auth]\nmethod = \"api_key\"\n",
		"missing client":    "team_keys = [\"A\"]\n[auth]\nmethod = \"oauth\"\n",
		"bad port":          "team_keys = [\"A\"]\n[auth]\nmethod = \"oauth\"\noauth_client_id = \"c\"\noauth_redirect_port = 70000\n",
		"unknown method":    "team_keys = [\"A\"]\n[auth]\nmethod = \"password\"\n",
		"malformed toml":    "team_keys = [\n",
		"no auth section":   "team_keys = [\"A\"]\n",
		"empty api key str": "team_keys = [\"A\"]\n[auth]\nmethod = \"api_key\"\napi_key = \"\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(Path(dir), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir); err == nil || errors.Is(err, ErrNotConfigured) {
				t.Fatalf("Load() error = %v, want validation error", err)
			}
		})
	}
}

func TestTitle(t *testing.T) {
	c := Config{TitleFormat: DefaultTitleFormat}
	got := c.Title("HOMECO-1", "  상담 신청\n화면   개선 ")
	if want := "HOMECO-1 상담 신청 화면 개선"; got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}

	c.TitleFormat = "[{identifier}] {title}"
	if got := c.Title("ENG-2", "Fix"); got != "[ENG-2] Fix" {
		t.Errorf("custom Title() = %q", got)
	}
	if got := c.Title("ENG-2", "{identifier}"); got != "[ENG-2] {identifier}" {
		t.Errorf("placeholders inside the issue title must not be expanded, got %q", got)
	}
}
