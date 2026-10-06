// Package cursorapi implements the browser login and account-usage requests
// used by Cursor's SDK and CLI, without Node or a Cursor installation.
package cursorapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const BaseURL = "https://api2.cursor.sh"
const dashboardPath = "/aiserver.v1.DashboardService/"

type HTTPError struct {
	Operation  string
	StatusCode int
}

func (err *HTTPError) Error() string {
	return fmt.Sprintf("Cursor %s failed (HTTP %d)", err.Operation, err.StatusCode)
}

var ErrMissingAPIKey = errors.New("Cursor API key missing; sign in again")

func IsAuthenticationError(err error) bool {
	var httpErr *HTTPError
	return errors.Is(err, ErrMissingAPIKey) || (errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403))
}

func requestJSON(ctx context.Context, client *http.Client, path, token string, input, output any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("x-cursor-client-type", "cli")
	req.Header.Set("User-Agent", "check-usage")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if client == nil {
		client = http.DefaultClient
	}
	sessionClient := *client
	sessionClient.Jar = nil
	sessionClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := sessionClient.Do(req)
	if err != nil {
		return fmt.Errorf("Cursor %s request failed: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Auth failures may reflect credentials, so omit server bodies.
		return &HTTPError{Operation: path, StatusCode: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(output); err != nil {
		return fmt.Errorf("decode Cursor %s response: %w", path, err)
	}
	return nil
}
func rpc(ctx context.Context, client *http.Client, token, method string, input, output any) error {
	return requestJSON(ctx, client, dashboardPath+method, token, input, output)
}

// Millis accepts both ProtoJSON int64 strings and JSON integers.
type Millis int64

func (value *Millis) UnmarshalJSON(data []byte) error {
	text := string(data)
	if len(text) > 0 && text[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Cursor millisecond timestamp")
	}
	*value = Millis(number)
	return nil
}

type PlanUsage struct {
	AutoPercentUsed *float64 `json:"autoPercentUsed"`
	APIPercentUsed  *float64 `json:"apiPercentUsed"`
}
type CurrentPeriodUsage struct {
	BillingCycleEnd Millis     `json:"billingCycleEnd"`
	PlanUsage       *PlanUsage `json:"planUsage"`
}
type PlanInfo struct {
	PlanName        string `json:"planName"`
	BillingCycleEnd Millis `json:"billingCycleEnd"`
}
type UsageData struct {
	Current  CurrentPeriodUsage
	PlanInfo *PlanInfo
}

func FetchUsage(ctx context.Context, client *http.Client, token string) (UsageData, error) {
	var result UsageData
	if err := rpc(ctx, client, token, "GetCurrentPeriodUsage", struct{}{}, &result.Current); err != nil {
		return result, err
	}
	if result.Current.PlanUsage == nil {
		return result, fmt.Errorf("Cursor account does not expose individual model-pool quotas")
	}
	// The plan name is optional metadata, as in the Cursor CLI.
	var plan struct {
		PlanInfo *PlanInfo `json:"planInfo"`
	}
	if err := rpc(ctx, client, token, "GetPlanInfo", struct{}{}, &plan); err == nil {
		result.PlanInfo = plan.PlanInfo
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

type Identity struct {
	AuthID   string `json:"authId"`
	WorkosID string `json:"workosId"`
	UserID   int64  `json:"userId"`
	Email    string `json:"email"`
}

func (identity Identity) AccountID() string {
	if identity.WorkosID != "" {
		return identity.WorkosID
	}
	if identity.AuthID != "" {
		return identity.AuthID
	}
	if identity.UserID > 0 {
		return strconv.FormatInt(identity.UserID, 10)
	}
	return ""
}
func GetIdentity(ctx context.Context, client *http.Client, token string) (Identity, error) {
	var result Identity
	err := rpc(ctx, client, token, "GetMe", struct{}{}, &result)
	if err == nil && result.AccountID() == "" {
		err = fmt.Errorf("Cursor login returned no account identity")
	}
	return result, err
}
