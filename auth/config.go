package auth

import (
	"os"
	"path/filepath"
)

// testConfigDir can be set during tests to override the config directory
var testConfigDir string

// SetTestConfigDir sets the config directory for testing purposes.
// Pass empty string to reset to default behavior.
func SetTestConfigDir(dir string) {
	testConfigDir = dir
}

// configDir returns the configuration directory path (e.g., ~/.config/FreeCarnival)
func configDir() (string, error) {
	if testConfigDir != "" {
		return testConfigDir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FreeCarnival"), nil
}

// configFile returns the path to the config file
func configFile(filename string) (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}
