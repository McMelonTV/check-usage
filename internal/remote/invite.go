package remote

import (
	"errors"
	"net/url"
	"strings"
)

const InviteScheme = "check-usage"

// FormatInvite builds a check-usage://pair?... deep link URI containing the
// validated server origin and one-time pairing code.
func FormatInvite(serverURL, code string) (string, error) {
	origin, err := ValidateURL(strings.TrimSpace(serverURL))
	if err != nil {
		return "", err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("pairing code cannot be empty")
	}
	query := url.Values{}
	query.Set("url", origin)
	query.Set("code", code)
	return (&url.URL{
		Scheme:   InviteScheme,
		Host:     "pair",
		RawQuery: query.Encode(),
	}).String(), nil
}

// FormatWebInvite builds a camera-scannable https://<server>/pair#code=...
// invite link. Keeping the code in the URL fragment prevents browsers and
// proxies from sending it in the GET /pair request or server logs.
func FormatWebInvite(serverURL, code string) (string, error) {
	origin, err := ValidateURL(strings.TrimSpace(serverURL))
	if err != nil {
		return "", err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("pairing code cannot be empty")
	}
	fragment := url.Values{}
	fragment.Set("code", code)
	return origin + "/pair#" + fragment.Encode(), nil
}

// ParseInvite extracts the server origin and one-time pairing code from either
// a check-usage://pair?... URI or a https://<server>/pair#code=... web invite.
// When input is not a pairing link (e.g. a plain server origin), ok is false.
func ParseInvite(input string) (serverURL, code string, ok bool, err error) {
	trimmed := strings.TrimSpace(input)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(lower, InviteScheme+"://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", "", true, errors.New("invalid pairing link")
		}
		if !strings.EqualFold(parsed.Scheme, InviteScheme) || !strings.EqualFold(parsed.Host, "pair") {
			return "", "", true, errors.New("invalid pairing link")
		}
		rawURL := strings.TrimSpace(parsed.Query().Get("url"))
		code = strings.TrimSpace(parsed.Query().Get("code"))
		if rawURL == "" || code == "" {
			return "", "", true, errors.New("pairing link is missing server URL or code")
		}
		origin, err := ValidateURL(rawURL)
		if err != nil {
			return "", "", true, err
		}
		return origin, code, true, nil
	}

	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", "", false, nil
		}
		if parsed.Path != "/pair" && parsed.Path != "/pair/" {
			return "", "", false, nil
		}
		if parsed.User != nil {
			return "", "", true, errors.New("invalid pairing link")
		}
		code = extractCodeParam(parsed.Fragment)
		if code == "" {
			code = strings.TrimSpace(parsed.Query().Get("code"))
		}
		if code == "" {
			return "", "", true, errors.New("pairing link is missing pairing code")
		}
		rawOrigin := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
		origin, err := ValidateURL(rawOrigin)
		if err != nil {
			return "", "", true, err
		}
		return origin, code, true, nil
	}

	return "", "", false, nil
}

func extractCodeParam(fragment string) string {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return ""
	}
	if values, err := url.ParseQuery(fragment); err == nil {
		if code := strings.TrimSpace(values.Get("code")); code != "" {
			return code
		}
	}
	return ""
}
