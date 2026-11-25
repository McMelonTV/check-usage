package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
)

func tailscaleAvailable() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

func runTailscale(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, "tailscale", args...).Output()
}

// detectTailscaleServeURL checks `tailscale serve status --json` for an HTTPS
// reverse proxy pointing at listenAddr and returns its origin (e.g. https://host.ts.net).
func detectTailscaleServeURL(listenAddr string) string {
	if !tailscaleAvailable() {
		return ""
	}
	out, err := runTailscale(2*time.Second, "serve", "status", "--json")
	if err != nil || len(out) == 0 {
		return ""
	}
	return parseTailscaleServeJSON(out, listenAddr)
}

func parseTailscaleServeJSON(data []byte, listenAddr string) string {
	var status struct {
		Web map[string]struct {
			Handlers map[string]struct {
				Proxy string `json:"Proxy"`
			} `json:"Handlers"`
		} `json:"Web"`
	}
	if json.Unmarshal(data, &status) != nil {
		return ""
	}
	_, listenPort, _ := net.SplitHostPort(listenAddr)
	for hostPort, web := range status.Web {
		for _, handler := range web.Handlers {
			if proxyMatchesListen(handler.Proxy, listenAddr, listenPort) {
				if origin := httpsOriginFromHostPort(hostPort); origin != "" {
					return origin
				}
			}
		}
	}
	return ""
}

func proxyMatchesListen(proxy, listenAddr, listenPort string) bool {
	proxy = strings.TrimRight(strings.TrimSpace(proxy), "/")
	if proxy == "" {
		return false
	}
	if strings.EqualFold(proxy, "http://"+listenAddr) {
		return true
	}
	if listenPort != "" {
		for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
			if strings.EqualFold(proxy, "http://"+host+":"+listenPort) {
				return true
			}
		}
	}
	return false
}

func httpsOriginFromHostPort(hostPort string) string {
	hostPort = strings.TrimSpace(hostPort)
	if hostPort == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = strings.TrimSuffix(hostPort, ".")
		port = "443"
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return ""
	}
	candidate := "https://" + host
	if port != "" && port != "443" {
		candidate = "https://" + net.JoinHostPort(host, port)
	}
	origin, err := remote.ValidateURL(candidate)
	if err != nil {
		return ""
	}
	return origin
}

// detectTailscaleDNSURL reads `tailscale status --json` and returns https://<Self.DNSName>.
func detectTailscaleDNSURL() string {
	if !tailscaleAvailable() {
		return ""
	}
	out, err := runTailscale(2*time.Second, "status", "--json")
	if err != nil || len(out) == 0 {
		return ""
	}
	return parseTailscaleStatusDNS(out)
}

func parseTailscaleStatusDNS(data []byte) string {
	var status struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if json.Unmarshal(data, &status) != nil {
		return ""
	}
	name := strings.TrimSuffix(strings.TrimSpace(status.Self.DNSName), ".")
	if name == "" {
		return ""
	}
	origin, err := remote.ValidateURL("https://" + name)
	if err != nil {
		return ""
	}
	return origin
}

// detectServerPublicURL resolves the best known HTTPS origin for pairing links.
func detectServerPublicURL(config remoteserver.Config) string {
	if origin, err := remote.ValidateURL(strings.TrimSpace(config.PublicURL)); err == nil {
		return origin
	}
	if origin := detectTailscaleServeURL(config.Listen); origin != "" {
		return origin
	}
	return detectTailscaleDNSURL()
}

func enableTailscaleServe(listenAddr string) (string, error) {
	if !tailscaleAvailable() {
		return "", errors.New("tailscale CLI not found on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tailscale", "serve", "--bg", "http://"+listenAddr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return "", fmt.Errorf("tailscale serve failed: %s", msg)
		}
		return "", fmt.Errorf("tailscale serve failed: %w", err)
	}
	if origin := detectTailscaleServeURL(listenAddr); origin != "" {
		return origin, nil
	}
	if origin := detectTailscaleDNSURL(); origin != "" {
		return origin, nil
	}
	for _, field := range strings.Fields(string(output)) {
		trimmed := strings.TrimRight(field, ".,;)")
		if strings.HasPrefix(trimmed, "https://") {
			if origin, err := remote.ValidateURL(trimmed); err == nil {
				return origin, nil
			}
		}
	}
	return "", nil
}
