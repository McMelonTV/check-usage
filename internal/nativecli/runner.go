// Package nativecli reads usage through locally installed provider tools.
// Commands are configured by the server administrator, never by network clients.
package nativecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/antigravityapi"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

type Config struct {
	Driver         string            `json:"driver"`
	Executable     string            `json:"executable,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	RefreshSeconds int               `json:"refresh_seconds,omitempty"`
	Fallback       bool              `json:"fallback,omitempty"`
}

type Result struct {
	Usage        providers.Usage
	Email        string
	AccountID    string
	ResetCredits *codexapi.ResetCreditsPayload
}

func (config Config) Validate(provider string) error {
	if config.Driver != "agy" && config.Driver != "codex" && config.Driver != "json" {
		return errors.New("native driver must be agy, codex, or json")
	}
	if config.Driver == "agy" && provider != providers.Antigravity || config.Driver == "codex" && provider != providers.Codex {
		return errors.New("native driver does not match provider")
	}
	if config.Driver == "json" && config.Executable == "" {
		return errors.New("JSON adapter requires an executable")
	}
	if config.Driver != "json" && len(config.Args) != 0 {
		return errors.New("built-in native drivers use fixed usage-only arguments")
	}
	if config.TimeoutSeconds < 0 || config.TimeoutSeconds > 120 || config.RefreshSeconds < 0 {
		return errors.New("invalid native timeout or refresh interval")
	}
	for key := range config.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || key == "HOME" {
			return errors.New("invalid native environment override; use provider configuration directories")
		}
	}
	return nil
}

func (config Config) Interval() time.Duration {
	minimum := time.Minute
	if config.Driver == "agy" {
		minimum = 5 * time.Minute
	}
	return max(minimum, time.Duration(config.RefreshSeconds)*time.Second)
}

func Fetch(ctx context.Context, provider string, config Config) (Result, error) {
	if err := config.Validate(provider); err != nil {
		return Result{}, err
	}
	timeout := 90 * time.Second
	if config.TimeoutSeconds > 0 {
		timeout = time.Duration(config.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "check-usage-probe-")
	if err != nil {
		return Result{}, errors.New("cannot create private native probe directory")
	}
	defer os.RemoveAll(dir)
	executable := config.Executable
	if executable == "" {
		executable = config.Driver
	}
	executable, err = exec.LookPath(executable)
	if err != nil {
		return Result{}, fmt.Errorf("%s CLI is not installed on the server", config.Driver)
	}
	if config.Driver == "codex" {
		return fetchCodex(ctx, executable, dir, config)
	}
	args := config.Args
	if config.Driver == "agy" {
		args = []string{"-p", "/usage", "--output-format", "json"}
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env = dir, environment(config.Env)
	if config.Driver == "agy" {
		// A quota refresh must not launch a login browser or update the CLI.
		guardDir := filepath.Join(dir, "browser-guard")
		if err := os.Mkdir(guardDir, 0700); err != nil {
			return Result{}, err
		}
		for _, name := range []string{"open", "xdg-open"} {
			if err := os.WriteFile(filepath.Join(guardDir, name), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
				return Result{}, err
			}
		}
		cmd.Env = withEnv(cmd.Env, "PATH", guardDir+string(os.PathListSeparator)+envValue(cmd.Env, "PATH"))
		cmd.Env = withEnv(cmd.Env, "BROWSER", filepath.Join(guardDir, "xdg-open"))
		cmd.Env = withEnv(cmd.Env, "AGY_CLI_DISABLE_AUTO_UPDATE", "true")
	}
	configureProcess(cmd)
	defer killProcess(cmd)
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("%s usage probe: %w", config.Driver, ctx.Err())
		}
		return Result{}, fmt.Errorf("%s usage command failed; check its login on the server", config.Driver)
	}
	if output.overflow {
		return Result{}, errors.New("native usage output exceeds 1 MiB")
	}
	if config.Driver == "agy" {
		return parseAgy(output.Bytes())
	}
	var usage providers.Usage
	if json.Unmarshal(output.Bytes(), &usage) != nil {
		return Result{}, errors.New("native adapter returned invalid usage JSON")
	}
	if err := validateUsage(usage); err != nil {
		return Result{}, err
	}
	return Result{Usage: usage}, nil
}

func parseAgy(data []byte) (Result, error) {
	// Print-mode releases use snake_case; localhost reports use camelCase.
	// Normalize known keys without interpreting missing fractions as defaults.
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return Result{}, errors.New("agy returned invalid usage JSON")
	}
	keys := map[string]string{"display_name": "displayName", "bucket_id": "bucketId", "remaining_fraction": "remainingFraction", "reset_time": "resetTime"}
	var normalize func(any) any
	normalize = func(value any) any {
		switch value := value.(type) {
		case map[string]any:
			result := map[string]any{}
			for key, item := range value {
				mapped := key
				if alias, ok := keys[key]; ok {
					mapped = alias
				}
				if key == "case" && item == "remaining_fraction" {
					item = "remainingFraction"
				}
				result[mapped] = normalize(item)
			}
			return result
		case []any:
			for i, item := range value {
				value[i] = normalize(item)
			}
			return value
		default:
			return value
		}
	}
	data, _ = json.Marshal(normalize(raw))
	var report struct {
		Status  string `json:"status"`
		Command struct {
			Name string                 `json:"name"`
			Data antigravityapi.Summary `json:"data"`
		} `json:"command"`
	}
	if json.Unmarshal(data, &report) != nil || report.Status != "SUCCESS" || report.Command.Name != "usage" {
		return Result{}, errors.New("agy returned no successful structured usage report; update agy and check its login")
	}
	quotas, err := report.Command.Data.Quotas()
	if err != nil {
		return Result{}, err
	}
	return Result{Usage: providers.AntigravityUsage(quotas)}, nil
}

func validateUsage(usage providers.Usage) error {
	if usage.Metrics == nil && usage.Plan == "" {
		return errors.New("native adapter returned no usage measurements")
	}
	for _, metric := range usage.Metrics {
		if metric.Slot != providers.SessionSlot && metric.Slot != providers.WeeklySlot && metric.Slot != providers.MonthlySlot {
			return errors.New("native adapter returned an invalid metric slot")
		}
		if metric.Kind != providers.Percentage && metric.Kind != providers.Text {
			return errors.New("native adapter returned an invalid metric kind")
		}
		if metric.Used != nil && (math.IsNaN(*metric.Used) || math.IsInf(*metric.Used, 0) || *metric.Used < 0 || *metric.Used > 100) {
			return errors.New("native adapter returned an invalid percentage")
		}
	}
	return nil
}

type cappedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	if buffer.Len()+len(data) > 1<<20 {
		buffer.overflow = true
		return 0, errors.New("native output limit reached")
	}
	return buffer.Buffer.Write(data)
}
func environment(overrides map[string]string) []string {
	values := os.Environ()
	for key, value := range overrides {
		values = withEnv(values, key, value)
	}
	return values
}
func withEnv(values []string, key, value string) []string {
	result := make([]string, 0, len(values)+1)
	for _, entry := range values {
		if !strings.HasPrefix(entry, key+"=") {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}
func envValue(values []string, key string) string {
	for _, entry := range values {
		if strings.HasPrefix(entry, key+"=") {
			return strings.TrimPrefix(entry, key+"=")
		}
	}
	return ""
}
