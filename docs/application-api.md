# Application API

Protocol version **2.0** separates paired network clients from local provider administration. The server owns provider credentials, native CLI profiles, the shared account catalog, and usage cache. Public account results contain no provider API keys, access tokens, refresh tokens, native executable arguments, or environment variables.

## HTTP transport and device authentication

Use the server's Tailscale HTTPS origin. The default listener is `127.0.0.1:8787`; Tailscale Serve forwards tailnet HTTPS to it. See [server setup](../README.md#set-up-the-homelab-server). Explicit loopback/Tailscale IPs also permit HTTP inside those networks.

Create a pairing code locally with `check-usage server pair --name laptop`. A code expires after ten minutes and can be redeemed once, including under concurrent requests. Add `--allow-account-changes` to grant shared rename/removal permission. Provider sign-in and source configuration always remain local server operations.

| Endpoint | Authentication | Request / response |
| --- | --- | --- |
| `POST /v1/pair` | One-use pairing code | `{"code":"..."}` → `{"server_id":"...","token":"...","device":{...}}` |
| `GET /v1/info` | `Authorization: Bearer DEVICE_TOKEN` | Server ID, protocol version, public device metadata and access |
| `POST /v1/rpc` | `Authorization: Bearer DEVICE_TOKEN` | One JSON-RPC 2.0 request → one response |

Send JSON bodies with `Content-Type: application/json`. Tokens belong in the authorization header. Query parameters and browser `Origin` headers are rejected. Requests are limited to 1 MiB; the bundled clients bound responses to 4 MiB and refuse redirects. HTTP responses include `Cache-Control: no-store` and `X-Check-Usage-Server-ID`; clients retain the paired server ID and require explicit reconnection if it changes.

The server stores a SHA-256 hash of each random device token in private state files. Devices store the token in an owner-only CLI connection file or Android Keystore-encrypted storage. `check-usage server devices list` lists devices without token hashes; `devices revoke ID` revokes just that device on subsequent requests. Keep the server state directory persistent across restarts.

## Network methods

Every HTTP JSON-RPC request requires `"jsonrpc":"2.0"` and an `id`. Notifications and batches are not supported over HTTP. Allowed calls use the usual JSON-RPC result/error envelope; denied methods return HTTP 403. Missing, invalid, or revoked device credentials return HTTP 401.

| Method | Params | Permission / result |
| --- | --- | --- |
| `rpc.discover` | `{}` | Read: protocol version, server ID, network method names and device access |
| `providers.list` | `{}` | Read: provider IDs, names and reset-credit capability |
| `accounts.list` | `{}` | Read: public account array |
| `usage.get` | `{"account":"optional id/name/email","refresh":true}` | Read: one result per selected account; omitting `account` selects all |
| `resets.get` | `{"account":"id/name/email","refresh":true}` | Read: available reset-credit details |
| `accounts.rename` | `{"account":"id/name/email","new_name":"..."}` | Write: mutation and public account |
| `accounts.remove` | `{"account":"id/name/email"}` | Write: mutation and removed public account |

Read-only is the default. The explicit `write` capability permits only shared account rename/removal. Network clients cannot access provider authentication, API-key saving, native process configuration, local settings, or reset claiming. `rpc.discover` lists the network interface and reports `access` so clients can disable unsupported account actions.

### Usage schema and refresh behavior

`refresh` defaults to `true`. With `false`, usage and reset reads use the server cache without provider network access or native CLI probes. With `true`, calls still respect minimum intervals and backoff. The server uses at least one minute between provider reads; Claude and native Antigravity use at least five minutes. Overlapping usage requests for an account share the cache. Slow usage probes do not hold the account catalog lock, and up to four accounts refresh concurrently.

Each `UsageResult` contains:

- `account`: public identity and plan metadata.
- `metrics`: percentage or text metrics, each with `kind`, `slot`, and `label`; optional `scope`, `used_percent`, `reset_at`, and `text`.
- `snapshot`: optional usage windows and reset-credit summary.
- `fetched_at_epoch_millis`: time of the last successful measurement, when known.
- `retry_at_epoch_millis`: earliest next refresh, when set.
- `source`: `native_cli`, `api`, or `api_fallback`, when known.
- `cached`: whether cached measurements were used.
- `error`: per-account failure, when present. Successful accounts remain in the same response. Provider diagnostics are replaced with recovery messages on the network transport.

