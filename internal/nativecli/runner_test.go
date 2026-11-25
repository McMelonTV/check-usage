package nativecli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/charmbracelet/x/term"
)

// serveFakeAgyStatus imitates interactive agy: it requires a terminal and
// serves GetUserStatus on loopback TLS, guarded by the CSRF token from its environment.
func serveFakeAgyStatus() {
	if !term.IsTerminal(os.Stdin.Fd()) {
		os.Exit(4)
	}
	_ = os.WriteFile(os.Getenv("CHECK_USAGE_AGY_PIDFILE"), []byte(strconv.Itoa(os.Getpid())), 0600)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != agyUserStatusRPC || request.Header.Get(agyCSRFHeader) == "" || request.Header.Get(agyCSRFHeader) != os.Getenv(agyCSRFEnv) {
			http.Error(writer, `{"code":"unauthenticated"}`, http.StatusUnauthorized)
			return
		}
		fmt.Fprint(writer, os.Getenv("CHECK_USAGE_AGY_STATUS"))
	}))
	server.StartTLS()
	select {}
}

func TestMain(m *testing.M) {
	if mode := os.Getenv("CHECK_USAGE_NATIVE_TEST"); mode != "" {
		switch mode {
		case "agy":
			if len(os.Args) == 1 {
				serveFakeAgyStatus()
			}
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

func TestNativeAgyReadsPlanFromInteractiveStatusAndStopsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the agy plan probe needs a Unix pseudo-terminal")
	}
	config := fixtureConfig(t, "agy", "agy")
	pidFile := t.TempDir() + "/pid"
	config.Env["CHECK_USAGE_AGY_PIDFILE"] = pidFile
	config.Env["CHECK_USAGE_AGY_STATUS"] = `{"userStatus":{"planStatus":{"planInfo":{"planName":"Google AI Pro"}},"g1Tier":"G1_ULTRA_TIER"}}`
	config.FetchPlan = true
	result, err := Fetch(t.Context(), providers.Antigravity, config)
	if err != nil || !result.PlanChecked || result.Usage.Plan != "Google AI Pro" || len(result.Usage.Metrics) != 4 {
		t.Fatalf("plan probe failed: plan=%q checked=%v err=%v", result.Usage.Plan, result.PlanChecked, err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal("fake agy never started in a terminal")
	}
	pid, _ := strconv.Atoi(string(data))
	if process, err := os.FindProcess(pid); err == nil && process.Signal(syscall.Signal(0)) == nil {
		t.Fatal("interactive agy was left running")
	}
}

func TestNativeAgySkipsPlanUnlessRequested(t *testing.T) {
	config := fixtureConfig(t, "agy", "agy")
	pidFile := t.TempDir() + "/pid"
	config.Env["CHECK_USAGE_AGY_PIDFILE"] = pidFile
	result, err := Fetch(t.Context(), providers.Antigravity, config)
	if err != nil || result.PlanChecked || result.Usage.Plan != "" {
		t.Fatal("plan probe ran without being requested")
	}
	if _, err := os.Stat(pidFile); err == nil {
		t.Fatal("interactive agy started without a plan request")
	}
}

func TestAgyPlanParsingPrefersPlanNameAndDetectsSignOut(t *testing.T) {
	cases := map[string]string{
		`{"userStatus":{"planStatus":{"planInfo":{"planName":"Google AI Ultra"}}}}`: "Google AI Ultra",
		`{"userStatus":{"planInfo":{"planName":"Pro"}}}`:                            "Pro",
		`{"userStatus":{"userTier":{"id":"g1-pro-tier","name":"Google AI Pro"}}}`:   "Google AI Pro",
		`{"userStatus":{"g1Tier":"G1_ULTRA_LITE_TIER"}}`:                            "Ultra Lite",
		`{"userStatus":{"g1Tier":"USER_TIER_UNSPECIFIED"}}`:                         "",
		`{"userStatus":{}}`: "",
	}
	for input, want := range cases {
		if plan, err := parseAgyPlan([]byte(input)); err != nil || plan != want {
			t.Fatalf("parseAgyPlan(%s) = %q, %v; want %q", input, plan, err, want)
		}
	}
	signedOut := `{"userStatus":{"cascadeModelConfigData":{"errorMessage":"failed to get load code assist response: error getting token source: You are not logged into Antigravity."}}}`
	if _, err := parseAgyPlan([]byte(signedOut)); err != errAgySignedOut {
		t.Fatal("signed-out agy was not detected")
	}
}

func TestAgyPlanDueLimitsInteractiveProbes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	if !AgyPlanDue("", time.Unix(0, 0), now) || AgyPlanDue("", now.Add(-30*time.Minute), now) || !AgyPlanDue("", now.Add(-time.Hour), now) {
		t.Fatal("unknown plan should be retried hourly")
	}
	if AgyPlanDue("Pro", now.Add(-23*time.Hour), now) || !AgyPlanDue("Pro", now.Add(-24*time.Hour), now) {
		t.Fatal("known plan should be refreshed daily")
	}
}
