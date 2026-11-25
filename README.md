# check-usage

A self-hosted AI usage server with a terminal dashboard (`check-usage`) and an Android companion app (**AI Usage Widgets**).

Run the server on your homelab alongside your signed-in AI CLIs and provider credentials. Every paired laptop or phone reads from the same account catalog and shared usage cache over your Tailscale tailnet—without storing provider credentials on client devices. Device display settings and cached snapshots stay local.

## Supported providers

| Provider | Collection method | Server registration |
| --- | --- | --- |
| **Antigravity** (Gemini & Claude/GPT) | Native CLI only (`agy`) | `server accounts native --driver agy` |
| **Codex** (OpenAI) | Native CLI (`codex`), opt-in API fallback, or direct login | `server accounts native --driver codex` or `server accounts login --provider codex` |
| **Claude** | OAuth browser code | `server accounts login --provider claude` |
| **Cursor** | Browser login or API key | `server accounts login --provider cursor` or `server accounts add --provider cursor` |
| **OpenCode Go** | API key | `server accounts add --provider opencode-go` |
| **DeepSeek** | API key | `server accounts add --provider deepseek` |
| **Custom adapter** | Trusted admin JSON executable | `server accounts native --provider ID --source-file ...` |

## Install

The same CLI binary runs both the homelab server and the paired terminal client, and ships with a short `cu` alias (usable anywhere `check-usage` is shown below). For Android, see [Android app](#android-app).

### With `mise`

Install the latest prebuilt release (puts both `check-usage` and `cu` on your `$PATH`):

```bash
mise use -g github:McMelonTV/check-usage
```

Upgrade later with `mise upgrade github:McMelonTV/check-usage`.

### With `go install`

Requires **Go 1.26 or newer** (ensure `$(go env GOPATH)/bin` is on your `$PATH`):

```bash
# Install check-usage and/or the short cu alias:
go install github.com/McMelonTV/check-usage@latest
go install github.com/McMelonTV/check-usage/cmd/cu@latest
```

### Prebuilt archives or source build

Download a platform archive from [GitHub Releases](https://github.com/McMelonTV/check-usage/releases) (each `.tar.gz` / `.zip` includes `check-usage` and the `cu` symlink/alias alongside `check-usage-SHA256SUMS.txt`), or build from a local checkout:

```bash
go build -o check-usage .
```

### Self-updates

Standalone installs (from GitHub Releases or `go install`) can update themselves directly from GitHub Releases with SHA-256 checksum verification:

```bash
cu update            # Download, verify, and replace the binary in place
cu update --check    # Check whether a newer release is available
cu version           # Show the installed version
```

On Unix, `update` also creates a `cu -> check-usage` symlink next to `check-usage` if one does not already exist (without overwriting an unrelated `cu` binary). On Linux, if `check-usage.service` is running, it is restarted automatically (pass `--no-restart` to skip). If the binary is managed by `mise`, `update` directs you to `mise upgrade` unless `--force` is passed.

## Set up the homelab server

Run these commands as the **same OS user** who owns your signed-in AI CLI profiles. Make sure you have already signed in interactively through `agy` (using a release that supports `agy -p /usage --output-format json`) and `codex` on the host.

### Quick setup (recommended)

```bash
./check-usage server setup
```

The interactive wizard walks through everything in one pass and can be re-run any time to add more accounts:

1. Initializes `~/.config/check-usage/server.json` if needed (all `server` commands auto-initialize on first run).
2. Auto-detects installed `agy` and `codex` CLIs on `$PATH` and offers one-key registration.
3. Lets you add **Claude**, **Cursor**, **OpenCode Go**, or **DeepSeek** from one menu (skips launching a local browser automatically over SSH/headless Linux, and prompts securely for API keys or reuses existing env vars).
4. Optionally runs `tailscale serve --bg http://127.0.0.1:8787` and saves the resulting `https://<host>.<tailnet>.ts.net` URL in `server.json`.
5. On Linux, optionally installs and starts the `systemd --user` service with your current `$PATH` so `agy`, `codex`, and shims (such as mise) work out of the box.
6. Generates a QR code and one-paste pairing link for your first device.

### Manual setup & background service

If you prefer explicit commands or scripted provisioning:

```bash
./check-usage server accounts native --driver agy --name "Google"
./check-usage server accounts native --driver codex --name "OpenAI"
./check-usage server accounts add                  # Interactive provider picker (or pass flags)
./check-usage server serve                         # Run in the foreground on 127.0.0.1:8787
```

Override the config path on any server command with `check-usage server --config /path/server.json <command>`. Keep `server-state` persistent across restarts so paired devices stay connected.

**Tailscale Serve:** By default, the server listens on `127.0.0.1:8787` (bind addresses are restricted to loopback or an explicit Tailscale IP—no public port is needed). Expose it over your tailnet with HTTPS:

```bash
tailscale serve --bg http://127.0.0.1:8787
```

**Systemd user service (Linux):** Install, inspect, or remove the background unit with one command (captures your current binary path and `$PATH`):

```bash
./check-usage server service install
./check-usage server service status
./check-usage server service uninstall
```

> **Note:** To start the user service at boot and keep it running after logout, enable user lingering for this account through your homelab's normal host administration.

**Live updates:** Server account files use cross-process locking and atomic writes, so you can run `server accounts ...`, `server pair`, and `server devices ...` while the server or systemd service is running.

### Provider logins, API keys, and native fallbacks

All provider credentials and source configurations are managed locally on the server. Over SSH or headless Linux, browser launches are skipped automatically (no need to pass `--no-browser`):

```bash
./check-usage server accounts login --provider claude --name "Claude"
./check-usage server accounts login --provider cursor --name "Cursor"
./check-usage server accounts login --provider codex --name "OpenAI API"
./check-usage server accounts add --provider opencode-go   # Prompts securely or reads OPENCODE_API_KEY
./check-usage server accounts add --provider deepseek --api-key-env DEEPSEEK_API_KEY
./check-usage server accounts add --provider cursor --api-key-env CURSOR_API_KEY
./check-usage server accounts reauth <id-or-name>
```

**Opt-in API fallback for Codex:** You can attach a native Codex profile to an existing OAuth-backed server account (such as "OpenAI API" above) and explicitly enable its saved API integration as fallback:

```bash
./check-usage server accounts native --driver codex --account "OpenAI API" --fallback
```

- Fallback is **off by default** and must be explicitly enabled for an existing account with saved API credentials (`--fallback` or the source file’s `fallback` setting).
- **Antigravity is native-only.** Direct Google quota/OAuth emulation is not used; the locally installed `agy` CLI owns authentication and upstream requests.
- **Usage-only native probes:** Built-in native drivers run fixed usage-only commands (`agy -p /usage --output-format json` and Codex `app-server` read RPCs) and never send inference prompts. Probes run in private temporary working directories with timeouts, bounded output, and process cleanup; auto-updates and browser launches are disabled for `agy`. Using a native CLI does not guarantee any provider's enforcement policy, and this project makes no promises regarding account standing.
- Removing a native account unregisters it from `check-usage` without signing out the underlying CLI.

### Multiple profiles and other native adapters

To register multiple Codex profiles, point each one at its own configuration directory (passed to the CLI via `CODEX_HOME`):

```bash
./check-usage server accounts native --driver codex \
  --config-dir /srv/ai-profiles/work-codex \
  --name "Work OpenAI"
```

For administrator-supplied adapters for supported providers, use the `json` driver with `--source-file` pointing to a private JSON definition:

```json
{
  "driver": "json",
  "executable": "/srv/check-usage/adapters/provider-usage",
  "args": ["--json"],
  "env": {"PROVIDER_CONFIG_DIR": "/srv/ai-profiles/work"},
  "timeout_seconds": 30,
  "refresh_seconds": 300,
  "fallback": false
}
```

Register the adapter locally on the server:

```bash
./check-usage server accounts native \
  --provider claude \
  --source-file /private/claude-source.json \
  --name "Work Claude"
```

The executable must print a normalized JSON usage object to stdout:

```json
{
  "plan": "Pro",
  "metrics": [
    {"kind": "percentage", "slot": "session", "label": "SESSION", "used_percent": 25, "reset_at": 1795568400},
    {"kind": "percentage", "slot": "weekly", "label": "WEEKLY", "used_percent": 40, "reset_at": 1796083200}
  ]
}
```

- Valid slots are `session`, `weekly`, and `monthly`; optional `scope` identifies a model family or pool.
- Omitting `used_percent` reports the metric as unknown (`0` means measured zero usage).
- The server never accepts executable paths, arguments, environment overrides, or shell commands from remote clients.

## Pair and manage devices

Each device must be paired with its own fresh, one-use code (valid for 10 minutes) and receives an independent random bearer token. The server stores only token hashes.

Generate a pairing QR code and one-paste `check-usage://pair?...` link on the homelab (automatically detects your Tailscale Serve HTTPS URL, or pass `--url https://...`):

```bash
./check-usage server pair laptop
./check-usage server pair phone
```

### Pair a terminal client

On your laptop (with Tailscale connected), run `./check-usage` (or `./check-usage connect`) and paste the `check-usage://pair?...` link when prompted:

```bash
./check-usage
```

For scripted setups, `check-usage connect --url https://homelab.your-tailnet.ts.net --pair-code-file path` (`-` reads stdin) and `check-usage server pair --code-only` / `--out path` remain supported. The CLI saves its token to owner-only `~/.config/check-usage/client.json` (override with `--connection-file path`).

### Pair the Android app

1. Generate a pairing code on the homelab (`./check-usage server pair phone`).
2. **Scan the terminal QR code** with your phone's camera—it opens **AI Usage Widgets** with the server URL and code pre-filled so you just confirm and tap **Pair device**.
   - Alternatively, open **Server** in the app and paste the `check-usage://pair?...` link into either field to auto-fill both, or enter the URL and code manually.

The token is encrypted with Android Keystore. Accounts sync from the server automatically—pull to refresh or tap **Sync accounts** at any time. Foreground sync runs every minute, while background and widget sync follow Android's ~15-minute schedule.

### Permissions and revocation

Paired devices are **read-only by default**. To allow a device to rename or remove shared accounts on the server, issue its code with `--allow-account-changes`:

```bash
./check-usage server pair phone --allow-account-changes
```

Shared renames and removals propagate to other devices on their next account refresh. Credential changes and provider sign-ins always stay server-local.

List or revoke devices independently on the homelab (by name or ID; pass `--json` for machine-readable output):

```bash
./check-usage server devices list
./check-usage server devices revoke phone
```

- Revocation takes effect on the next request; a revoked device may still show previously cached display data locally, and reconnecting requires a new code.
- Pairing Android to a different server replaces its local account cache and widget assignments.
- **Upgrading from an older client-only version:** Previous local provider sign-ins on CLI or Android devices are not uploaded automatically—configure your accounts on the homelab server, then pair each device.

## Terminal client

```bash
cu                                                 # Interactive dashboard (TUI)
cu --plain                                         # Non-interactive usage table
cu accounts list                                   # List shared server accounts
cu accounts rename ACCOUNT "New name"              # Rename shared account (requires write access)
cu accounts remove ACCOUNT                         # Remove shared account (requires write access)
cu resets ACCOUNT                                  # Show reset-credit details
cu api usage.get '{"refresh":true}'                # Call the authenticated HTTP JSON-RPC API
cu update                                          # Self-update from GitHub Releases
cu version                                         # Show installed version
```

- **Dashboard controls:** Switch between **Usage**, **Resets**, and **Settings** tabs. Press `r` to refresh, `e` to rename a shared account, `d` twice to remove it (when permitted), and `q` to quit. Display preferences stay local to each terminal client.
- **Caching & rate limits:** Clients display cached usage immediately and mark it stale when offline. Manual refreshes share the server cache and coalesce overlapping requests, enforcing minimum refresh intervals (**≥ 5 minutes** for native Antigravity and Claude; **≥ 1 minute** for other standard providers). Failed probes back off while retaining the last measured values, and slow providers do not block account discovery or other accounts. Source metadata distinguishes native reads from API fallback.
- Reset credit claiming is not implemented.

## Grouped model limits

Some subscriptions expose multiple model pools under a single account. The TUI, Android cards, and widgets render these in a shared grouped layout with clean column headers:

- **Antigravity:** Preserves all four scoped limits reported by `agy` across **Gemini** and **Claude/GPT** rows with **5-hour** and **Weekly** columns under one selectable account. Weekly-only plans do not receive invented 5-hour allowances, and missing measurements remain unknown. If a family's weekly allowance is exhausted, only that family's 5-hour allowance is struck through. In the TUI, scoped rows use a purple star indicator, muted tree connectors, and normal-text family names.
- **Antigravity plan:** `agy` print mode does not report the plan, so on Linux and macOS the server briefly starts interactive `agy` in a pseudo-terminal, reads `GetUserStatus` from its loopback language server using a one-time CSRF token, and stops it. This runs at most daily once the plan is known (hourly while unknown), uses no model quota, and never touches the Google login; `agy` makes the upstream request itself. On Windows the plan stays unknown.
- **Cursor:** Renders **Cursor models** and **Other models** as separate independent rows, both under **Monthly** (older cached slots are normalized automatically).
- **Compact widgets:** Small square Android widgets retain all four Antigravity values; very short widgets summarize the tightest reported limit per family.

## Android app

- **Offline resilience:** Cached usage stays visible when the homelab or Tailscale is unreachable. Network errors never wipe the local account catalog.
- **Account sync:** Successful sync reconciles additions, renames, and removals while preserving widget assignments for unchanged account IDs.
- **Widget styles:** Tonal, Frosted, Midnight, Sunset, and Dot Matrix on supported Nothing devices.
- **Privacy:** No analytics or hosted backend.

### Screenshots

<p align="center">
  <img src=".github/assets/mobile-nothing.png" alt="AI Usage Widgets on a Nothing phone" height="480">
  &nbsp;
  <img src=".github/assets/tablet-portrait.png" alt="AI Usage Widgets on a tablet in portrait orientation" height="480">
</p>

<p align="center">
  <img src=".github/assets/tablet-landscape.png" alt="AI Usage Widgets on a tablet in landscape orientation" width="800">
</p>

### Build from source

Requires **Go 1.26+**, **JDK 25**, **Android SDK 36**, and an **Android NDK** supported by Go Mobile. Minimum supported OS is **Android 8.0 (API 26)**.

```bash
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug
```

Debug APKs (one universal APK plus four per-architecture variants) are written to `android/app/build/outputs/apk/debug/`.

## Application API

Protocol **2.0** exposes versioned HTTP endpoints for paired devices (`/v1/pair`, authenticated `/v1/info`, and `/v1/rpc`) while keeping provider authentication, provider credentials, and native source configuration local to the server.

For local server administration and embedding, the importable `usageapi` Go package and local NDJSON interface (`check-usage server api serve`) remain available.

See [docs/application-api.md](docs/application-api.md) for the transport specification, pairing flow, metric schema, and local administration API.
