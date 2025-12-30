package config

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
)

const (
	passwordLength  = 24
	passwordCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	defaultUsername = "admin"
)

// SecretsLoadStatus indicates how secrets were loaded.
type SecretsLoadStatus int

const (
	// SecretsLoaded means secrets were successfully loaded from file.
	SecretsLoaded SecretsLoadStatus = iota
	// SecretsMissing means the secrets file doesn't exist (safe to create).
	SecretsMissing
	// SecretsFallback means there was an error reading/parsing (unsafe to overwrite).
	SecretsFallback
)

// Secret is a string type that masks its value when printed or logged.
// Use Value() to get the actual string value.
type Secret string

// String returns a masked value for logging safety.
func (s Secret) String() string {
	return "[REDACTED]"
}

// GoString returns a masked value for %#v formatting.
func (s Secret) GoString() string {
	return "[REDACTED]"
}

// Value returns the actual secret value.
// Use this only when the actual value is needed (e.g., HTTP headers, API calls).
func (s Secret) Value() string {
	return string(s)
}

// IsEmpty returns true if the secret is empty.
func (s Secret) IsEmpty() bool {
	return s == ""
}

// Secrets holds sensitive application configuration.
// WARNING: Do not log this struct directly as json.Marshal will expose values.
type Secrets struct {
	SchemaVersion     int    `json:"schema_version"`
	DiscordWebhookURL Secret `json:"discord_webhook_url"`
	BasicAuthUsername string `json:"basic_auth_username"`
	BasicAuthPassword Secret `json:"basic_auth_password"`
}

// DefaultSecrets returns a Secrets with empty values.
func DefaultSecrets() Secrets {
	return Secrets{
		SchemaVersion:     CurrentSchemaVersion,
		DiscordWebhookURL: "",
		BasicAuthUsername: "",
		BasicAuthPassword: "",
	}
}

// LoadSecrets reads secrets from disk. Returns the secrets, load status, and any error.
// Status indicates whether it's safe to overwrite the secrets file.
func LoadSecrets() (Secrets, SecretsLoadStatus, error) {
	path, err := SecretsPath()
	if err != nil {
		return DefaultSecrets(), SecretsFallback, err
	}

	return LoadSecretsFrom(path)
}

// LoadSecretsFrom reads secrets from the specified path.
// Returns status to indicate whether it's safe to overwrite the file.
func LoadSecretsFrom(path string) (Secrets, SecretsLoadStatus, error) {
	sec := DefaultSecrets()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File doesn't exist, safe to create
			return sec, SecretsMissing, nil
		}
		log.Printf("Warning: failed to read secrets file: %v, using defaults", err)
		return sec, SecretsFallback, fmt.Errorf("read secrets: %w", err)
	}

	// Try to parse JSON
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&sec); err != nil {
		log.Printf("Warning: secrets file is corrupt: %v, using defaults", err)
		return DefaultSecrets(), SecretsFallback, fmt.Errorf("decode secrets: %w", err)
	}

	// Check schema version
	if sec.SchemaVersion != CurrentSchemaVersion {
		log.Printf("Warning: secrets schema version mismatch (got %d, expected %d), using defaults",
			sec.SchemaVersion, CurrentSchemaVersion)
		return DefaultSecrets(), SecretsFallback, fmt.Errorf("schema mismatch: got %d", sec.SchemaVersion)
	}

	return sec, SecretsLoaded, nil
}

// SaveSecrets writes secrets to disk atomically.
func SaveSecrets(sec Secrets) error {
	path, err := SecretsPath()
	if err != nil {
		return err
	}

	return SaveSecretsTo(sec, path)
}

// SaveSecretsTo writes secrets to the specified path atomically.
func SaveSecretsTo(sec Secrets, path string) error {
	// Ensure schema version is set
	sec.SchemaVersion = CurrentSchemaVersion

	return writeJSONAtomic(path, sec)
}

// GeneratePassword generates a cryptographically secure random password.
func GeneratePassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("generate password: length must be positive")
	}
	b := make([]byte, length)
	charsetLen := big.NewInt(int64(len(passwordCharset)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		b[i] = passwordCharset[idx.Int64()]
	}
	return string(b), nil
}

// EnsureLanAuth ensures Basic Auth credentials exist when LAN mode is enabled.
// Returns (updated bool, generatedPassword string, error).
// If credentials were generated, generatedPassword contains the plaintext for one-time display.
func EnsureLanAuth(s *Secrets, lanEnabled bool) (updated bool, generatedPassword string, err error) {
	if !lanEnabled {
		return false, "", nil
	}

	if s.BasicAuthUsername == "" {
		s.BasicAuthUsername = defaultUsername
		updated = true
	}

	if s.BasicAuthPassword.IsEmpty() {
		pw, err := GeneratePassword(passwordLength)
		if err != nil {
			return false, "", err
		}
		s.BasicAuthPassword = Secret(pw)
		generatedPassword = pw
		updated = true
	}

	return updated, generatedPassword, nil
}

// WritePasswordFile writes the generated password to a file in the data directory.
// Returns the file path. File is created with 0600 permissions.
func WritePasswordFile(username, password string) (string, error) {
	dataDir, err := EnsureDataDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, "generated_password.txt")
	content := fmt.Sprintf("Username: %s\nPassword: %s\n\nDelete this file after saving the credentials.\n", username, password)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("write password file: %w", err)
	}
	return path, nil
}
