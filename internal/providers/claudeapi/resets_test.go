package claudeapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetFixtureNow sits inside the fixtures' grant windows (November 2026).
var resetFixtureNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func loadUsageFixture(t *testing.T, name string) *UsagePayload {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var payload UsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	return &payload
}

func TestUsageURLRequestsResetStatus(t *testing.T) {
	parsed, err := url.Parse(UsageURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/oauth/usage" || parsed.Query().Get("cedar_ember") != "1" || parsed.Query().Get("skip_spend") != "1" {
		t.Fatalf("usage URL = %s", UsageURL)
	}
}

func TestResetStatusParsesGrants(t *testing.T) {
	payload := loadUsageFixture(t, "usage_resets_present.json")
	status, err := payload.ResetStatus()
	if err != nil || status == nil {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	if !status.Eligible || !status.AtLimit || len(status.Grants) != 3 || status.NextGrantID != "monthly_session" {
		t.Fatalf("status = %#v", status)
	}
	first := status.Grants[0]
	if first.ID != "launch_bonus" || first.Label != "Launch bonus" || first.ResetsTotal != 3 || first.ResetsLeft != 2 ||
		first.EndsAt != "2026-12-01T00:00:00Z" || first.Type != "full" || len(first.Clears) != 2 || !first.UsableNow {
		t.Fatalf("first grant = %#v", first)
	}
	if got := status.AvailableResets(resetFixtureNow); got != 4 {
		t.Fatalf("available = %d, want 4", got)
	}
	// The usage windows still decode next to the reset block.
	if session := payload.Session(); session == nil || *session.Utilization != 100 {
		t.Fatalf("session = %#v", session)
	}
}

func TestResetStatusAbsentIsNil(t *testing.T) {
	status, err := loadUsageFixture(t, "usage_resets_absent.json").ResetStatus()
	if err != nil || status != nil {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	var payload UsagePayload
	if err := json.Unmarshal([]byte(`{"cedar_ember":null}`), &payload); err != nil {
		t.Fatal(err)
	}
	if status, err := payload.ResetStatus(); err != nil || status != nil {
		t.Fatalf("null block = %#v, %v", status, err)
	}
	if got := (*ResetStatus)(nil).AvailableResets(resetFixtureNow); got != 0 {
		t.Fatalf("nil status available = %d", got)
	}
}

func TestResetStatusFiltersUnusableGrants(t *testing.T) {
	status, err := loadUsageFixture(t, "usage_resets_filtered.json").ResetStatus()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]ResetGrant{}
	for _, grant := range status.Grants {
		ids[grant.ID] = grant
	}
	if len(status.Grants) != 4 || ids["malformed_left"].ID != "" {
		t.Fatalf("malformed grants were kept: %#v", status.Grants)
	}
	if status.NextGrantID != "" {
		t.Fatalf("next grant not in list was kept: %q", status.NextGrantID)
	}
	for id, holding := range map[string]bool{"expired_grant": false, "used_up": false, "paused_grant": false, "live": true} {
		if got := ids[id].Holding(resetFixtureNow); got != holding {
			t.Errorf("%s holding = %v, want %v", id, got, holding)
		}
	}
	if got := status.AvailableResets(resetFixtureNow); got != 1 {
		t.Fatalf("available = %d, want 1", got)
	}
}

func TestResetStatusIneligibleHoldsNothing(t *testing.T) {
	status, err := loadUsageFixture(t, "usage_resets_ineligible.json").ResetStatus()
	if err != nil || status == nil || status.Eligible || status.IneligibleReason != "tier" {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	if got := status.AvailableResets(resetFixtureNow); got != 0 {
		t.Fatalf("available = %d", got)
	}
}

func TestMalformedResetStatusKeepsUsage(t *testing.T) {
	for _, body := range []string{`{"five_hour":{"utilization":7},"cedar_ember":"broken"}`, `{"five_hour":{"utilization":7},"cedar_ember":{"grants":[]}}`} {
		var payload UsagePayload
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("%s: usage decode failed: %v", body, err)
		}
		if status, err := payload.ResetStatus(); err == nil || status != nil {
			t.Fatalf("%s: status = %#v, err = %v", body, status, err)
		}
		if *payload.Session().Utilization != 7 {
			t.Fatalf("%s: session lost", body)
		}
	}
}

func TestFetchUsageReadsResetStatus(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "usage_resets_present.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != UsageURL || request.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL)
		}
		return response(http.StatusOK, string(body)), nil
	})}
	payload, err := FetchUsage(t.Context(), client, "token")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := payload.ResetStatus(); err != nil || status.AvailableResets(resetFixtureNow) != 4 {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}
