// Package codexlogic is the small, gomobile-compatible API used by Android.
// Rich Go types stay in codexapi; JSON keeps the generated Java surface stable.
package codexlogic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/codex-usage/codexapi"
)

var client = &http.Client{Timeout: 30 * time.Second}

func VerificationURL() string { return codexapi.DeviceVerificationURL }

func RequestDeviceCode() (string, error) {
	response, err := codexapi.RequestDeviceUserCode(context.Background(), client)
	return encode(response, mobileError(err))
}

// PollDeviceCode returns an empty string while user authorization is pending.
func PollDeviceCode(deviceAuthID, userCode string) (string, error) {
	response, pending, err := codexapi.PollDeviceToken(context.Background(), client, deviceAuthID, userCode)
	if err != nil || pending {
		return "", mobileError(err)
	}
	return encode(response, nil)
}

func ExchangeCode(code, verifier string) (string, error) {
	response, err := codexapi.ExchangeAuthorizationCode(context.Background(), client, code, codexapi.DeviceRedirectURI, verifier)
	return encode(response, mobileError(err))
}

func RefreshCredentials(credentialsJSON string) (string, error) {
	var credentials codexapi.Credentials
	if err := json.Unmarshal([]byte(credentialsJSON), &credentials); err != nil {
		return "", fmt.Errorf("decode credentials: %w", err)
	}
	updated, changed, err := codexapi.RefreshCredentials(context.Background(), client, credentials, time.Now())
	if err != nil {
		return "", mobileError(err)
	}
	return encode(struct {
		Credentials codexapi.Credentials `json:"credentials"`
		Changed     bool                 `json:"changed"`
	}{updated, changed}, nil)
}

func Identity(idToken string) (string, error) {
	identity, err := codexapi.ParseIdentity(idToken)
	return encode(identity, err)
}

func FetchSnapshot(accessToken, accountID string) (string, error) {
	snapshot, err := codexapi.FetchSnapshot(context.Background(), client, accessToken, accountID, "usage-widgets/0.1.0", time.Now())
	return encode(snapshot, mobileError(err))
}

// WindowKind is exported so the Android adapter can be contract-tested without
// reproducing the classification rule in Kotlin. Pass -1 when duration is absent.
func WindowKind(seconds int64, fallback string) string {
	if seconds < 0 {
		return codexapi.WindowKind(nil, fallback)
	}
	value := int(seconds)
	return codexapi.WindowKind(&value, fallback)
}

func encode(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func mobileError(err error) error {
	if err == nil {
		return nil
	}
	if codexapi.IsAuthenticationError(err) {
		return fmt.Errorf("authentication required: %w", err)
	}
	if codexapi.IsCompatibilityError(err) {
		return fmt.Errorf("compatibility error: %w", err)
	}
	return err
}
