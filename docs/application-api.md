# Application API

`codex-usage` exposes the same account, authentication, usage, reset-credit, and settings operations through two non-HTTP interfaces:

- Go applications can import `github.com/McMelonTV/codex-usage/usageapi`.
- Applications in any language can spawn `codex-usage api serve` and exchange newline-delimited JSON-RPC 2.0 messages over stdin/stdout.

The protocol version is `1.0`. Call `rpc.discover` to inspect the methods supported by the installed binary. Account and authentication results never include access tokens, refresh tokens, or ID tokens. Credentials remain in the configured `accounts.json` file.

## One-shot JSON commands

The simplest integration is one process per request:

```bash
codex-usage api accounts.list
codex-usage api usage.get '{"refresh":true}'
codex-usage api resets.get '{"account":"My Account","include_unavailable":true}'
codex-usage api --pretty settings.get
```

Use `-` as the params argument to read one JSON value from stdin:

```bash
printf '%s' '{"account":"My Account","new_name":"Work"}' \
  | codex-usage api accounts.rename -
```

Flags must precede the method. The supported flags are `--accounts-file`, `--cache-dir`, `--timeout`, and `--pretty`. A successful RPC response exits with status 0, an RPC/application error with status 1, and invalid command syntax with status 2.

## Persistent NDJSON RPC

For multiple calls, keep one process alive:

```bash
codex-usage api --accounts-file ./accounts.json serve
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
| `accounts.api_key.save` | `{"account":"optional id/name","provider":"opencode-go/deepseek","api_key":"...","name":"optional"}` | Creates or updates an API-key account and returns public metadata |
| `auth.device.begin` | `{"provider":"openai-codex"}` | Session ID, user code, verification URL, and polling interval |
| `auth.device.poll` | `{"provider":"openai-codex","session_id":"...","user_code":"...","name":"optional"}` | `pending`, or `complete` with the persisted public account |
| `usage.get` | `{"account":"optional","refresh":true}` | One result per selected account with typed provider metrics; omitting `account` selects all |
| `resets.get` | `{"account":"...","refresh":true,"include_unavailable":false}` | Reset-credit payload for one account |
| `settings.get` | `{}` | Current settings |
| `settings.set` | Complete settings object | Normalized, persisted settings |

`refresh` defaults to `true`. With `false`, usage and reset methods perform no network access and return the cache used by the CLI dashboard. When refreshing every account, a provider failure is returned in that account's `error` field so successful accounts are not discarded.

Provider metrics are returned in `UsageResult.metrics`. Percentage metrics include `used_percent` and optional `reset_at`. API keys are accepted as input only and are never returned.

### Device authentication

1. Call `auth.device.begin` with `provider: "openai-codex"`.
2. Show or open `verification_url` and display `user_code`.
3. Poll `auth.device.poll` with the same provider no faster than `poll_interval_seconds`.
4. Stop when the returned status is `complete`. The service exchanges the authorization code and saves the credentials itself.

## Go package

Go callers can skip JSON entirely:

```go
package main

import (
    "context"
    "log"

    "github.com/McMelonTV/codex-usage/usageapi"
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

The main entry points are `Service.ListAccounts`, `RenameAccount`, `RemoveAccount`, `SaveAPIKeyAccount`, `BeginDeviceAuth`, `PollDeviceAuth`, `Usage`, `ResetCredits`, `Settings`, and `UpdateSettings`. A custom `http.Client`, clock, accounts path, cache directory, and user agent can be supplied through `usageapi.Config` for embedding and testing.
