package connectorconfig

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	APIURL              string `json:"api_url"`
	WSURL               string `json:"ws_url"`
	DeviceID            string `json:"device_id"`
	CredentialID        string `json:"credential_id"`
	PrivateKey          string `json:"private_key,omitempty"`
	ProtectedPrivateKey string `json:"protected_private_key,omitempty"`
}

const (
	DefaultAPIURL = "https://aiboard.agentcaseshare.cn"
	DefaultWSURL  = "wss://aiboard.agentcaseshare.cn/v1/connectors/ws"
)

func DefaultPath() string {
	if value := os.Getenv("AGENTBOARD_CONFIG_PATH"); value != "" {
		return value
	}
	return filepath.Join(DefaultDirectory(), "connector.json")
}

// DefaultDirectory is independent of the process working directory so a
// scheduled task and an interactive terminal share the same connector state.
func DefaultDirectory() string {
	directory, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(directory) == "" {
		return ".agentboard"
	}
	return filepath.Join(directory, "AgentBoard")
}

// LoadDefault migrates the working-directory-based location used by early
// Connector builds. Migration keeps existing pairings usable after upgrading.
func LoadDefault() (Config, error) {
	path := DefaultPath()
	config, found, err := loadIfExists(path)
	if err != nil || found {
		return config, err
	}
	for _, legacyPath := range legacyPaths() {
		if legacyPath == path {
			continue
		}
		config, found, err = loadIfExists(legacyPath)
		if err != nil {
			return Config{}, err
		}
		if !found {
			continue
		}
		if err := Save(path, config); err != nil {
			return Config{}, fmt.Errorf("migrate connector config: %w", err)
		}
		return config, nil
	}
	return Config{}, nil
}

func loadIfExists(path string) (Config, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("stat connector config: %w", err)
	}
	config, err := Load(path)
	return config, true, err
}

func legacyPaths() []string {
	paths := make([]string, 0, 2)
	if workingDirectory, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(workingDirectory, ".agentboard", "connector.json"))
	}
	if executable, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(executable), ".agentboard", "connector.json"))
	}
	return paths
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read connector config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, fmt.Errorf("decode connector config: %w", err)
	}
	return config, nil
}

func (c *Config) FillDefaults() {
	if c.APIURL == "" {
		c.APIURL = getenv("AGENTBOARD_API_URL", DefaultAPIURL)
	}
	if c.WSURL == "" {
		c.WSURL = getenv("AGENTBOARD_WS_URL", DefaultWSURL)
	}
	if c.DeviceID == "" {
		c.DeviceID = getenv("AGENTBOARD_DEVICE_ID", "")
	}
	if c.CredentialID == "" {
		c.CredentialID = getenv("AGENTBOARD_CREDENTIAL_ID", "")
	}
}

func (c *Config) EnsureIdentity() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	if c.DeviceID == "" {
		id, err := randomID("device")
		if err != nil {
			return nil, nil, err
		}
		c.DeviceID = id
	}
	if c.CredentialID == "" {
		id, err := randomID("cred")
		if err != nil {
			return nil, nil, err
		}
		c.CredentialID = id
	}
	if c.PrivateKey == "" && c.ProtectedPrivateKey != "" {
		privateKey, err := unprotectPrivateKey(c.ProtectedPrivateKey)
		if err != nil {
			return nil, nil, fmt.Errorf("unprotect stored device key: %w", err)
		}
		c.PrivateKey = privateKey
	}
	if c.PrivateKey != "" {
		raw, err := base64.StdEncoding.DecodeString(c.PrivateKey)
		if err != nil || len(raw) != ed25519.PrivateKeySize {
			return nil, nil, errors.New("stored connector private key is invalid")
		}
		privateKey := ed25519.PrivateKey(raw)
		return privateKey.Public().(ed25519.PublicKey), privateKey, nil
	}
	publicKey, privateKey, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate device key: %w", err)
	}
	c.PrivateKey = base64.StdEncoding.EncodeToString(privateKey)
	return publicKey, privateKey, nil
}

func Save(path string, config Config) error {
	if strings.TrimSpace(config.PrivateKey) == "" || strings.TrimSpace(config.DeviceID) == "" || strings.TrimSpace(config.CredentialID) == "" {
		return errors.New("connector identity is incomplete")
	}
	directory := filepath.Dir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create connector config directory: %w", err)
		}
	}
	persisted, err := configForStorage(config)
	if err != nil {
		return fmt.Errorf("protect device key: %w", err)
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("encode connector config: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write connector config: %w", err)
	}
	return nil
}

func configForStorage(config Config) (Config, error) {
	if !usePlatformSecretProtection() {
		return config, nil
	}
	protected, err := protectPrivateKey(config.PrivateKey)
	if err != nil {
		return Config{}, err
	}
	config.PrivateKey = ""
	config.ProtectedPrivateKey = protected
	return config, nil
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := cryptorand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate identity: %w", err)
	}
	return fmt.Sprintf("%s_%x", prefix, bytes), nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
