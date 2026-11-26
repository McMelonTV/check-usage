// Package remote implements the authenticated device client. Provider credentials
// and native process configuration never cross this transport.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/usageapi"
)

type Config struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	ServerID string `json:"server_id"`
}
type ServerInfo struct {
	ServerID        string `json:"server_id"`
	ProtocolVersion string `json:"protocol_version"`
	Device          struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Access string `json:"access"`
	} `json:"device"`
}
type Client struct {
	Config Config
	HTTP   *http.Client
}

func DefaultConfigPath() string {
	return filepath.Join(filepath.Dir(storage.DefaultAccountsPath()), "client.json")
}
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, errors.New("server connection is not configured; run check-usage connect")
	}
	var config Config
	if json.Unmarshal(data, &config) != nil {
		return Config{}, errors.New("invalid server connection config")
	}
	if _, err := New(config, nil); err != nil {
		return Config{}, err
	}
	return config, nil
}
func SaveConfig(path string, config Config) error {
	if _, err := New(config, nil); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".client-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func ValidateURL(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("server URL must be an origin without credentials, path, query, or fragment")
	}
	if parsed.Scheme != "https" {
		ip := net.ParseIP(parsed.Hostname())
		_, v4, _ := net.ParseCIDR("100.64.0.0/10")
		_, v6, _ := net.ParseCIDR("fd7a:115c:a1e0::/48")
		if parsed.Scheme != "http" || (parsed.Hostname() != "localhost" && (ip == nil || (!ip.IsLoopback() && !v4.Contains(ip) && !v6.Contains(ip)))) {
			return "", errors.New("use HTTPS, or HTTP on an explicit loopback/Tailscale IP")
		}
	}
	return strings.TrimRight(address, "/"), nil
}
func New(config Config, httpClient *http.Client) (*Client, error) {
	address, err := ValidateURL(config.URL)
	if err != nil {
		return nil, err
	}
	config.URL = address
	if strings.TrimSpace(config.Token) == "" {
		return nil, errors.New("device token missing; pair this device with the server")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	copy := *httpClient
	if copy.Transport == nil {
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			tr := base.Clone()
			if tr.TLSHandshakeTimeout < 30*time.Second {
				tr.TLSHandshakeTimeout = 30 * time.Second
			}
			copy.Transport = tr
		}
	}
	copy.Jar = nil
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{Config: config, HTTP: &copy}, nil
}
func (client *Client) request(ctx context.Context, method, path string, input, output any) error {
	var data []byte
	var err error
	if input != nil {
		data, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, client.Config.URL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if path != "/v1/pair" {
		req.Header.Set("Authorization", "Bearer "+client.Config.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := client.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach usage server: %w", err)
	}
	defer response.Body.Close()
	if path != "/v1/pair" {
		if err := checkServerIdentity(client.Config.ServerID, response); err != nil {
			return err
		}
	}
	if response.StatusCode == 401 {
		return errors.New("device authorization rejected; pair again or check whether this device was revoked")
	}
	if response.StatusCode == 403 {
		return errors.New("this device does not have permission for that server operation")
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("usage server returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return errors.New("cannot read bounded server response")
	}
	if json.Unmarshal(body, output) != nil {
		return errors.New("usage server returned invalid JSON")
	}
	return nil
}

// checkServerIdentity verifies the response came from the paired server. A
// response without the server-ID header did not come from check-usage at all
// (typically a Tailscale Serve or reverse-proxy error page while the server is
// restarting or unreachable), so it is reported as unavailable rather than as
// an identity change. Successful responses must always carry the header.
func checkServerIdentity(serverID string, response *http.Response) error {
	if serverID == "" {
		return nil
	}
	got := response.Header.Get("X-Check-Usage-Server-ID")
	if got == "" {
		if response.StatusCode == http.StatusOK {
			return errors.New("response did not come from a check-usage server; check the server URL")
		}
		return fmt.Errorf("usage server is unavailable (HTTP %d from proxy); is `check-usage server serve` running?", response.StatusCode)
	}
	if got != serverID {
		return errors.New("server identity changed; reconnect explicitly")
	}
	return nil
}

func Pair(ctx context.Context, address, code string, httpClient *http.Client) (Config, error) {
	client, err := New(Config{URL: address, Token: "pairing"}, httpClient)
	if err != nil {
		return Config{}, err
	}
	var result struct {
		ServerID string `json:"server_id"`
		Token    string `json:"token"`
	}
	if err := client.request(ctx, http.MethodPost, "/v1/pair", map[string]string{"code": strings.TrimSpace(code)}, &result); err != nil {
		return Config{}, err
	}
	if result.ServerID == "" || result.Token == "" {
		return Config{}, errors.New("server returned an incomplete pairing response")
	}
	return Config{URL: client.Config.URL, Token: result.Token, ServerID: result.ServerID}, nil
}
func (client *Client) Info(ctx context.Context) (ServerInfo, error) {
	var info ServerInfo
	if err := client.request(ctx, http.MethodGet, "/v1/info", nil, &info); err != nil {
		return info, err
	}
	if info.ServerID == "" || !strings.HasPrefix(info.ProtocolVersion, "2.") {
		return info, errors.New("usage server protocol is incompatible")
	}
	if client.Config.ServerID != "" && client.Config.ServerID != info.ServerID {
		return info, errors.New("server identity changed; reconnect explicitly")
	}
	return info, nil
}
func (client *Client) Call(ctx context.Context, method string, params any, target any) error {
	input, err := json.Marshal(params)
	if err != nil {
		return err
	}
	request := usageapi.RPCRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: method, Params: input}
	var response struct {
		JSONRPC string             `json:"jsonrpc"`
		ID      json.RawMessage    `json:"id"`
		Result  json.RawMessage    `json:"result"`
		Error   *usageapi.RPCError `json:"error"`
	}
	if err := client.request(ctx, http.MethodPost, "/v1/rpc", request, &response); err != nil {
		return err
	}
	if response.JSONRPC != "2.0" || string(response.ID) != "1" {
		return errors.New("invalid usage server RPC envelope")
	}
	if response.Error != nil {
		if message, ok := response.Error.Data.(string); ok {
			return errors.New(message)
		}
		return errors.New(response.Error.Message)
	}
	if target != nil && json.Unmarshal(response.Result, target) != nil {
		return errors.New("invalid usage server result")
	}
	return nil
}
func (client *Client) Accounts(ctx context.Context) ([]usageapi.Account, error) {
	var accounts []usageapi.Account
	err := client.Call(ctx, "accounts.list", struct{}{}, &accounts)
	return accounts, err
}
func (client *Client) Usage(ctx context.Context, account string, refresh bool) ([]usageapi.UsageResult, error) {
	var results []usageapi.UsageResult
	err := client.Call(ctx, "usage.get", map[string]any{"account": account, "refresh": refresh}, &results)
	return results, err
}
func (client *Client) Resets(ctx context.Context, account string) (usageapi.ResetCreditsResult, error) {
	var result usageapi.ResetCreditsResult
	err := client.Call(ctx, "resets.get", map[string]any{"account": account, "refresh": true}, &result)
	return result, err
}
