package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func settingsPath(accountsPath string) string {
	return filepath.Join(filepath.Dir(accountsPath), "settings.json")
}

func loadSettings(accountsPath string) (appSettings, error) {
	settings := defaultAppSettings()
	content, err := os.ReadFile(settingsPath(accountsPath))
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return appSettings{}, err
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return appSettings{}, err
	}
	normalizeSettings(&settings)
	return settings, nil
}

func saveSettings(accountsPath string, settings appSettings) error {
	normalizeSettings(&settings)
	path := settingsPath(accountsPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func normalizeSettings(settings *appSettings) {
	if settings.UsageDisplay != "used" && settings.UsageDisplay != "remaining" {
		settings.UsageDisplay = "used"
	}
	if settings.BarFill != "left" && settings.BarFill != "right" {
		settings.BarFill = "left"
	}
	if settings.PercentagePosition != "left" && settings.PercentagePosition != "right" {
		settings.PercentagePosition = "right"
	}
	if !validColorTheme(settings.ColorTheme) {
		settings.ColorTheme = "default"
	}
	if !validAutoRefreshInterval(settings.AutoRefreshSeconds) {
		settings.AutoRefreshSeconds = 60
	}
}

func validColorTheme(theme string) bool {
	for _, candidate := range []string{"default", "colorblind", "monochrome"} {
		if theme == candidate {
			return true
		}
	}
	return false
}

func validAutoRefreshInterval(seconds int) bool {
	for _, candidate := range []int{0, 30, 60, 300, 900} {
		if seconds == candidate {
			return true
		}
	}
	return false
}
