package providers

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestOpenCodeGoRequiresCompleteWindows(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"usage":{"rolling":{"percent":0}}}`), nil
	})}
	if _, err := FetchAPIKeyUsage(t.Context(), client, OpenCodeGo, "key", "test"); err == nil {
		t.Fatal("incomplete usage response was accepted")
	}
}

func TestDeepSeekRequiresAvailabilityAndBalances(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{}`), nil
	})}
	if _, err := FetchAPIKeyUsage(t.Context(), client, DeepSeek, "key", "test"); err == nil {
		t.Fatal("incomplete balance response was accepted")
	}
}

func TestUnauthorizedIsCredentialError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{"error":"invalid key"}`), nil
	})}
	_, err := FetchAPIKeyUsage(t.Context(), client, DeepSeek, "bad", "test")
	if !IsCredentialError(err) {
		t.Fatalf("error = %v", err)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
