# codex-usage

Small Go CLI for checking ChatGPT usage/rate-limit info across saved accounts.

This repository also contains **Usage Widgets**, a provider-ready Android companion app. Codex is its first provider. The app signs in independently, shows both remaining usage windows and reset credits, and offers Nothing-inspired, Glass, and Pixel/Material You home-screen widget styles.

## Android app

Requirements:

- JDK 25
- Android SDK 36
- An API 26+ Android device or emulator

Build the debug APK:

```bash
cd android
./gradlew :app:assembleDebug
```

The APK is written to `android/app/build/outputs/apk/debug/app-debug.apk`.

The Android namespace and application ID are `ing.boykiss.usagewidgets`. Java and Kotlin compilation both target JVM 25. Provider credentials are kept in Android Keystore-backed encrypted storage; usage snapshots and widget configuration are stored locally in Room. The app has no analytics or backend.

After installing, connect Codex using device authorization, then add **Usage Widgets** from the launcher widget picker. Each widget can select an account and one of the three styles.

Periodic refresh uses WorkManager's 15-minute minimum interval, but Android may defer work. The current Codex provider mirrors compatibility-sensitive ChatGPT/Codex endpoints used by the CLI and may require updates if those interfaces change.

## Build

```bash
go build -o codex-usage .
```

## Quick Start

1. Log in an account:

```bash
./codex-usage accounts login --name "My Account"
```

2. Check current usage, including Codex rate limits and a compact reset-credit summary:

```bash
./codex-usage
```

The output includes 5-hour and weekly usage limits plus the total number of
available reset credits and the earliest available reset-credit expiry.

3. Show reset-credit details for one account:

```bash
./codex-usage resets "My Account"
```

The `resets` subcommand accepts an account name, email, or ID. It shows reset
credit totals plus individual available credits with title, status, gained time,
expiry time, and remaining time. Add `--show-used` to include redeemed, expired,
or otherwise unavailable credits.

The default accounts file is:

`~/.config/codex-usage/accounts.json`

## Commands

Main command:

```bash
./codex-usage [--accounts-file path] [--timeout seconds] [--show-color-config]
```

Account management:

```bash
./codex-usage accounts list [--accounts-file path]
./codex-usage accounts login [--accounts-file path] [--name name] [--timeout seconds] [--no-browser] [--auth-flow device|browser]
./codex-usage accounts remove [--accounts-file path] <id-or-name>
./codex-usage accounts rename [--accounts-file path] <id-or-name> <new-name>
./codex-usage resets [--accounts-file path] [--timeout seconds] [--show-used] <account name/email/id>
```
