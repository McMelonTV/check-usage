# Application API

`check-usage` exposes the same account, authentication, usage, reset-credit, and settings operations through two non-HTTP interfaces:

- Go applications can import `github.com/McMelonTV/check-usage/usageapi`.
- Applications in any language can spawn `check-usage api serve` and exchange newline-delimited JSON-RPC 2.0 messages over stdin/stdout.

The protocol version is `1.4`. Call `rpc.discover` to inspect the methods supported by the installed binary. Account and authentication results never include API keys, access tokens, refresh tokens, or ID tokens. Credentials remain in the configured `accounts.json` file.

## One-shot JSON commands

The simplest integration is one process per request:

```bash
check-usage api accounts.list
check-usage api usage.get '{"refresh":true}'
check-usage api resets.get '{"account":"My Account","include_unavailable":true}'
check-usage api --pretty settings.get
```

Use `-` as the params argument to read one JSON value from stdin:

```bash
printf '%s' '{"account":"My Account","new_name":"Work"}' \
  | check-usage api accounts.rename -
```

Flags must precede the method. The supported flags are `--accounts-file`, `--cache-dir`, `--timeout`, and `--pretty`. A successful RPC response exits with status 0, an RPC/application error with status 1, and invalid command syntax with status 2.

## Persistent NDJSON RPC

For multiple calls, keep one process alive:

```bash
check-usage api --accounts-file ./accounts.json serve
```

Write exactly one JSON-RPC request per line:

```json
{"jsonrpc":"2.0","id":1,"method":"accounts.list","params":{}}
{"jsonrpc":"2.0","id":2,"method":"usage.get","params":{"refresh":false}}
```

The process writes exactly one response per line. Requests without `id` are notifications and produce no response. JSON-RPC batches are not supported. Standard JSON-RPC error codes are used for parsing, request, method, and parameter errors; application failures use `-32000` with a human-readable string in `error.data`.

Keep a single RPC process responsible for a given accounts file when possible. Service calls are synchronized within one process, but separate processes do not coordinate concurrent writes to the same file.

## Methods

| Method | Params | Result |
| --- | --- | --- |
| `rpc.discover` | `{}` | Protocol version and method descriptions |
| `accounts.list` | `{}` | Public account array |
| `accounts.rename` | `{"account":"id/name/email","new_name":"..."}` | Mutation and public account |
| `accounts.remove` | `{"account":"id/name/email"}` | Mutation and removed public account |
| `accounts.api_key.save` | `{"account":"optional id/name","provider":"opencode-go/deepseek/cursor","api_key":"...","name":"optional"}` | Creates or updates an API-key account and returns public metadata |
| `auth.device.begin` | `{"provider":"codex"}` | Session ID, user code, verification URL, and polling interval |
| `auth.device.poll` | `{"provider":"codex","session_id":"...","user_code":"...","name":"optional"}` | `pending`, or `complete` with the persisted public account |
| `auth.oauth.begin` | `{"provider":"claude"}` | Session ID and the authorization URL to open |
| `auth.oauth.complete` | `{"provider":"claude","session_id":"...","code":"...","name":"optional"}` | `complete` with the persisted public account |
| `auth.browser.begin` | `{"provider":"cursor"}` | Session ID, verification URL, and polling interval |
| `auth.browser.poll` | `{"provider":"cursor","session_id":"...","name":"optional","account":"optional id/name/email"}` | `pending`, or `complete` with the persisted public account |
| `usage.get` | `{"account":"optional","refresh":true}` | One result per selected account with typed provider metrics; omitting `account` selects all |
| `resets.get` | `{"account":"...","refresh":true,"include_unavailable":false}` | Reset-credit payload for one account |
| `settings.get` | `{}` | Current settings |
| `settings.set` | Complete settings object | Normalized, persisted settings |

`refresh` defaults to `true`. With `false`, usage and reset methods perform no network access and return the cache used by the CLI dashboard. When refreshing every account, a provider failure is returned in that account's `error` field so successful accounts are not discarded.

Provider metrics are returned in `UsageResult.metrics`. Percentage metrics include `used_percent` and optional `reset_at`. A metric with a `scope` field is a provider-specific limit placed in a shared slot for display, such as Claude's `Fable weekly` limit in `monthly` or Cursor's pools; it is not that slot's own window. Every browser sign-in begin method returns the link to open as `verification_url`. API keys are accepted as input only and are never returned.

### Cursor login and metrics

1. Call `auth.browser.begin` with `provider: "cursor"`.
2. Show or open the returned `verification_url`.
3. Poll `auth.browser.poll` with the session ID no faster than `poll_interval_seconds`.
4. Stop when status is `complete`. The service creates a revocable 90-day API key and persists it with a bearer token; browser refresh tokens are discarded.

Use the same `Service` instance or persistent `api serve` process for both calls. The service keeps the PKCE verifier in memory and never returns it. Sessions expire after 20 minutes and cannot be replayed after completion. One-shot API commands cannot span this login flow. To reauthenticate an existing account, include `account` in the poll request; the signed-in identity must match.

An existing user API key can be saved with `accounts.api_key.save` and `provider: "cursor"`. Public metadata reports `auth_type: "api_key"`. Replacing credentials invalidates cached usage. See [Cursor setup](../README.md#cursor-setup) for protocol details.

Cursor `usage.get` responses put `Cursor models` in the `weekly` slot and `Other models` in the `monthly` slot (`kind: "percentage"`), each with a `scope` naming what it really is; Cursor has no `session` metric. Both use the monthly billing-cycle `reset_at` when available. On-demand spending is not reported. A missing percentage is omitted rather than reported as zero. The pools are independent and are not session or weekly windows; the slot only places them in a column. Cursor does not support `resets.get`.

### Device authentication

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

The main entry points are `Service.ListAccounts`, `RenameAccount`, `RemoveAccount`, `SaveAPIKeyAccount`, `BeginDeviceAuth`, `PollDeviceAuth`, `BeginBrowserAuth`, `PollBrowserAuth`, `Usage`, `ResetCredits`, `Settings`, and `UpdateSettings`. A custom `http.Client`, clock, accounts path, cache directory, and user agent can be supplied through `usageapi.Config` for embedding and testing.
