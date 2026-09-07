package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultHomepage = "https://termium.dev/welcome/"

func normalizeHomepage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "default" {
		return defaultHomepage, nil
	}
	if value == "about:termium" || value == "about:blank" {
		return value, nil
	}
	return normalizeAddress(value)
}
func homepageSettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "termium", "settings.json"), nil
}
func readHomepageSettings() (map[string]json.RawMessage, error) {
	p, err := homepageSettingsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	var settings map[string]json.RawMessage
	if len(data) > 64*1024 {
		return nil, fmt.Errorf("settings file exceeds 64 KiB: %s", p)
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("invalid settings in %s: %w", p, err)
	}
	if settings == nil {
		settings = map[string]json.RawMessage{}
	}
	return settings, nil
}
func resolveHomepage(flagValue string) (string, error) {
	value := flagValue
	if value == "" {
		value = os.Getenv("TERMIUM_HOMEPAGE")
	}
	if value == "" {
		settings, err := readHomepageSettings()
		if err != nil {
			return "", err
		}
		if data, ok := settings["homepage"]; ok {
			if err = json.Unmarshal(data, &value); err != nil {
				return "", fmt.Errorf("homepage setting must be a URL string")
			}
		}
	}
	if value == "" {
		value = defaultHomepage
	}
	return normalizeHomepage(value)
}
func saveHomepage(value string) error {
	value, err := normalizeHomepage(value)
	if err != nil {
		return err
	}
	settings, err := readHomepageSettings()
	if err != nil {
		return err
	}
	settings["homepage"], err = json.Marshal(value)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	p, err := homepageSettingsPath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
