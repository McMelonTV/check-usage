package remote

import "testing"

func TestInviteRoundTripAndValidation(t *testing.T) {
	link, err := FormatInvite("https://homelab.tailnet.ts.net/", "abc123XYZ")
	if err != nil {
		t.Fatalf("FormatInvite() error = %v", err)
	}
	serverURL, code, ok, err := ParseInvite(link)
	if err != nil || !ok {
		t.Fatalf("ParseInvite() = (%q, %q, %v, %v)", serverURL, code, ok, err)
	}
	if serverURL != "https://homelab.tailnet.ts.net" || code != "abc123XYZ" {
		t.Fatalf("unexpected parsed invite: url=%q code=%q", serverURL, code)
	}

	if _, _, ok, err := ParseInvite("https://homelab.tailnet.ts.net"); ok || err != nil {
		t.Fatalf("plain URL should not be treated as invite: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := ParseInvite("check-usage://pair?url=http://example.com&code=abc"); !ok || err == nil {
		t.Fatalf("expected invalid HTTP non-tailnet URL in invite to fail: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := ParseInvite("check-usage://pair?url=https://homelab.tailnet.ts.net"); !ok || err == nil {
		t.Fatalf("expected invite missing code to fail: ok=%v err=%v", ok, err)
	}
}
