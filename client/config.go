package main

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	Debug      bool
	ServerAddr string
	SplashPath string
	LogFile    string
}

func parseFlags() (*Config, error) {
	cfg := &Config{}

	// Define flags
	flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug output")
	flag.StringVar(&cfg.ServerAddr, "server", "localhost:50051", "Server address (ip:port)")
	flag.StringVar(&cfg.SplashPath, "splash", "./ship.jpg", "Path to splash screen image")
	flag.StringVar(&cfg.LogFile, "logfile", "", "Path to log file (optional, if not specified logs only go to console)")

	// Handle both --flag and -flag formats
	flag.BoolVar(&cfg.Debug, "d", false, "Enable debug output (shorthand)")
	flag.StringVar(&cfg.ServerAddr, "s", "localhost:50051", "Server address (shorthand)")
	flag.StringVar(&cfg.LogFile, "l", "", "Path to log file (shorthand)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
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

	flag.Parse()

	// Validate server address format
	if cfg.ServerAddr != "" {
		// TODO: Add validation for ip:port format
	}

	// Check if splash image exists
	if _, err := os.Stat(cfg.SplashPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("splash image not found: %s", cfg.SplashPath)
	}

	return cfg, nil
}
