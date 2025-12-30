package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
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

// LoadSecrets reads secrets from disk. If the file doesn't exist or is corrupt,
// it returns DefaultSecrets with a warning logged (non-fatal).
func LoadSecrets() (Secrets, error) {
	path, err := SecretsPath()
	if err != nil {
		return DefaultSecrets(), err
	}

	return LoadSecretsFrom(path)
}

// LoadSecretsFrom reads secrets from the specified path.
func LoadSecretsFrom(path string) (Secrets, error) {
	sec := DefaultSecrets()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File doesn't exist, use defaults (not an error)
			return sec, nil
		}
		log.Printf("Warning: failed to read secrets file: %v, using defaults", err)
		return sec, nil
	}

	// Try to parse JSON
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&sec); err != nil {
		log.Printf("Warning: secrets file is corrupt: %v, using defaults", err)
		return DefaultSecrets(), nil
	}

	// Check schema version
	if sec.SchemaVersion != CurrentSchemaVersion {
		log.Printf("Warning: secrets schema version mismatch (got %d, expected %d), using defaults",
			sec.SchemaVersion, CurrentSchemaVersion)
		return DefaultSecrets(), nil
	}

	return sec, nil
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
