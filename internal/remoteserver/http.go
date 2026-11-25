package remoteserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/usageapi"
)

type Handler struct {
	Config  Config
	Service *usageapi.Service
	Now     func() time.Time
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Check-Usage-Server-ID", handler.Config.ServerID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/pair" || r.URL.Path == "/pair/" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "pairing codes must be passed in the URL fragment (#code=...)", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.WriteString(w, pairHandoffHTML)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser-origin requests are not supported", http.StatusForbidden)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "query parameters are not supported", http.StatusBadRequest)
		return
	}
	now := time.Now()
	if handler.Now != nil {
		now = handler.Now()
	}
	if r.URL.Path == "/v1/pair" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var input struct {
			Code string `json:"code"`
		}
		if decodeBody(w, r, &input) != nil {
			http.Error(w, "invalid pairing request", 400)
			return
		}
		result, err := redeemPairing(handler.Config, input.Code, now)
		if err != nil {
			http.Error(w, "invalid or expired pairing code", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(401)
		return
	}
	device, ok := authenticate(handler.Config, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(401)
		return
	}
	if r.URL.Path == "/v1/info" && r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{"server_id": handler.Config.ServerID, "protocol_version": usageapi.ProtocolVersion, "device": device})
		return
	}
	if r.URL.Path != "/v1/rpc" {
		w.WriteHeader(404)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var request usageapi.RPCRequest
	if decodeBody(w, r, &request) != nil {
		http.Error(w, "invalid JSON-RPC request", 400)
		return
	}
	read := map[string]bool{"rpc.discover": true, "providers.list": true, "accounts.list": true, "usage.get": true, "resets.get": true}
	write := map[string]bool{"accounts.rename": true, "accounts.remove": true}
	if !read[request.Method] && (!write[request.Method] || device.Access != "write") {
		http.Error(w, "method is not permitted for this device", 403)
		return
	}
	if request.JSONRPC != "2.0" || len(request.ID) == 0 {
		http.Error(w, "JSON-RPC 2.0 with an id is required", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if request.Method == "providers.list" {
		definitions := []map[string]any{}
		for _, definition := range providers.Definitions() {
			definitions = append(definitions, map[string]any{"id": definition.ID, "name": definition.Name, "reset_credits": definition.SupportsResetCredits})
		}
		_ = json.NewEncoder(w).Encode(usageapi.RPCResponse{JSONRPC: "2.0", ID: request.ID, Result: definitions})
		return
	}
	if request.Method == "rpc.discover" {
		_ = json.NewEncoder(w).Encode(usageapi.RPCResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{"protocol_version": usageapi.ProtocolVersion, "server_id": handler.Config.ServerID, "methods": []string{"providers.list", "accounts.list", "usage.get", "resets.get", "accounts.rename", "accounts.remove"}, "access": device.Access}})
		return
	}
	response := (usageapi.RPCServer{Service: handler.Service}).Handle(ctx, request)
	// Legacy provider errors can include reflected upstream bodies. Network
	// clients get a useful recovery message, never raw provider diagnostics.
	if request.Method == "usage.get" {
		if results, ok := response.Result.([]usageapi.UsageResult); ok {
			for i := range results {
				if results[i].Error != "" {
					results[i].Error = "Provider usage unavailable; check its login or adapter on the server."
				}
			}
			response.Result = results
		}
	}
	if response.Error != nil && request.Method == "resets.get" {
		response.Error.Data = "Reset credits unavailable; check this account on the server."
	}
	_ = json.NewEncoder(w).Encode(response)
}
func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return errors.New("JSON content type required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("only one JSON value is allowed")
	}
	return nil
}

const pairHandoffHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Pair AI Usage Widgets</title>
<style>
  :root { color-scheme: dark light; }
  body { font-family: system-ui, -apple-system, sans-serif; margin: 0; min-height: 100vh; display: grid; place-items: center; background: #111318; color: #e8ecf1; padding: 24px; box-sizing: border-box; }
  main { max-width: 420px; width: 100%; background: #1a1e26; border: 1px solid #2b3140; border-radius: 16px; padding: 24px; box-sizing: border-box; }
  h1 { font-size: 1.25rem; margin: 0 0 8px; }
  p { font-size: 0.95rem; line-height: 1.45; color: #b5bfd0; margin: 0 0 16px; }
  .actions { display: flex; flex-direction: column; gap: 10px; }
  a.btn, button.btn { display: block; text-align: center; text-decoration: none; font: inherit; font-weight: 600; padding: 12px 16px; border-radius: 10px; border: 0; cursor: pointer; }
  a.primary { background: #7aa2f7; color: #0e1117; }
  button.secondary { background: #262c3a; color: #e8ecf1; }
  .hint { font-size: 0.85rem; margin-top: 12px; color: #8c98ad; word-break: break-all; }
</style>
</head>
<body>
<main>
  <h1>Pair device</h1>
  <p id="status">Opening AI Usage Widgets...</p>
  <div class="actions">
    <a id="open" class="btn primary" href="#" hidden>Open AI Usage Widgets</a>
    <button id="copy" class="btn secondary" type="button" hidden>Copy pairing link</button>
  </div>
  <p id="hint" class="hint"></p>
</main>
<script>
(function () {
  var hash = window.location.hash ? window.location.hash.slice(1) : "";
  var code = new URLSearchParams(hash).get("code") || "";
  var status = document.getElementById("status");
  var openBtn = document.getElementById("open");
  var copyBtn = document.getElementById("copy");
  var hint = document.getElementById("hint");
  if (!code) {
    status.textContent = "This pairing link is missing its one-time code. Run 'cu server pair' on your homelab to generate a new link.";
    return;
  }
  var origin = window.location.origin;
  var query = "url=" + encodeURIComponent(origin) + "&code=" + encodeURIComponent(code);
  var deepLink = "check-usage://pair?" + query;
  var launchLink = /Android/i.test(navigator.userAgent)
    ? "intent://pair?" + query + "#Intent;scheme=check-usage;package=ing.boykiss.aiusagewidgets;end"
    : deepLink;
  openBtn.href = launchLink;
  openBtn.hidden = false;
  copyBtn.hidden = false;
  hint.textContent = "Server: " + origin;
  copyBtn.addEventListener("click", function () {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(deepLink).then(function () {
        copyBtn.textContent = "Copied pairing link";
      });
    } else {
      window.prompt("Copy pairing link:", deepLink);
    }
  });
  status.textContent = "Tap below if AI Usage Widgets does not open automatically.";
  window.location.href = launchLink;
})();
</script>
</body>
</html>
`

