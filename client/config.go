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
}

func parseFlags() (*Config, error) {
    cfg := &Config{}
    
    // Define flags
    flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug output")
    flag.StringVar(&cfg.ServerAddr, "server", "localhost:50051", "Server address (ip:port)")
    flag.StringVar(&cfg.SplashPath, "splash", "./ship.jpg", "Path to splash screen image")
    
    // Handle both --flag and -flag formats
    flag.BoolVar(&cfg.Debug, "d", false, "Enable debug output (shorthand)")
    flag.StringVar(&cfg.ServerAddr, "s", "localhost:50051", "Server address (shorthand)")
    
    // Custom usage message
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
        fmt.Fprintf(os.Stderr, "\nFlags:\n")
        flag.PrintDefaults()
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
