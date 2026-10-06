package usageapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/providers"
)

func TestCursorRPCFetchCacheAndKeyReplacement(t *testing.T) {
	exchanges, calls, reject := 0, 0, false
	token := testJWT(t, map[string]any{"exp": 4_000_000_000})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api2.cursor.sh" || r.Header.Get("Cookie") != "" {
			t.Fatal("wrong Cursor endpoint")
		}
		if reject {
			return jsonResponse(401, `{"error":"key-secret"}`), nil
		}
		if r.URL.Path == "/auth/exchange_user_api_key" {
			exchanges++
			return jsonResponse(200, `{"accessToken":`+mustJSON(t, token)+`}`), nil
		}
		switch r.URL.Path {
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			return jsonResponse(200, `{"planUsage":{"autoPercentUsed":0,"apiPercentUsed":30}}`), nil
		case "/aiserver.v1.DashboardService/GetHardLimit":
			return jsonResponse(200, `{"hardLimit":0}`), nil
		case "/aiserver.v1.DashboardService/GetPlanInfo":
			return jsonResponse(200, `{"planInfo":{"planName":"pro"}}`), nil
		default:
			t.Fatal("unexpected RPC")
			return nil, nil
		}
	})}
	service := testService(t, client)
	server := RPCServer{Service: service}
	response := server.Handle(t.Context(), RPCRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "accounts.api_key.save", Params: json.RawMessage(`{"provider":"cursor","api_key":"key-secret","name":"My Cursor"}`)})
	if response.Error != nil {
		t.Fatalf("save: %#v", response.Error)
	}
	mutation := response.Result.(AccountMutation)
	results, err := service.Usage(t.Context(), mutation.Account.ID, true)
	if err != nil || len(results) != 1 || results[0].Error != "" || results[0].Account.PlanType != "pro" || len(results[0].Metrics) != 3 || results[0].Metrics[0].Slot != providers.CursorModelsSlot || results[0].Metrics[2].Text != "Disabled" {
		t.Fatalf("usage %#v, %v", results, err)
	}
	cached, err := service.Usage(t.Context(), mutation.Account.ID, false)
	if err != nil || !cached[0].Cached || calls != 4 {
		t.Fatalf("cache: %#v, %v, calls %d", cached, err, calls)
	}
	if _, err = service.Usage(t.Context(), mutation.Account.ID, true); err != nil || exchanges != 1 {
		t.Fatalf("bearer cache: %v, exchanges %d", err, exchanges)
	}
	stored, err := service.loadAccounts()
	if err != nil || stringValue(stored.Accounts[0].AuthData.AccessToken) != token {
		t.Fatal("bearer was not persisted")
	}
	reject = true
	stale, err := service.Usage(t.Context(), mutation.Account.ID, true)
	if err != nil || !stale[0].Cached || stale[0].Error == "" {
		t.Fatalf("stale fallback: %#v, %v", stale, err)
	}
	accounts, _ := service.ListAccounts()
	for _, value := range []any{response, results, cached, stale, accounts} {
		encoded, _ := json.Marshal(value)
		if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), token) {
			t.Fatalf("public response leaked credential: %s", encoded)
		}
	}
	updated, err := service.SaveAPIKeyAccount(APIKeyAccount{Account: mutation.Account.ID, Provider: "cursor", APIKey: "replacement-secret"})
	if err != nil || updated.Action != "updated" {
		t.Fatalf("replace %#v %v", updated, err)
	}
	if _, exists, err := service.loadCache(mutation.Account.ID); err != nil || exists {
		t.Fatal("old usage survived key replacement")
	}
	stored, _ = service.loadAccounts()
	if stored.Accounts[0].AuthData.AccessToken != nil {
		t.Fatal("old bearer survived key replacement")
	}
}

