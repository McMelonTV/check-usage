package login

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

func TestCursorLoginRetainsRevocableKeyOnly(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"apiKey":"key-secret"}`
		if strings.HasSuffix(r.URL.Path, "GetMe") {
			body = `{"workosId":"user-1","email":"person@example.com"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	account, err := FinishCursor(t.Context(), client, "access-secret", "")
	if err != nil || account.Provider != providers.Cursor || storage.StringValue(account.AuthData.APIKey) != "key-secret" || storage.StringValue(account.AuthData.AccountID) != "user-1" || account.AuthData.RefreshToken != nil {
		t.Fatalf("login = %#v, %v", account, err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
