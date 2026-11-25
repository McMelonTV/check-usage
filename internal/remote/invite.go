package remote

import (
	"errors"
	"net/url"
	"strings"
)

const InviteScheme = "check-usage"

// FormatInvite builds a single-paste / QR-scannable pairing URI containing the
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

// ParseInvite extracts the server origin and one-time pairing code from a
// check-usage://pair?... URI. When input is not a pairing URI, ok is false.
func ParseInvite(input string) (serverURL, code string, ok bool, err error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(strings.ToLower(trimmed), InviteScheme+"://") {
		return "", "", false, nil
	}
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
