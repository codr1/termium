package main

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	InitialURL      string
	Homepage        string
	SetHomepage     string
	Debug           bool
	ServerAddr      string
	SplashPath      string
	LogFile         string
	UseTCell        bool
	SaveScreenshots bool
	CPUProfile      string
	TraceProfile    string
	ShowTimings     bool
	Doctor          bool
	InstallBundle   bool
	FirstRun        bool
	Palette         string
	Renderer        string // "sixel" (default), "kitty", or "tcell"
}

func parseFlags() (*Config, error) {
	cfg := &Config{}

	showVersion := false
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&showVersion, "v", false, "Print version and exit (shorthand)")
	flag.BoolVar(&cfg.Doctor, "doctor", false, "Check the runtime and browser without opening the terminal UI")
	flag.BoolVar(&cfg.InstallBundle, "install-bundle", false, "Install this verified release bundle for the current user")
	flag.BoolVar(&cfg.FirstRun, "first-run", false, "Open after installation when attached to a terminal")

	flag.StringVar(&cfg.InitialURL, "url", "", "Open an address instead of your home page")
	flag.StringVar(&cfg.Homepage, "homepage", "", "Home page for this session (URL, about:termium, or about:blank)")
	flag.StringVar(&cfg.SetHomepage, "set-homepage", "", "Save a home page and exit (URL, default, about:termium, or about:blank)")
	// Define flags
	flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug output")
	flag.StringVar(&cfg.ServerAddr, "tcp", "", "Use TCP connection (default: Unix socket at /tmp/termium.sock, with --tcp defaults to localhost:50051)")
	flag.StringVar(&cfg.SplashPath, "splash", "", "Path to custom splash screen image or NONE to skip splash screen")
	flag.StringVar(&cfg.LogFile, "logfile", "", "Path to log file (optional, if not specified logs only go to console)")
	flag.BoolVar(&cfg.UseTCell, "tcell", false, "Use tcell renderer (deprecated: use --renderer tcell)")
	flag.BoolVar(&cfg.SaveScreenshots, "save-screenshots", false, "Save debug screenshots to disk (impacts performance)")
	flag.StringVar(&cfg.CPUProfile, "cpuprofile", "", "Write CPU profile to file")
	flag.StringVar(&cfg.TraceProfile, "trace", "", "Write execution trace to file")
	flag.BoolVar(&cfg.ShowTimings, "timings", false, "Show timing measurements for each frame")
	flag.StringVar(&cfg.Palette, "palette", "websafe", "Color palette: adaptive, websafe, plan9")
	flag.StringVar(&cfg.Palette, "p", "websafe", "Color palette: adaptive, websafe, plan9 (short form)")
	flag.StringVar(&cfg.Renderer, "renderer", "auto", "Rendering protocol: auto, sixel, kitty, tcell")
	flag.StringVar(&cfg.Renderer, "r", "auto", "Rendering protocol (short form)")

	// Handle both --flag and -flag formats
	flag.BoolVar(&cfg.Debug, "d", false, "Enable debug output (shorthand)")
	flag.StringVar(&cfg.ServerAddr, "s", "", "Server address (shorthand, only works with --tcp)")
	flag.StringVar(&cfg.LogFile, "l", "", "Path to log file (shorthand)")
	flag.BoolVar(&cfg.UseTCell, "t", false, "Use tcell renderer (shorthand)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: termium [options] [URL]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nDebug Levels:\n")
		fmt.Fprintf(os.Stderr, "  DEBUG: Detailed information for debugging\n")
		fmt.Fprintf(os.Stderr, "  INFO:  Normal operational messages\n")
		fmt.Fprintf(os.Stderr, "  WARN:  Warning messages for potentially harmful situations\n")
		fmt.Fprintf(os.Stderr, "  ERROR: Error messages for serious problems\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s --debug --logfile /var/log/termium.log\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -d -l /var/log/termium.log -s remote:50051\n", os.Args[0])
	}

	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	if showVersion {
		fmt.Printf("termium %s (%s)\n", version, commit)
		os.Exit(0)
	}
	urlFlag := false
	flag.Visit(func(f *flag.Flag) { urlFlag = urlFlag || f.Name == "url" })
	if flag.NArg() > 1 || (flag.NArg() == 1 && urlFlag) {
		return nil, fmt.Errorf("use one URL, either positional or --url; place options before the URL")
	}
	if flag.NArg() == 1 {
		cfg.InitialURL = flag.Arg(0)
	}
	if cfg.InitialURL != "" && cfg.InitialURL != "about:blank" {
		address, err := normalizeAddress(cfg.InitialURL)
		if err != nil {
			return nil, err
		}
		cfg.InitialURL = address
	}

	if cfg.SetHomepage != "" {
		value, err := normalizeHomepage(cfg.SetHomepage)
		if err != nil {
			return nil, err
		}
		cfg.SetHomepage = value
	}
	if !cfg.Doctor && !cfg.InstallBundle && cfg.SetHomepage == "" {
		value, err := resolveHomepage(cfg.Homepage)
		if err != nil {
			return nil, err
		}
		cfg.Homepage = value
	}

	// Validate server address format
	if cfg.ServerAddr != "" {
		// TODO: Add validation for ip:port format
	}

	// Backward compat: --tcell flag overrides renderer
	if cfg.UseTCell {
		cfg.Renderer = "tcell"
	}

	// Validate renderer
	switch cfg.Renderer {
	case "auto", "sixel", "kitty", "tcell":
		// valid
	default:
		return nil, fmt.Errorf("invalid renderer %q: must be auto, sixel, kitty, or tcell", cfg.Renderer)
	}

	// Check if splash image exists (only if specified and not NONE)
	if cfg.SplashPath != "" && cfg.SplashPath != "NONE" {
		if _, err := os.Stat(cfg.SplashPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("splash image not found: %s", cfg.SplashPath)
		}
	}

	return cfg, nil
}
