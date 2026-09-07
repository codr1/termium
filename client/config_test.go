package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

func resetFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TERMIUM_HOMEPAGE", "")
	t.Helper()
	args, flags, usage := os.Args, flag.CommandLine, flag.Usage
	t.Cleanup(func() { os.Args, flag.CommandLine, flag.Usage = args, flags, usage })
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func TestParseFlagsDefaults(t *testing.T) {
	resetFlags(t)
	os.Args = []string{"termium"}

	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.Renderer != "auto" {
		t.Errorf("expected default renderer 'auto', got %q", cfg.Renderer)
	}
	if cfg.Palette != "websafe" {
		t.Errorf("expected default palette 'websafe', got %q", cfg.Palette)
	}
	if cfg.Debug {
		t.Error("expected debug=false by default")
	}
}

func TestLaunchURLShorthand(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
		bad  bool
	}{
		{[]string{"example.com"}, "https://example.com", false},
		{[]string{"--renderer", "sixel", "http://localhost:8080/path?q=a"}, "http://localhost:8080/path?q=a", false},
		{[]string{"--url", "example.com"}, "https://example.com", false},
		{[]string{"example.com", "second.com"}, "", true},
		{[]string{"--url", "example.com", "second.com"}, "", true},
		{[]string{"javascript:alert(1)"}, "", true},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			resetFlags(t)
			os.Args = append([]string{"termium"}, test.args...)
			config, err := parseFlags()
			if test.bad {
				if err == nil {
					t.Fatal("invalid launch accepted")
				}
				return
			}
			if err != nil || config.InitialURL != test.want {
				t.Fatalf("launch URL: %v %v", config, err)
			}
		})
	}
}

func TestParseFlagsKittyRenderer(t *testing.T) {
	resetFlags(t)
	os.Args = []string{"termium", "--renderer", "kitty"}

	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.Renderer != "kitty" {
		t.Errorf("expected renderer 'kitty', got %q", cfg.Renderer)
	}
}

func TestParseFlagsInvalidRenderer(t *testing.T) {
	resetFlags(t)
	os.Args = []string{"termium", "--renderer", "bogus"}

	_, err := parseFlags()
	if err == nil {
		t.Fatal("expected error for invalid renderer")
	}
}

func TestParseFlagsTcellBackwardCompat(t *testing.T) {
	resetFlags(t)
	os.Args = []string{"termium", "--tcell"}

	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.Renderer != "tcell" {
		t.Errorf("expected --tcell to set renderer to 'tcell', got %q", cfg.Renderer)
	}
}

func TestParseFlagsShortRenderer(t *testing.T) {
	resetFlags(t)
	os.Args = []string{"termium", "-r", "kitty"}

	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.Renderer != "kitty" {
		t.Errorf("expected renderer 'kitty' via -r, got %q", cfg.Renderer)
	}
}

func TestParseFlagsSupportedRenderers(t *testing.T) {
	for _, renderer := range []string{"auto", "sixel", "kitty", "tcell"} {
		t.Run(renderer, func(t *testing.T) {
			resetFlags(t)
			os.Args = []string{"termium", "--renderer", renderer, "--splash", "NONE"}
			config, err := parseFlags()
			if err != nil {
				t.Fatal(err)
			}
			if config.Renderer != renderer {
				t.Fatalf("got renderer %q", config.Renderer)
			}
		})
	}
}
