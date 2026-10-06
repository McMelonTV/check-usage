package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func settingsPath(accountsPath string) string {
	return filepath.Join(filepath.Dir(accountsPath), "settings.json")
}

func LoadSettings(accountsPath string) (Settings, error) {
	settings := DefaultSettings()
	content, err := os.ReadFile(settingsPath(accountsPath))
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return Settings{}, err
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return Settings{}, err
	}
	normalizeSettings(&settings)
	return settings, nil
}

func SaveSettings(accountsPath string, settings Settings) error {
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

func normalizeSettings(settings *Settings) {
	if settings.UsageDisplay != "used" && settings.UsageDisplay != "remaining" {
		settings.UsageDisplay = "used"
	}
	if settings.BarFill != "left" && settings.BarFill != "right" {
		settings.BarFill = "left"
	}
	if !ValidBarOrder(settings.BarOrder) {
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

var BarOrders = []string{
	"bar_percent_reset",
	"bar_reset_percent",
	"percent_bar_reset",
	"percent_reset_bar",
	"reset_bar_percent",
	"reset_percent_bar",
}

func ValidBarOrder(order string) bool {
	for _, candidate := range BarOrders {
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
