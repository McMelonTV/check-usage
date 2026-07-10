package main

const (
	chatgptUsageURL           = "https://chatgpt.com/backend-api/wham/usage"
	chatgptResetCreditsURL    = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	oauthTokenURL             = "https://auth.openai.com/oauth/token"
	oauthIssuerURL            = "https://auth.openai.com"
	oauthClientID             = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthOriginator           = "codex_cli_rs"
	oauthScope                = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	deviceAuthBaseURL         = "https://auth.openai.com/api/accounts"
	deviceAuthUserCodePath    = "/deviceauth/usercode"
	deviceAuthTokenPath       = "/deviceauth/token"
	deviceAuthVerificationURL = "https://auth.openai.com/codex/device"
	deviceAuthRedirectURI     = "https://auth.openai.com/deviceauth/callback"
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
)

const (
	defaultAccountsRelPath = ".config/codex-usage/accounts.json"
)
