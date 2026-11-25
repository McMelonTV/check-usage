package remoteserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/McMelonTV/check-usage/internal/storage"
)

type Config struct {
	ServerID     string `json:"server_id"`
	Listen       string `json:"listen"`
	PublicURL    string `json:"public_url,omitempty"`
	AccountsFile string `json:"accounts_file"`
	CacheDir     string `json:"cache_dir"`
	StateDir     string `json:"state_dir"`
}

func DefaultConfigPath() string {
	return filepath.Join(filepath.Dir(storage.DefaultAccountsPath()), "server.json")
}
func Initialize(path string) (Config, error) {
	if _, err := os.Stat(path); err == nil {
		return Config{}, errors.New("server config already exists")
	} else if !os.IsNotExist(err) {
		return Config{}, err
	}
	id, err := randomToken(24)
	if err != nil {
		return Config{}, err
	}
	config := Config{ServerID: id, Listen: "127.0.0.1:8787", AccountsFile: storage.DefaultAccountsPath(), CacheDir: storage.CacheDir(), StateDir: filepath.Join(filepath.Dir(path), "server-state")}
	return config, writeJSON(path, config)
}
func EnsureInitialized(path string) (Config, bool, error) {
	if _, err := os.Stat(path); err == nil {
		cfg, loadErr := LoadConfig(path)
		return cfg, false, loadErr
	} else if !os.IsNotExist(err) {
		return Config{}, false, err
	}
	cfg, err := Initialize(path)
	return cfg, err == nil, err
}
func SaveConfig(path string, config Config) error {
	return writeJSON(path, config)
}
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if json.Unmarshal(data, &config) != nil {
		return Config{}, errors.New("invalid server configuration")
	}
	if config.ServerID == "" || config.AccountsFile == "" || config.CacheDir == "" || config.StateDir == "" {
		return Config{}, errors.New("incomplete server configuration")
	}
	host, _, err := net.SplitHostPort(config.Listen)
	if err != nil {
		return Config{}, errors.New("server listen address must include host and port")
	}
	ip := net.ParseIP(host)
	// Bind only to loopback or an explicit Tailscale address. Tailscale Serve
	// can forward tailnet HTTPS to the default loopback listener.
	if ip == nil || (!ip.IsLoopback() && !tailnetIP(ip)) {
		return Config{}, errors.New("bind to loopback or an explicit Tailscale IP, not a public/wildcard interface")
	}
	return config, nil
}
func tailnetIP(ip net.IP) bool {
	_, v4, _ := net.ParseCIDR("100.64.0.0/10")
	_, v6, _ := net.ParseCIDR("fd7a:115c:a1e0::/48")
	return v4.Contains(ip) || v6.Contains(ip)
}
func randomToken(bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, data)
}
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".check-usage-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
