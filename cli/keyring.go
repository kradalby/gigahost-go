package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// keyringService identifies this application in the OS keyring.
const keyringService = "gigahost-go"

// storeToken persists a token for later use.
//
// It first tries the OS keyring. If the keyring is unavailable (most
// often on headless Linux systems without a running secret-service),
// it falls back to a JSON file with 0600 permissions in
// $XDG_CONFIG_HOME/gigahost/token.json, and reports the file path back
// to the caller for audit purposes.
func storeToken(username, token string) (string, error) {
	if username == "" {
		return "", errors.New("storeToken: username is empty")
	}

	if token == "" {
		return "", errors.New("storeToken: token is empty")
	}

	if err := keyring.Set(keyringService, username, token); err == nil {
		return "keyring", nil
	}

	// Fallback: write to XDG_CONFIG_HOME/gigahost/token.json.
	path, err := tokenFilePath()
	if err != nil {
		return "", err
	}

	if err := writeTokenFile(path, username, token); err != nil {
		return "", err
	}

	return path, nil
}

// loadToken retrieves a token from the keyring or the fallback file.
// It returns ("", nil) when no token is stored.
func loadToken(username string) (string, error) {
	if username == "" {
		return "", errors.New("loadToken: username is empty")
	}

	tok, err := keyring.Get(keyringService, username)
	if err == nil {
		return tok, nil
	}

	path, err := tokenFilePath()
	if err != nil {
		return "", nil //nolint:nilerr // absence is not a hard error
	}

	m, err := readTokenFile(path)
	if err != nil {
		return "", nil //nolint:nilerr // absence is not a hard error
	}

	return m[username], nil
}

// clearToken removes a stored token from both the keyring and the
// fallback file.
func clearToken(username string) error {
	if username == "" {
		return errors.New("clearToken: username is empty")
	}

	_ = keyring.Delete(keyringService, username)

	path, err := tokenFilePath()
	if err != nil {
		return nil //nolint:nilerr // not fatal
	}

	m, err := readTokenFile(path)
	if err != nil {
		return nil //nolint:nilerr // absence is fine
	}

	delete(m, username)

	if len(m) == 0 {
		_ = os.Remove(path)

		return nil
	}

	return writeAllTokens(path, m)
}

func tokenFilePath() (string, error) {
	base, err := xdgConfigHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(base, configDir, "token.json"), nil
}

func writeTokenFile(path, username, token string) error {
	m, _ := readTokenFile(path)
	if m == nil {
		m = map[string]string{}
	}

	m[username] = token

	return writeAllTokens(path, m)
}

func writeAllTokens(path string, m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// json/v2 does not sort map keys unless asked, so without this every
	// login rewrites token.json with the entries shuffled — pure diff noise
	// for anyone who versions or backs up their config directory.
	data, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}

	return nil
}

func readTokenFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	return m, nil
}
