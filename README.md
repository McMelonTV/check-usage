# codex-usage

A simple Go CLI app for easily checking OpenAI Codex/ChatGPT Work usage limits and available "reset credits" across multiple ChatGPT accounts.

This repository also includes an Android app with home-screen widgets for the same usage information.

## Install

Download a prebuilt binary for your platform from [GitHub Releases](https://github.com/McMelonTV/codex-usage/releases) and run `codex-usage`/`codex-usage.exe` in a terminal.

To build the CLI from source instead, install [Go](https://go.dev/doc/install) and run:

On Linux/macOS:
```bash
go build -o codex-usage .
```

On Windows:
```sh
go build -o codex-usage.exe .
```

## Get started

Sign in to your first account:

```bash
./codex-usage accounts login --name "My Account"
```

Then view usage for all saved accounts:

```bash
./codex-usage
```

The summary shows the 5-hour and weekly Codex limits, available reset credits, and the earliest credit expiry.

To see individual reset credits for an account:

```bash
./codex-usage resets "My Account"
```

An account can be identified by its name, email, or ID. Add `--show-used` to include redeemed and expired credits.

## Commands

```text
codex-usage accounts list
codex-usage accounts login [--name name] [--no-browser] [--auth-flow device|browser]
codex-usage accounts remove <id-or-name>
codex-usage accounts rename <id-or-name> <new-name>
codex-usage resets [--show-used] <account-name-email-or-id>
```

Use `--accounts-file path` to choose a different accounts file or `--timeout seconds` to change the request timeout. By default, accounts are stored in `~/.config/codex-usage/accounts.json`.

## Android app ("AI Usage Widgets")

The app signs in separately from the CLI and displays remaining usage windows and "reset credits". It offers Glass and Material You widgets, plus a Nothing-inspired style available on Nothing devices. Each widget can use a different account and style.

### Screenshots

<p align="center">
  <img src=".github/assets/mobile-nothing.png" alt="AI Usage Widgets on a Nothing phone" height="480">
  &nbsp;
  <img src=".github/assets/tablet-portrait.png" alt="AI Usage Widgets on a tablet in portrait orientation" height="480">
</p>

<p align="center">
  <img src=".github/assets/tablet-landscape.png" alt="AI Usage Widgets on a tablet in landscape orientation" width="800">
</p>

After installing the app, connect a Codex account and add **AI Usage Widgets** from your launcher's widget picker. Account credentials are encrypted using Android Keystore, and the app has no analytics or backend.

To build the Android app from source, you need JDK 25, Android SDK 36, and an Android 8.0 (API 26) or newer device or emulator:

```bash
cd android
./gradlew :app:assembleDebug
```

The APK is created at `android/app/build/outputs/apk/debug/app-debug.apk`.

The widgets should automatically refresh approximately every 15 minutes, although the exact refresh timing is controlled by Android and appears to be a bit inconsistent. You should always be able to trigger a refresh using the button in the widget.
