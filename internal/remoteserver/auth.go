package remoteserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Pairing struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
}
type pairingRecord struct {
	Name      string `json:"name"`
	Access    string `json:"access"`
	ExpiresAt int64  `json:"expires_at"`
}
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Access    string `json:"access"`
	CreatedAt int64  `json:"created_at"`
	TokenHash string `json:"token_hash,omitempty"`
}
type PairedDevice struct {
	ServerID string `json:"server_id"`
	Device   Device `json:"device"`
	Token    string `json:"token"`
}

var deviceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func CreatePairing(config Config, name string, allowChanges bool, now time.Time) (Pairing, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return Pairing{}, errors.New("device name must be between 1 and 100 characters")
	}
	code, err := randomToken(32)
	if err != nil {
		return Pairing{}, err
	}
	record := pairingRecord{Name: name, Access: "read", ExpiresAt: now.Add(10 * time.Minute).Unix()}
	if allowChanges {
		record.Access = "write"
	}
	if err := writeJSON(filepath.Join(config.StateDir, "pairings", hash(code)+".json"), record); err != nil {
		return Pairing{}, err
	}
	return Pairing{Code: code, ExpiresAt: record.ExpiresAt}, nil
}

// LocalLoopbackURL converts a server listen address (e.g. 127.0.0.1:8787 or :8787)
// into a local HTTP origin suitable for same-host CLI connections.
func LocalLoopbackURL(listen string) (string, error) {
	addr := strings.TrimSpace(listen)
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(port) == "" {
		return "", errors.New("invalid server listen address")
	}
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// ProvisionLocalDevice registers a paired device directly in local server state
// for same-host CLI usage without requiring an interactive pairing code.
func ProvisionLocalDevice(config Config, name string, allowChanges bool, now time.Time) (PairedDevice, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "local"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	access := "read"
	if allowChanges {
		access = "write"
	}
	return createDeviceRecord(config, name, access, now)
}

func createDeviceRecord(config Config, name, access string, now time.Time) (PairedDevice, error) {
	idSecret, err := randomToken(32)
	if err != nil {
		return PairedDevice{}, err
	}
	id := hash(idSecret)[:32]
	secret, err := randomToken(32)
	if err != nil {
		return PairedDevice{}, err
	}
	token := id + "." + secret
	device := Device{ID: id, Name: name, Access: access, CreatedAt: now.Unix(), TokenHash: hash(token)}
	if err := writeJSON(filepath.Join(config.StateDir, "devices", id+".json"), device); err != nil {
		return PairedDevice{}, err
	}
	device.TokenHash = ""
	return PairedDevice{ServerID: config.ServerID, Device: device, Token: token}, nil
}

func redeemPairing(config Config, code string, now time.Time) (PairedDevice, error) {
	if len(code) != 43 {
		return PairedDevice{}, errors.New("invalid or expired pairing code")
	}
	path := filepath.Join(config.StateDir, "pairings", hash(code)+".json")
	claimed := path + ".claimed"
	// Renaming claims a one-time code atomically, including concurrent clients.
	if err := os.Rename(path, claimed); err != nil {
		return PairedDevice{}, errors.New("invalid or expired pairing code")
	}
	defer os.Remove(claimed)
	data, err := os.ReadFile(claimed)
	if err != nil {
		return PairedDevice{}, errors.New("invalid or expired pairing code")
	}
	var record pairingRecord
	if json.Unmarshal(data, &record) != nil || now.Unix() >= record.ExpiresAt {
		return PairedDevice{}, errors.New("invalid or expired pairing code")
	}
	return createDeviceRecord(config, record.Name, record.Access, now)
}

func authenticate(config Config, token string) (Device, bool) {
	id, secret, ok := strings.Cut(token, ".")
	if !ok || !deviceIDPattern.MatchString(id) || len(secret) != 43 {
		return Device{}, false
	}
	data, err := os.ReadFile(filepath.Join(config.StateDir, "devices", id+".json"))
	if err != nil {
		return Device{}, false
	}
	var device Device
	if json.Unmarshal(data, &device) != nil || device.ID != id {
		return Device{}, false
	}
	if len(device.TokenHash) != 64 || subtle.ConstantTimeCompare([]byte(device.TokenHash), []byte(hash(token))) != 1 {
		return Device{}, false
	}
	device.TokenHash = ""
	return device, true
}
func ListDevices(config Config) ([]Device, error) {
	paths, err := filepath.Glob(filepath.Join(config.StateDir, "devices", "*.json"))
	if err != nil {
		return nil, err
	}
	devices := []Device{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var device Device
		if json.Unmarshal(data, &device) != nil {
			return nil, errors.New("invalid saved device record")
		}
		device.TokenHash = ""
		devices = append(devices, device)
	}
	for i := 0; i < len(devices); i++ {
		for j := i + 1; j < len(devices); j++ {
			if devices[j].CreatedAt > devices[i].CreatedAt || (devices[j].CreatedAt == devices[i].CreatedAt && strings.ToLower(devices[j].Name) < strings.ToLower(devices[i].Name)) {
				devices[i], devices[j] = devices[j], devices[i]
			}
		}
	}
	return devices, nil
}
func RevokeDevice(config Config, target string) error {
	target = strings.TrimSpace(target)
	if deviceIDPattern.MatchString(target) {
		return os.Remove(filepath.Join(config.StateDir, "devices", target+".json"))
	}
	devices, err := ListDevices(config)
	if err != nil {
		return err
	}
	var matches []Device
	for _, device := range devices {
		if strings.EqualFold(strings.TrimSpace(device.Name), target) {
			matches = append(matches, device)
		}
	}
	switch len(matches) {
	case 0:
		return errors.New("device not found (pass a device ID or name from server devices list)")
	case 1:
		return os.Remove(filepath.Join(config.StateDir, "devices", matches[0].ID+".json"))
	default:
		return errors.New("multiple devices share that name; revoke by device ID instead")
	}
}
