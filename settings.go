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
	if !validBarOrder(settings.BarOrder) {
		settings.BarOrder = "bar_percent_reset"
	}
	if settings.ShowBar == nil {
		v := true
		settings.ShowBar = &v
	}
	if settings.ShowPercent == nil {
		v := true
		settings.ShowPercent = &v
	}
	if settings.ShowReset == nil {
		v := true
		settings.ShowReset = &v
	}
	if !validColorTheme(settings.ColorTheme) {
		settings.ColorTheme = "default"
	}
	if !validAutoRefreshInterval(settings.AutoRefreshSeconds) {
		settings.AutoRefreshSeconds = 60
	}
}

func boolPtr(value bool) *bool {
	return &value
}

var barOrders = []string{
	"bar_percent_reset",
	"bar_reset_percent",
	"percent_bar_reset",
	"percent_reset_bar",
	"reset_bar_percent",
	"reset_percent_bar",
}

func validBarOrder(order string) bool {
	for _, candidate := range barOrders {
		if order == candidate {
			return true
		}
	}
	return false
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
