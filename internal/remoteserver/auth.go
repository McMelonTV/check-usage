package remoteserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	device := Device{ID: id, Name: record.Name, Access: record.Access, CreatedAt: now.Unix(), TokenHash: hash(token)}
	if err := writeJSON(filepath.Join(config.StateDir, "devices", id+".json"), device); err != nil {
		return PairedDevice{}, err
	}
	device.TokenHash = ""
	return PairedDevice{ServerID: config.ServerID, Device: device, Token: token}, nil
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
	return devices, nil
}
func RevokeDevice(config Config, id string) error {
	if !deviceIDPattern.MatchString(id) {
		return errors.New("invalid device ID")
	}
	return os.Remove(filepath.Join(config.StateDir, "devices", id+".json"))
}