A missing `used_percent` means unknown; zero means measured zero use. Reset timestamps use Unix **seconds**. Fetch/retry timestamps use Unix **milliseconds**. Slots (`session`, `weekly`, `monthly`) are display placements and can repeat. A `scope` identifies a model family or pool; inspect its label rather than assuming the slot describes the pool's actual duration.

For Antigravity, native `agy -p /usage --output-format json` supplies both Gemini and Claude/GPT families, each with its reported five-hour and weekly windows. Preserve all four metrics, keyed by scope and slot rather than by slot alone. Weekly-only reports do not acquire invented five-hour limits. Direct Google credential import and quota request emulation are not supported. Register native sources through local `server accounts native` administration; see [native adapters](../README.md#multiple-profiles-and-other-native-adapters).

### CLI calls to the paired server

```bash
check-usage api accounts.list
check-usage api usage.get '{"refresh":true}'
check-usage api resets.get '{"account":"My Account"}'
check-usage api --connection-file ./client.json rpc.discover
```

The remote `api` command accepts `--connection-file` before the method. Use `-` as the params argument to read one JSON value from stdin:

```bash
printf '%s' '{"account":"My Account","new_name":"Work"}' \
  | check-usage api accounts.rename -
```

Successful calls print a JSON-RPC response and exit 0. RPC/application failures exit 1; invalid command syntax exits 2.

## Local administration API

On the server, Go applications can import `github.com/McMelonTV/check-usage/usageapi`, or spawn the local NDJSON process:

```bash
check-usage server api serve
check-usage server --config ./server.json api serve
```

Stop the HTTP server before running local credential/source mutation commands against its accounts file, and restart afterward. Separate service processes do not coordinate account/cache writes. Pairing/revocation use separate state and can run while the HTTP server is active.

The local process accepts one JSON-RPC request per line and returns one response per line:

```json
{"jsonrpc":"2.0","id":1,"method":"accounts.list","params":{}}
{"jsonrpc":"2.0","id":2,"method":"usage.get","params":{"refresh":false}}
```

Local notifications without an `id` produce no response. Batches are not supported. Standard JSON-RPC codes cover parsing/request/method/parameter errors; application errors use `-32000` and a human-readable `error.data` string. Local diagnostics can contain provider details and should remain private.

One-shot local calls use `check-usage server api [flags] <method> [params-json|-]`. Supported flags are `--accounts-file`, `--cache-dir`, `--timeout`, and `--pretty`; flags precede the method. Secrets should come through stdin or private files rather than command arguments.

The local interface exposes `rpc.discover`, account list/rename/removal, usage, and resets, plus these administration methods. `providers.list` belongs to the HTTP interface.

| Local-only method | Params | Result |
| --- | --- | --- |
| `accounts.api_key.save` | `{"account":"optional id/name","provider":"opencode-go/deepseek/cursor","api_key":"...","name":"optional"}` | Public account mutation |
| `auth.device.begin` | `{"provider":"codex"}` | Session ID, user code, verification URL and polling interval |
| `auth.device.poll` | `{"provider":"codex","session_id":"...","user_code":"...","name":"optional"}` | `pending`, or `complete` with persisted public account |
| `auth.oauth.begin` | `{"provider":"claude"}` | Session ID and authorization URL |
| `auth.oauth.complete` | `{"provider":"claude","session_id":"...","code":"...","name":"optional"}` | `complete` with persisted public account |
| `auth.browser.begin` | `{"provider":"cursor"}` | Session ID, verification URL and polling interval |
| `auth.browser.poll` | `{"provider":"cursor","session_id":"...","name":"optional","account":"optional id/name/email"}` | `pending`, or `complete` with persisted public account |
| `settings.get` | `{}` | Current local settings |
| `settings.set` | Complete settings object | Normalized, persisted local settings |

### Cursor login and metrics

1. Call `auth.browser.begin` with `provider: "cursor"`.
2. Show or open the returned `verification_url`.
3. Poll `auth.browser.poll` with the session ID no faster than `poll_interval_seconds`.
4. Stop when status is `complete`. The service creates a revocable 90-day API key and persists it with a bearer token; browser refresh tokens are discarded.

Use the same `Service` instance or persistent `server api serve` process for both calls. The service keeps the PKCE verifier in memory and never returns it. Sessions expire after 20 minutes and cannot be replayed after completion. One-shot API commands cannot span this login flow. To reauthenticate an existing account, include `account` in the poll request; the signed-in identity must match.

An existing user API key can be saved with `accounts.api_key.save` and `provider: "cursor"`. Public metadata reports `auth_type: "api_key"`. Replacing credentials invalidates cached usage. These authentication methods are available only through the local administration API.

Cursor `usage.get` responses put both `Cursor models` and `Other models` in the `monthly` slot (`kind: "percentage"`), distinguished by `scope`. Clients render independent pool rows under one account, preserving both metrics even though their slots match. Both use the monthly billing-cycle `reset_at` when available. Previously cached pools in the weekly slot are normalized to monthly. On-demand spending is not reported. Missing percentages remain unknown; exhausting one pool does not block the other. Cursor has no session/weekly window and does not support `resets.get`.

### Codex provider authentication

1. Call `auth.device.begin` with `provider: "codex"`.
2. Show or open `verification_url` and display `user_code`.
3. Poll `auth.device.poll` with the same provider no faster than `poll_interval_seconds`.
4. Stop when the returned status is `complete`. The service exchanges the authorization code and saves the credentials itself.

### Browser code authentication

Claude accounts sign in with a pasted authorization code instead of a device code.

1. Call `auth.oauth.begin` with `provider: "claude"`.
2. Open `verification_url`. After the user approves, claude.com shows a code in the form `code#state`.
3. Call `auth.oauth.complete` with the same `session_id` and the pasted `code`. The service exchanges it, saves the credentials, and returns the public account.

The `session_id` contains the PKCE verifier for this login, so keep it private to the calling process and discard it afterward.

### Reset credits

Codex and Claude accounts support `resets.get` when their source reports the details, and their `usage.get` results include a `snapshot` whose `credits.available_count` is the number of resets usable now. For Claude these are limit resets (grants that refill the 5-hour or weekly limit early). They arrive in the same rate-limited usage response as the metrics, so Claude needs no extra request, and a `resets.get` refresh within the usage throttle returns the cached credits with `cached: true`. Each Claude reset is one credit: `title` is the grant label, and `granted_at` and `expires_at` are the grant's start and end. Confirmed ineligible accounts report zero credits. A missing reset-status block or a refused client surface reports reset credits as unavailable and preserves any previously cached balance. Native Codex exposes the available count and individual credits but may omit `total_earned_count`; an omitted total means unknown. Native sources without reset details return an unavailable error. Claiming resets is not supported.

## Go package

Go callers can skip JSON entirely:

```go
package main

import (
    "context"
    "log"

    "github.com/McMelonTV/check-usage/usageapi"
)

func main() {
    service := usageapi.New(usageapi.Config{
        AccountsFile: "./accounts.json",
        CacheDir:     "./cache",
    })

    accounts, err := service.ListAccounts()
    if err != nil {
        log.Fatal(err)
    }
    usage, err := service.Usage(context.Background(), "", true)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("accounts=%d usage-results=%d", len(accounts), len(usage))
}
```

The main entry points are `Service.ListAccounts`, `RenameAccount`, `RemoveAccount`, `SaveAPIKeyAccount`, `SaveNativeAccount`, `BeginDeviceAuth`, `PollDeviceAuth`, `BeginBrowserAuth`, `PollBrowserAuth`, `BeginOAuthAuth`, `CompleteOAuthAuth`, `Usage`, `ResetCredits`, `Settings`, and `UpdateSettings`. A custom `http.Client`, clock, accounts path, cache directory, and user agent can be supplied through `usageapi.Config` for embedding and testing.

Read results use only `usageapi` types (`UsageResult`, `Metric`, `UsageSnapshot`, `ResetCredits`, and so on), so they can be stored and passed around by name. Provider clients live under `internal/` and are not part of the Go API.
