package usageapi

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRPCRejectsUnknownParams(t *testing.T) {
	server := RPCServer{Service: testService(t, nil)}
	response := server.Handle(context.Background(), RPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "accounts.list",
		Params: json.RawMessage(`{"unexpected":true}`),
	})
	if response.Error == nil || response.Error.Code != -32602 {
		t.Fatalf("unknown param response = %#v", response)
	}

	response = server.Handle(context.Background(), RPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "accounts.remove",
		Params: json.RawMessage(`{"account":"one","unexpected":true}`),
	})
	if response.Error == nil || response.Error.Code != -32602 {
		t.Fatalf("unknown param response = %#v", response)
	}
}

func TestRPCServeUsesOneLinePerResponseAndHonorsNotifications(t *testing.T) {
	server := RPCServer{Service: testService(t, nil)}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"one","method":"rpc.discover"}`,
		`{"jsonrpc":"2.0","method":"accounts.list"}`,
		`not-json`,
	}, "\n")
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses = %d, want 2: %s", len(lines), output.String())
	}
	var discover RPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &discover); err != nil || discover.Error != nil {
		t.Fatalf("discover response = %#v, error = %v", discover, err)
	}
	var parseError RPCResponse
	if err := json.Unmarshal([]byte(lines[1]), &parseError); err != nil || parseError.Error == nil || parseError.Error.Code != -32700 {
		t.Fatalf("parse response = %#v, error = %v", parseError, err)
	}
}

func TestRPCSettingsRoundTrip(t *testing.T) {
	server := RPCServer{Service: testService(t, nil)}
	response := server.Handle(context.Background(), RPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "settings.set",
		Params: json.RawMessage(`{
			"usage_display":"remaining","bar_fill":"right","percentage_position":"left",
			"color_theme":"colorblind","auto_refresh_seconds":300,"compact_mode":true
		}`),
	})
	if response.Error != nil {
		t.Fatalf("settings.set failed: %#v", response.Error)
	}
	settings, err := server.Service.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.UsageDisplay != "remaining" || settings.AutoRefreshSeconds != 300 || !settings.CompactMode {
		t.Fatalf("settings did not round-trip: %#v", settings)
	}
}

func TestRPCSavesAPIKeyAccount(t *testing.T) {
	server := RPCServer{Service: testService(t, nil)}
	response := server.Handle(context.Background(), RPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "accounts.api_key.save",
		Params: json.RawMessage(`{"provider":"deepseek","api_key":"secret"}`),
	})
	if response.Error != nil {
		t.Fatalf("accounts.api_key.save failed: %#v", response.Error)
	}
	accounts, err := server.Service.ListAccounts()
	if err != nil || len(accounts) != 1 || accounts[0].Provider != providerDeepSeek {
		t.Fatalf("accounts = %#v, %v", accounts, err)
	}
}
