package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchOpenCodeGoUsage(t *testing.T) {
	client := &http.Client{Transport: usageRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://opencode.ai/zen/go/v1/usage" {
			t.Fatalf("URL = %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"usage":{"rolling":{"percent":12},"weekly":{"percent":35},"monthly":{"percent":50}}}`))}, nil
	})}
	result, err := fetchProviderUsage(t.Context(), client, storedAccount{Provider: providerOpenCodeGo, AuthData: authData{APIKey: strPtr("key")}})
	if err != nil || result.Usage.Primary.Label != "ROLLING" || result.Usage.Primary.Used == nil || *result.Usage.Primary.Used != 12 {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestFetchDeepSeekUsage(t *testing.T) {
	client := &http.Client{Transport: usageRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"100.00"}]}`))}, nil
	})}
	result, err := fetchProviderUsage(t.Context(), client, storedAccount{Provider: providerDeepSeek, AuthData: authData{APIKey: strPtr("key")}})
	if err != nil || result.Usage.Primary.Summary != "CNY 100.00" || result.Usage.Details.Summary != "insufficient balance" {
		t.Fatalf("result = %#v, %v", result, err)
	}
}
