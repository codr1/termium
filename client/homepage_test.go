package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHomepagePersistenceAndPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TERMIUM_HOMEPAGE", "")
	if got, err := resolveHomepage(""); err != nil || got != defaultHomepage {
		t.Fatalf("default: %q %v", got, err)
	}
	p, err := homepageSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, []byte(`{"future_setting":{"enabled":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = saveHomepage("example.com/start"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveHomepage(""); err != nil || got != "https://example.com/start" {
		t.Fatalf("saved: %q %v", got, err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err = json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	var future struct {
		Enabled bool `json:"enabled"`
	}
	if err = json.Unmarshal(settings["future_setting"], &future); err != nil || !future.Enabled {
		t.Fatalf("unrelated setting was lost: %s", data)
	}
	t.Setenv("TERMIUM_HOMEPAGE", "https://env.example/")
	if got, err := resolveHomepage(""); err != nil || got != "https://env.example/" {
		t.Fatalf("env: %q %v", got, err)
	}
	if got, err := resolveHomepage("about:blank"); err != nil || got != "about:blank" {
		t.Fatalf("flag: %q %v", got, err)
	}
	if err = saveHomepage("javascript:alert(1)"); err == nil {
		t.Fatal("unsafe home page saved")
	}
	after, err := os.ReadFile(p)
	if err != nil || string(after) != string(data) {
		t.Fatal("invalid setting changed saved preferences")
	}
	t.Setenv("TERMIUM_HOMEPAGE", "")
	if err = saveHomepage("default"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveHomepage(""); err != nil || got != defaultHomepage {
		t.Fatalf("reset: %q %v", got, err)
	}
}

func TestExplicitURLKeepsSavedNewTabHomepage(t *testing.T) {
	resetFlags(t)
	if err := saveHomepage("https://home.example/"); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"termium", "https://one-off.example/"}
	cfg, err := parseFlags()
	if err != nil || cfg.InitialURL != "https://one-off.example/" || cfg.Homepage != "https://home.example/" {
		t.Fatalf("one-off navigation changed home page: %+v %v", cfg, err)
	}
}

func TestSetHomepageRepairsInvalidSavedValue(t *testing.T) {
	resetFlags(t)
	p, err := homepageSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, []byte(`{"homepage":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMIUM_HOMEPAGE", "javascript:invalid")
	os.Args = []string{"termium", "--set-homepage", "default"}
	cfg, err := parseFlags()
	if err != nil {
		t.Fatal(err)
	}
	if err = saveHomepage(cfg.SetHomepage); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMIUM_HOMEPAGE", "")
	if got, err := resolveHomepage(""); err != nil || got != defaultHomepage {
		t.Fatalf("reset: %q %v", got, err)
	}
}
