package nativecli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("CHECK_USAGE_NATIVE_TEST"); mode != "" {
		switch mode {
		case "agy":
			if strings.Join(os.Args[1:], " ") != "-p /usage --output-format json" || os.Getenv("AGY_CLI_DISABLE_AUTO_UPDATE") != "true" || os.Getenv("BROWSER") == "" {
				os.Exit(2)
			}
			fmt.Print(`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-weekly","remainingFraction":1},{"bucketId":"gemini-5h","remainingFraction":0.75}]},{"displayName":"Claude and GPT models","buckets":[{"bucketId":"3p-weekly","remainingFraction":0},{"bucketId":"3p-5h","remainingFraction":0.25}]}]}}}`)
		case "json":
			fmt.Print(`{"plan":"Fixture","metrics":[{"kind":"percentage","slot":"weekly","label":"WEEKLY","used_percent":30}]}`)
		case "bad":
			fmt.Print(`{"metrics":[{"kind":"percentage","slot":"weekly","used_percent":999}]}`)
		case "error":
			fmt.Fprint(os.Stderr, "credential-secret")
			os.Exit(3)
		case "hang":
			time.Sleep(time.Hour)
		case "codex":
			if strings.Join(os.Args[1:], " ") != "app-server" {
				os.Exit(2)
			}
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				var input struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				_ = json.Unmarshal(scanner.Bytes(), &input)
				var result any
				switch input.Method {
				case "initialize":
					result = map[string]any{}
				case "initialized":
					continue
				case "account/read":
					result = json.RawMessage(`{"account":{"type":"chatgpt","email":"fixture@example.com","planType":"pro"},"userId":"user"}`)
				case "account/rateLimits/read":
					result = json.RawMessage(`{"rateLimits":{"planType":"pro","primary":{"usedPercent":25,"windowDurationMins":300},"secondary":{"usedPercent":50,"windowDurationMins":10080}},"rateLimitResetCredits":{"availableCount":0,"credits":[]}}`)
				default:
					os.Exit(2)
				}
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": input.ID, "result": result})
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func fixtureConfig(t *testing.T, driver, mode string) Config {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Config{Driver: driver, Executable: executable, Env: map[string]string{"CHECK_USAGE_NATIVE_TEST": mode}}
}
func TestNativeAgyReadsStructuredReportAndRetainsFourLimits(t *testing.T) {
	result, err := Fetch(t.Context(), providers.Antigravity, fixtureConfig(t, "agy", "agy"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage.Metrics) != 4 || result.Usage.Metrics[2].Scope != "Claude/GPT" || *result.Usage.Metrics[2].Used != 100 {
		t.Fatal("lost model-family quotas")
	}
}

func TestAgySnakeCaseReportKeepsUntouchedAndUnknownValues(t *testing.T) {
	result, err := parseAgy([]byte(`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"display_name":"Gemini Models","buckets":[{"bucket_id":"gemini-weekly","remaining":{"remaining_fraction":1},"reset_time":"2026-12-01T00:00:00Z"},{"bucket_id":"gemini-5h"}]}]}}}`))
	if err != nil || len(result.Usage.Metrics) != 2 || result.Usage.Metrics[0].Used == nil || *result.Usage.Metrics[0].Used != 0 || result.Usage.Metrics[0].ResetAt == nil || result.Usage.Metrics[1].Used != nil {
		t.Fatal("snake-case native report lost measured/unknown distinction")
	}
}
func TestNativeCodexUsesOnlyReadRPCs(t *testing.T) {
	result, err := Fetch(t.Context(), providers.Codex, fixtureConfig(t, "codex", "codex"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Email != "fixture@example.com" || len(result.Usage.Metrics) != 2 || *result.Usage.Metrics[1].Used != 50 || result.ResetCredits == nil || !result.ResetCredits.TotalEarnedUnknown {
		t.Fatal("native account/limits mapping failed")
	}
}
func TestNativeAdapterTimeoutAndSanitizedFailures(t *testing.T) {
	result, err := Fetch(t.Context(), providers.Claude, fixtureConfig(t, "json", "json"))
	if err != nil || result.Usage.Plan != "Fixture" {
		t.Fatal("JSON adapter failed")
	}
	for _, mode := range []string{"bad", "error", "hang"} {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		_, err := Fetch(ctx, providers.Claude, fixtureConfig(t, "json", mode))
		cancel()
		if err == nil || strings.Contains(err.Error(), "credential-secret") {
			t.Fatalf("unsafe adapter failure for %s", mode)
		}
	}
	if err := (Config{Driver: "agy", Args: []string{"prompt"}}).Validate(providers.Antigravity); err == nil {
		t.Fatal("native usage arguments could be replaced by model prompts")
	}
}
