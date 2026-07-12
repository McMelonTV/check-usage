# codex-usage

Small Go CLI for checking ChatGPT usage/rate-limit info across saved accounts.

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
