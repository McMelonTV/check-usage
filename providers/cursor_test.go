package providers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/cursorapi"
)

func cursorJWT(now time.Time) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, now.Add(time.Hour).Unix()))) + ".signature"
}

func TestCursorUsageMatchesDashboardPools(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	token := cursorJWT(now)
	exchanges := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Host != "api2.cursor.sh" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.URL.Path == "/auth/exchange_user_api_key" {
			exchanges++
			if r.Header.Get("Authorization") != "Bearer key-secret" {
				t.Fatal("wrong exchange credentials")
			}
			return response(200, fmt.Sprintf(`{"accessToken":%q}`, token)), nil
		}
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Connect-Protocol-Version") != "1" {
			t.Fatal("wrong RPC authentication")
		}
		switch r.URL.Path {
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			return response(200, `{"billingCycleEnd":"1791590400000","planUsage":{"autoPercentUsed":0,"apiPercentUsed":30},"spendLimitUsage":{"individualUsed":123}}`), nil
		case "/aiserver.v1.DashboardService/GetHardLimit":
			return response(200, `{"hardLimit":0}`), nil
		case "/aiserver.v1.DashboardService/GetPlanInfo":
			return response(200, `{"planInfo":{"planName":"pro"}}`), nil
		default:
			t.Fatalf("unexpected RPC: %s", r.URL)
			return nil, nil
		}
	})}
	usage, bearer, err := FetchCursorUsage(t.Context(), client, "key-secret", "", now)
	if err != nil || bearer != token || usage.Plan != "pro" || len(usage.Metrics) != 3 {
		t.Fatalf("usage = %#v, %v", usage, err)
	}
	reset := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC).Unix()
	for i, want := range []float64{0, 30} {
		metric := usage.Metrics[i]
		if metric.Used == nil || *metric.Used != want || metric.ResetAt == nil || *metric.ResetAt != reset {
			t.Fatalf("pool %d = %#v", i, metric)
		}
	}
	if usage.Metrics[2].Text != "Disabled" {
		t.Fatalf("on-demand = %#v", usage.Metrics[2])
	}
	if _, _, err = FetchCursorUsage(t.Context(), client, "key-secret", bearer, now); err != nil || exchanges != 1 {
		t.Fatalf("cached bearer: %v, exchanges %d", err, exchanges)
	}
}

func TestCursorUsageMissingValuesAndOptionalMetadata(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "GetCurrentPeriodUsage") {
			return response(200, `{"planUsage":{"apiPercentUsed":30}}`), nil
		}
		return response(503, `{"error":"unavailable"}`), nil
	})}
	usage, _, err := FetchCursorUsage(t.Context(), client, "key", cursorJWT(time.Now()), time.Now())
	if err != nil || usage.Metrics[0].Used != nil || usage.Metrics[0].ResetAt != nil || usage.Metrics[2].Text != "" {
		t.Fatalf("missing data = %#v, %v", usage, err)
	}
	for _, body := range []string{`{}`, `{"billingCycleEnd":"bad","planUsage":{}}`, `{"planUsage":{"autoPercentUsed":"bad"}}`} {
		client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		if _, _, err := FetchCursorUsage(t.Context(), client, "key", cursorJWT(time.Now()), time.Now()); err == nil {
			t.Fatalf("accepted incompatible response %s", body)
		}
	}
}

func TestCursorSpendUsesCorrectUnitsAndScopes(t *testing.T) {
	cents := int64(1000)
	pooled := int64(10000)
	for _, test := range []struct {
		name   string
		spend  *cursorapi.SpendLimitUsage
		policy *cursorapi.HardLimit
		want   string
	}{
		{"individual cents", &cursorapi.SpendLimitUsage{IndividualUsed: 123, IndividualLimit: &cents}, &cursorapi.HardLimit{HardLimit: 50}, "USD 1.23 / 10.00"},
		{"hard limit dollars", &cursorapi.SpendLimitUsage{IndividualUsed: 123}, &cursorapi.HardLimit{HardLimit: 10}, "USD 1.23 / 10.00"},
		{"unlimited", &cursorapi.SpendLimitUsage{IndividualUsed: 123}, &cursorapi.HardLimit{HardLimit: 2147483647}, "USD 1.23 spent (unlimited)"},
		{"organization disabled", &cursorapi.SpendLimitUsage{IndividualLimit: &cents}, &cursorapi.HardLimit{DisabledByOrganization: true}, "Disabled"},
		{"team shared pool", &cursorapi.SpendLimitUsage{IndividualUsed: 123, PooledLimit: &pooled, LimitType: "team"}, nil, "USD 1.23 spent (team)"},
		{"no metadata", nil, nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := cursorSpendText(cursorapi.UsageData{Current: cursorapi.CurrentPeriodUsage{SpendLimitUsage: test.spend}, HardLimit: test.policy})
			if got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
}

func TestCursorRevokedBearerRetriesOnce(t *testing.T) {
	now := time.Now()
	old := cursorJWT(now)
	exchanges, usageCalls := 0, 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/auth/exchange_user_api_key" {
			exchanges++
			return response(200, `{"accessToken":"replacement-secret"}`), nil
		}
		usageCalls++
		return response(401, `{"error":"key-secret"}`), nil
	})}
	_, _, err := FetchCursorUsage(t.Context(), client, "key-secret", old, now)
	if !IsCredentialError(err) || exchanges != 1 || usageCalls != 2 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("retry = %v, %d/%d", err, exchanges, usageCalls)
	}
}
