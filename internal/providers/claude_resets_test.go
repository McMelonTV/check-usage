package providers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

func TestClaudeResetCreditsAbsentOrIneligibleAreZero(t *testing.T) {
	for _, name := range []string{"usage_resets_absent.json", "usage_resets_ineligible.json"} {
		payload, _ := claudeFixture(t, name)
		credits, err := ClaudeResetCredits(payload, claudeResetNow)
		if err != nil || credits == nil || credits.AvailableCount != 0 || credits.TotalEarnedCount != 0 || credits.Credits == nil || len(credits.Credits) != 0 {
			t.Fatalf("%s: credits = %#v, err = %v", name, credits, err)
		}
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
		name, body string
		want       int
		malformed  bool
	}{
		{name: "present", body: body, want: 4},
		{name: "malformed", body: `{"five_hour":{"utilization":5,"resets_at":null},"cedar_ember":[1,2]}`, malformed: true},
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
		if tc.malformed {
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
