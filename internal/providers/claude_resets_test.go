package providers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
)

var claudeResetNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func claudeFixture(t *testing.T, name string) (*claudeapi.UsagePayload, string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("claudeapi", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var payload claudeapi.UsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	return &payload, string(body)
}

func statusCounts(t *testing.T, payload *claudeapi.UsagePayload) (map[string]int, int, int) {
	t.Helper()
	credits, err := ClaudeResetCredits(payload, claudeResetNow)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, credit := range credits.Credits {
		counts[credit.Status]++
	}
	return counts, credits.AvailableCount, credits.TotalEarnedCount
}

func TestClaudeResetCreditsPresent(t *testing.T) {
	payload, _ := claudeFixture(t, "usage_resets_present.json")
	credits, err := ClaudeResetCredits(payload, claudeResetNow)
	if err != nil {
		t.Fatal(err)
	}
	if credits.AvailableCount != 4 || credits.TotalEarnedCount != 5 || len(credits.Credits) != 5 {
		t.Fatalf("credits = %#v", credits)
	}
	first := credits.Credits[0]
	if first.Status != "available" || first.Title != "Launch bonus" || first.GrantedAt != "2026-11-01T00:00:00Z" || first.ExpiresAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("first credit = %#v", first)
	}
	if credits.Credits[2].Status != "redeemed" || credits.Credits[3].Title != claudeResetTitle {
		t.Fatalf("credits = %#v", credits.Credits)
	}
	snapshot := ClaudeSnapshot(ClaudeUsage(payload), credits, nil, claudeResetNow)
	if snapshot.Credits == nil || snapshot.Credits.AvailableCount != 4 || snapshot.CreditsError != "" {
		t.Fatalf("snapshot credits = %#v, %q", snapshot.Credits, snapshot.CreditsError)
	}
	// The earliest expiry among usable resets is the session grant's end.
	if want := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC).Unix(); snapshot.Credits.EarliestExpiryEpoch == nil || *snapshot.Credits.EarliestExpiryEpoch != want {
		t.Fatalf("earliest expiry = %v, want %d", snapshot.Credits.EarliestExpiryEpoch, want)
	}
	if len(snapshot.Windows) != 2 || snapshot.Windows[0].Kind != "short_window" || *snapshot.Windows[0].UsedPercent != 100 ||
		*snapshot.Windows[0].Remaining != 0 || *snapshot.Windows[1].WindowSeconds != 7*24*3600 {
		t.Fatalf("windows = %#v", snapshot.Windows)
	}
}

func TestClaudeResetCreditsConfirmedIneligibleAreZero(t *testing.T) {
	payload, _ := claudeFixture(t, "usage_resets_ineligible.json")
	credits, err := ClaudeResetCredits(payload, claudeResetNow)
	if err != nil || credits == nil || credits.AvailableCount != 0 || credits.TotalEarnedCount != 0 || credits.Credits == nil || len(credits.Credits) != 0 {
		t.Fatalf("credits = %#v, err = %v", credits, err)
	}
}

func TestClaudeProResetCreditsFromCLISurface(t *testing.T) {
	payload, _ := claudeFixture(t, "usage_resets_pro.json")
	credits, err := ClaudeResetCredits(payload, claudeResetNow)
	if err != nil || credits == nil || credits.AvailableCount != 1 || credits.TotalEarnedCount != 2 || len(credits.Credits) != 2 {
		t.Fatalf("credits = %#v, err = %v", credits, err)
	}
	if credits.Credits[0].Status != "redeemed" || credits.Credits[1].Status != "available" || credits.Credits[1].Title != "One full limit reset (November 2026)" {
		t.Fatalf("credits = %#v", credits.Credits)
	}
	snapshot := ClaudeSnapshot(ClaudeUsage(payload), credits, nil, claudeResetNow)
	expiresAt := time.Date(2026, 12, 10, 22, 0, 0, 0, time.UTC).Unix()
	if snapshot.Credits == nil || snapshot.Credits.AvailableCount != 1 || snapshot.Credits.EarliestExpiryEpoch == nil || *snapshot.Credits.EarliestExpiryEpoch != expiresAt {
		t.Fatalf("snapshot credits = %#v", snapshot.Credits)
	}
}

