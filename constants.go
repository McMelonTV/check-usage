package main

import "github.com/McMelonTV/check-usage/codexapi"

const (
	oauthIssuerURL            = "https://auth.openai.com"
	oauthClientID             = codexapi.ClientID
	oauthOriginator           = "codex_cli_rs"
	oauthScope                = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	deviceAuthVerificationURL = codexapi.DeviceVerificationURL
	deviceAuthRedirectURI     = codexapi.DeviceRedirectURI
	deviceAuthMaxWaitSeconds  = 15 * 60
	oauthTimeout              = 5 * 60
	oauthDefaultPort          = 1455
	expirySkew                = 60
	userAgent                 = "codex-cli/1.0.0"
)

const (
	ansiReset      = "\x1b[0m"
	ansiHeader     = "\x1b[1;36m"
	ansiDarkRed    = "\x1b[1;38;2;200;20;20m"
	ansiRed        = "\x1b[38;2;239;68;68m"
	ansiAmber      = "\x1b[38;2;245;158;11m"
	ansiGreen      = "\x1b[38;2;34;197;94m"
	ansiLightGreen = "\x1b[38;2;134;239;172m"

	ansiStrike        = "\x1b[9m"
	ansiMutedDarkRed  = "\x1b[38;2;110;35;35m"
	ansiDim           = "\x1b[2m"
	ansiBlocked       = "\x1b[9;38;2;110;35;35m"
)

const (
	defaultAccountsRelPath = ".config/check-usage/accounts.json"
)
