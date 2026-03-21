package main

import (
	"flag"
	"os"
	"testing"
)

func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func TestParseFlagsDefaults(t *testing.T) {
	resetFlags()
	os.Args = []string{"termium"}

	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.Renderer != "auto" {
		t.Errorf("expected default renderer 'auto', got %q", cfg.Renderer)
	}
	if cfg.Palette != "adaptive" {
		t.Errorf("expected default palette 'adaptive', got %q", cfg.Palette)
	}
	if cfg.Debug {
		t.Error("expected debug=false by default")
	}
}

func TestParseFlagsKittyRenderer(t *testing.T) {
	resetFlags()
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
	resetFlags()
	os.Args = []string{"termium", "--renderer", "bogus"}

	_, err := parseFlags()
	if err == nil {
		t.Fatal("expected error for invalid renderer")
	}
}

func TestParseFlagsTcellBackwardCompat(t *testing.T) {
	resetFlags()
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
	resetFlags()
	os.Args = []string{"termium", "-r", "kitty"}

	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.Renderer != "kitty" {
		t.Errorf("expected renderer 'kitty' via -r, got %q", cfg.Renderer)
	}
}