func TestCursorBrowserRPCLoginAndReauthentication(t *testing.T) {
	pending := true
	keys := 0
	identity := "cursor-user"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/auth/poll":
			if pending {
				return jsonResponse(404, `{}`), nil
			}
			return jsonResponse(200, `{"accessToken":"access-secret","refreshToken":"discard-secret"}`), nil
		case "/aiserver.v1.DashboardService/GetMe":
			return jsonResponse(200, `{"workosId":`+mustJSON(t, identity)+`,"email":"person@example.com"}`), nil
		case "/aiserver.v1.DashboardService/CreateUserApiKey":
			keys++
			return jsonResponse(200, `{"apiKey":"key-secret"}`), nil
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}
	})}
	service := testService(t, client)
	server := RPCServer{Service: service}
	result := server.Handle(t.Context(), RPCRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "auth.browser.begin", Params: json.RawMessage(`{"provider":"cursor"}`)})
	if result.Error != nil {
		t.Fatalf("begin: %#v", result.Error)
	}
	session := result.Result.(BrowserAuthSession)
	private := service.browserSessions[session.SessionID]
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), private.Verifier) {
		t.Fatal("verifier leaked")
	}
	poll := BrowserAuthPoll{Provider: "cursor", SessionID: session.SessionID}
	params, _ := json.Marshal(poll)
	result = server.Handle(t.Context(), RPCRequest{JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "auth.browser.poll", Params: params})
	if result.Error != nil || result.Result.(DeviceAuthResult).Status != "pending" {
		t.Fatalf("pending %#v", result)
	}
	pending = false
	complete, err := service.PollBrowserAuth(t.Context(), poll)
	if err != nil || complete.Status != "complete" || complete.Account.AuthType != "api_key" || keys != 1 {
		t.Fatalf("login %#v %v", complete, err)
	}
	encoded, _ = json.Marshal(complete)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal("login returned credentials")
	}
	store, _ := service.loadAccounts()
	if stringValue(store.Accounts[0].AuthData.APIKey) != "key-secret" || store.Accounts[0].AuthData.RefreshToken != nil {
		t.Fatal("incorrect stored credentials")
	}
	if _, err := service.PollBrowserAuth(t.Context(), poll); err == nil || keys != 1 {
		t.Fatal("completed session was replayed")
	}
	if err := service.saveCache(complete.Account.ID, cacheEntry{FetchedAt: 1}); err != nil {
		t.Fatal(err)
	}
	next, _ := service.BeginBrowserAuth(t.Context(), "cursor")
	updated, err := service.PollBrowserAuth(t.Context(), BrowserAuthPoll{Provider: "cursor", SessionID: next.SessionID, Account: complete.Account.ID})
	if err != nil || updated.Action != "updated" || updated.Account.ID != complete.Account.ID {
		t.Fatalf("reauth %#v %v", updated, err)
	}
	if _, exists, _ := service.loadCache(complete.Account.ID); exists {
		t.Fatal("reauth retained old usage")
	}
	identity = "wrong-user"
	next, _ = service.BeginBrowserAuth(t.Context(), "cursor")
	if _, err := service.PollBrowserAuth(t.Context(), BrowserAuthPoll{Provider: "cursor", SessionID: next.SessionID, Account: complete.Account.ID}); err == nil || keys != 2 {
		t.Fatal("wrong account replaced login or minted key")
	}
}

func TestCursorBrowserSessionsAreScopedAndExpire(t *testing.T) {
	service := testService(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired session made HTTP request")
		return nil, nil
	})})
	if _, err := service.BeginBrowserAuth(t.Context(), "codex"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
	session, _ := service.BeginBrowserAuth(t.Context(), "cursor")
	private := service.browserSessions[session.SessionID]
	service.now = func() time.Time { return private.ExpiresAt }
	if _, err := service.PollBrowserAuth(t.Context(), BrowserAuthPoll{Provider: "cursor", SessionID: session.SessionID}); err == nil {
		t.Fatal("expired session accepted")
	}
	if len(service.browserSessions) != 0 {
		t.Fatal("expired session retained")
	}
}

func TestCursorLoginMatchingDoesNotReplaceOtherProviders(t *testing.T) {
	candidate := storedAccount{Provider: providerCursor, Email: stringPointer("person@example.com"), AuthData: authData{AccountID: stringPointer("cursor-user")}}
	accounts := []storedAccount{{Provider: providerCodex, Email: candidate.Email}, {Provider: providerCursor, AuthData: candidate.AuthData}}
	if index := matchingLogin(accounts, candidate, false); index != 1 {
		t.Fatalf("matched wrong account %d", index)
	}
}
