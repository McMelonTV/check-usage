package remoteserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/usageapi"
)

func TestProviderDiagnosticsAreRedactedBeforeNetworkResponses(t *testing.T) {
	dir := t.TempDir()
	config := Config{ServerID: "test", StateDir: filepath.Join(dir, "state")}
	service := usageapi.New(usageapi.Config{AccountsFile: filepath.Join(dir, "accounts.json"), CacheDir: filepath.Join(dir, "cache"), NativeFetch: func(context.Context, string, nativecli.Config) (nativecli.Result, error) {
		return nativecli.Result{}, errors.New("provider echoed credential-secret")
	}})
	account, err := service.SaveNativeAccount(usageapi.NativeAccount{Provider: providers.Codex, Config: nativecli.Config{Driver: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&Handler{Config: config, Service: service})
	defer server.Close()
	pairing, err := CreatePairing(config, "phone", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	paired, err := remote.Pair(t.Context(), server.URL, pairing.Code, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client, err := remote.New(paired, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.Usage(t.Context(), account.Account.ID, true)
	if err != nil || len(results) != 1 || results[0].Error == "" || strings.Contains(results[0].Error, "credential-secret") {
		t.Fatalf("provider diagnostics leaked or failure disappeared: %#v, %v", results, err)
	}
	if _, err := client.Resets(t.Context(), account.Account.ID); err == nil || strings.Contains(err.Error(), "credential-secret") {
		t.Fatal("reset diagnostics leaked or failure disappeared")
	}
}

func TestPairedDevicesSyncAccountsWithoutReceivingCredentials(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	config := Config{ServerID: "test-server", AccountsFile: filepath.Join(dir, "accounts.json"), CacheDir: filepath.Join(dir, "cache"), StateDir: filepath.Join(dir, "state")}
	var reads atomic.Int32
	service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir, MinRefreshInterval: time.Minute, NativeFetch: func(context.Context, string, nativecli.Config) (nativecli.Result, error) {
		reads.Add(1)
		return nativecli.Result{Usage: providers.Usage{Plan: "Pro", Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: new(float64(25))}}}}, nil
	}})
	added, err := service.SaveNativeAccount(usageapi.NativeAccount{Provider: providers.Antigravity, Name: "Google", Config: nativecli.Config{Driver: "agy", Env: map[string]string{"PRIVATE_CONFIG": "server-secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&Handler{Config: config, Service: service})
	defer server.Close()
	newClient := func(name string, write bool) *remote.Client {
		t.Helper()
		pairing, err := CreatePairing(config, name, write, now)
		if err != nil {
			t.Fatal(err)
		}
		paired, err := remote.Pair(t.Context(), server.URL, pairing.Code, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := remote.Pair(t.Context(), server.URL, pairing.Code, server.Client()); err == nil {
			t.Fatal("one-time code was replayed")
		}
		client, err := remote.New(paired, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	phone, laptop := newClient("phone", false), newClient("laptop", true)
	info, err := phone.Info(t.Context())
	if err != nil || info.Device.Access != "read" {
		t.Fatal("missing device identity or default read scope")
	}
	var wg sync.WaitGroup
	for _, client := range []*remote.Client{phone, laptop} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := client.Usage(t.Context(), "", true)
			if err != nil || len(results) != 1 || len(results[0].Metrics) != 1 {
				t.Errorf("usage did not synchronize: %v", err)
			}
		}()
	}
	wg.Wait()
	if reads.Load() != 1 {
		t.Fatal("multiple devices launched duplicate native probes")
	}
	if err := phone.Call(t.Context(), "accounts.rename", map[string]string{"account": added.Account.ID, "new_name": "Renamed"}, nil); err == nil {
		t.Fatal("read-only device renamed shared account")
	}
	if err := laptop.Call(t.Context(), "accounts.rename", map[string]string{"account": added.Account.ID, "new_name": "Renamed"}, nil); err != nil {
		t.Fatal(err)
	}
	accounts, err := phone.Accounts(t.Context())
	if err != nil || accounts[0].Name != "Renamed" {
		t.Fatal("rename did not reach other device")
	}
	for _, method := range []string{"auth.device.begin", "accounts.api_key.save", "accounts.oauth.import", "native.exec", "settings.set"} {
		if err := laptop.Call(t.Context(), method, map[string]string{"command": "secret-command"}, nil); err == nil {
			t.Fatalf("network device accessed local-only method %s", method)
		}
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"accounts.list","params":{}}`))
	request.Header.Set("Authorization", "Bearer "+phone.Config.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if strings.Contains(string(body), "server-secret") || strings.Contains(string(body), "native_cli\"") && strings.Contains(string(body), "env") {
		t.Fatal("public metadata exposed process configuration")
	}
	devices, err := ListDevices(config)
	if err != nil || len(devices) != 2 || devices[0].TokenHash != "" {
		t.Fatal("device listing exposed token hashes")
	}
	files, _ := filepath.Glob(filepath.Join(config.StateDir, "devices", "*.json"))
	for _, file := range files {
		data, _ := os.ReadFile(file)
		if strings.Contains(string(data), phone.Config.Token) || strings.Contains(string(data), laptop.Config.Token) {
			t.Fatal("server stored a raw device token")
		}
	}
	if err := RevokeDevice(config, info.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.Accounts(t.Context()); err == nil {
		t.Fatal("revoked device retained access")
	}
	if _, err := laptop.Accounts(t.Context()); err != nil {
		t.Fatal("revoking phone revoked laptop")
	}
	if err := laptop.Call(t.Context(), "accounts.remove", map[string]string{"account": added.Account.ID}, nil); err != nil {
		t.Fatal(err)
	}
	accounts, err = laptop.Accounts(t.Context())
	if err != nil || len(accounts) != 0 {
		t.Fatal("removal did not synchronize")
	}
}

func TestPairingExpirationAndConcurrentRedemption(t *testing.T) {
	config := Config{ServerID: "test", StateDir: t.TempDir()}
	now := time.Now()
	expired, err := CreatePairing(config, "expired", false, now.Add(-11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := redeemPairing(config, expired.Code, now); err == nil {
		t.Fatal("expired code accepted")
	}
	pairing, _ := CreatePairing(config, "phone", false, now)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := redeemPairing(config, pairing.Code, now); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("pairing was not single-use under concurrency")
	}
	if _, ok := authenticate(config, "../../file.token"); ok {
		t.Fatal("invalid device token accepted")
	}
}

func TestHTTPRejectsOriginsQueriesAndMissingAuthentication(t *testing.T) {
	handler := &Handler{Config: Config{ServerID: "test", StateDir: t.TempDir()}}
	for _, test := range []struct {
		path, origin string
		want         int
	}{{"/v1/info", "", 401}, {"/v1/info?token=secret", "", 400}, {"/v1/pair", "https://other.invalid", 403}} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.origin != "" {
			request.Header.Set("Origin", test.origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		if w.Code != test.want || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("authentication boundary failed")
		}
	}
}
func TestPairHandoffPageAndLocalDeviceProvisioning(t *testing.T) {
	dir := t.TempDir()
	config := Config{
		ServerID:     "pair-page-server",
		Listen:       "127.0.0.1:8787",
		AccountsFile: filepath.Join(dir, "accounts.json"),
		CacheDir:     filepath.Join(dir, "cache"),
		StateDir:     filepath.Join(dir, "state"),
	}
	service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir})
	server := httptest.NewServer(&Handler{Config: config, Service: service})
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/pair")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET /pair status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "check-usage://pair?") || !strings.Contains(string(body), "ing.boykiss.aiusagewidgets") {
		t.Fatalf("GET /pair missing expected deep link handoff:\n%s", string(body))
	}

	badResp, err := server.Client().Get(server.URL + "/pair?code=leak")
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /pair?code=leak status=%d, want 400", badResp.StatusCode)
	}

	localURL, err := LocalLoopbackURL(":8787")
	if err != nil || localURL != "http://127.0.0.1:8787" {
		t.Fatalf("LocalLoopbackURL(:8787) = (%q, %v)", localURL, err)
	}
	paired, err := ProvisionLocalDevice(config, "local (kube)", true, time.Now())
	if err != nil {
		t.Fatalf("ProvisionLocalDevice() error = %v", err)
	}
	client, err := remote.New(remote.Config{URL: server.URL, Token: paired.Token, ServerID: paired.ServerID}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(t.Context())
	if err != nil || info.Device.Access != "write" || info.Device.Name != "local (kube)" {
		t.Fatalf("unexpected local provisioned device info: %#v err=%v", info, err)
	}
}
