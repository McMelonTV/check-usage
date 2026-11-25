package remote

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientDoesNotForwardCredentialsThroughRedirects(t *testing.T) {
	var reached atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := New(Config{URL: server.URL, Token: "device-secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Info(t.Context()); err == nil || reached.Load() {
		t.Fatal("client accepted or followed a redirect")
	}
	if _, err := Pair(t.Context(), server.URL, "pairing-secret", server.Client()); err == nil || reached.Load() {
		t.Fatal("pairing followed a redirect")
	}
}

func TestClientRejectsChangedServerIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Check-Usage-Server-ID", "replacement-server")
		_, _ = w.Write([]byte(`{"server_id":"replacement-server","protocol_version":"2.0"}`))
	}))
	defer server.Close()
	client, err := New(Config{URL: server.URL, Token: "device-secret", ServerID: "paired-server"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Info(t.Context()); err == nil {
		t.Fatal("accepted a different server")
	}
	if _, err := client.Accounts(t.Context()); err == nil {
		t.Fatal("RPC accepted a different server")
	}
}

func TestServerURLValidation(t *testing.T) {
	for _, address := range []string{"https://homelab.example.ts.net", "http://100.101.102.103:8787", "http://[fd7a:115c:a1e0::1]:8787", "http://127.0.0.1:8787"} {
		if _, err := ValidateURL(address); err != nil {
			t.Fatalf("valid origin %q: %v", address, err)
		}
	}
	for _, address := range []string{"http://public.example.com", "http://127.attacker.example", "https://user:secret@example.com", "https://server/path", "https://server?token=secret", "https://server/#fragment"} {
		if _, err := ValidateURL(address); err == nil {
			t.Fatalf("accepted unsafe origin %q", address)
		}
	}
}
