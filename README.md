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

2. Check current usage:

```bash
./codex-usage
```

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
```