func TestClaudeResetCreditsUnknownBalanceIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
	}{
		{"absent", `{"five_hour":{"utilization":100}}`, "omitted reset status"},
		{"null", `{"cedar_ember":null}`, "omitted reset status"},
		{"old client", `{"cedar_ember":{"eligible":false,"ineligible_reason":"cli_version"}}`, "cli_version"},
		{"surface", `{"cedar_ember":{"eligible":false,"ineligible_reason":"surface"}}`, "surface"},
		{"mobile surface", `{"cedar_ember":{"eligible":false,"ineligible_reason":"mobile"}}`, "mobile"},
		{"not at limit", `{"cedar_ember":{"eligible":false,"ineligible_reason":"not_at_wall"}}`, "not_at_wall"},
		{"server unavailable", `{"cedar_ember":{"eligible":false,"ineligible_reason":"unavailable"}}`, "unavailable"},
		{"unspecified", `{"cedar_ember":{"eligible":false}}`, "eligibility was not confirmed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload claudeapi.UsagePayload
			if err := json.Unmarshal([]byte(tc.body), &payload); err != nil {
				t.Fatal(err)
			}
			credits, err := ClaudeResetCredits(&payload, claudeResetNow)
			if credits != nil || err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("unknown balance reported as credits: %#v, err = %v", credits, err)
			}
			snapshot := ClaudeSnapshot(ClaudeUsage(&payload), credits, err, claudeResetNow)
			if snapshot.Credits != nil || snapshot.CreditsError == "" {
				t.Fatalf("snapshot reported unknown balance as credits: %#v", snapshot)
			}
		})
	}
}

func TestClaudeResetCreditsFiltersExpiredUsedAndPaused(t *testing.T) {
	payload, _ := claudeFixture(t, "usage_resets_filtered.json")
	counts, available, total := statusCounts(t, payload)
	// expired_grant: 1 expired + 1 redeemed; used_up: 2 redeemed;
	// paused_grant: 1 paused; live: 1 available. Malformed grants are dropped.
	if available != 1 || total != 6 || counts["available"] != 1 || counts["expired"] != 1 || counts["redeemed"] != 3 || counts["paused"] != 1 {
		t.Fatalf("counts = %v, available = %d, total = %d", counts, available, total)
	}
}

func TestFetchClaudeUsageCarriesResetCredits(t *testing.T) {
	_, body := claudeFixture(t, "usage_resets_present.json")
	for _, tc := range []struct {
		name, body  string
		want        int
		unavailable bool
	}{
		{name: "present", body: body, want: 4},
		{name: "malformed", body: `{"five_hour":{"utilization":5,"resets_at":null},"cedar_ember":[1,2]}`, unavailable: true},
		{name: "absent", body: `{"five_hour":{"utilization":100,"resets_at":null}}`, unavailable: true},
		{name: "client refused", body: `{"five_hour":{"utilization":100},"cedar_ember":{"eligible":false,"ineligible_reason":"cli_version"}}`, unavailable: true},
		{name: "surface refused", body: `{"five_hour":{"utilization":100},"cedar_ember":{"eligible":false,"ineligible_reason":"surface","at_limit":false,"exhausted":[],"grants":[],"next_grant_id":null,"weekly_resets_at":null,"cooldown_until":null}}`, unavailable: true},
	} {
		client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != claudeapi.UsageURL {
				t.Fatalf("unexpected request %s", request.URL)
			}
			return response(http.StatusOK, tc.body), nil
		})}
		result, err := FetchClaudeUsage(t.Context(), client, claudeapi.Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: claudeResetNow.Unix() + 3600}, claudeResetNow, false)
		if err != nil || len(result.Usage.Metrics) < 2 || result.Usage.Metrics[0].Used == nil {
			t.Fatalf("%s: usage = %#v, err = %v", tc.name, result.Usage, err)
		}
		if tc.unavailable {
			if result.ResetError == nil || result.ResetCredits != nil {
				t.Fatalf("%s: reset = %#v, %v", tc.name, result.ResetCredits, result.ResetError)
			}
			snapshot := ClaudeSnapshot(result.Usage, result.ResetCredits, result.ResetError, claudeResetNow)
			if snapshot.Credits != nil || snapshot.CreditsError == "" {
				t.Fatalf("%s: snapshot = %#v", tc.name, snapshot)
			}
			continue
		}
		if result.ResetError != nil || result.ResetCredits == nil || result.ResetCredits.AvailableCount != tc.want {
			t.Fatalf("%s: reset = %#v, %v", tc.name, result.ResetCredits, result.ResetError)
		}
	}
	if _, err := ClaudeResetCredits(&claudeapi.UsagePayload{Resets: json.RawMessage(`"x"`)}, claudeResetNow); err == nil {
		t.Fatalf("malformed block error = %v", err)
	}
}

func TestClaudeSupportsResetCredits(t *testing.T) {
	definition, ok := Get(Claude)
	if !ok || !definition.SupportsResetCredits {
		t.Fatalf("definition = %#v", definition)
	}
}
