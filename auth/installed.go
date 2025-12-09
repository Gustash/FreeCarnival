package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// InstallInfo contains information about an installed game
type InstallInfo struct {
	// Directory where the game is installed
	InstallPath string `json:"install_path"`
	// Version of the installed game
	Version string `json:"version"`
	// OS the build is for
	OS BuildOS `json:"os"`
}

// InstalledConfig is a map of slug -> InstallInfo
type InstalledConfig map[string]*InstallInfo

func installedFilePath() (string, error) {
	return configFile("installed.json")
}

// SaveInstalled saves the installed games config to disk
func SaveInstalled(installed InstalledConfig) error {
	path, err := installedFilePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(installed, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// LoadInstalled loads the installed games config from disk
func LoadInstalled() (InstalledConfig, error) {
	path, err := installedFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config if file doesn't exist
			return make(InstalledConfig), nil
		}
		return nil, err
	}

	var installed InstalledConfig
	if err := json.Unmarshal(data, &installed); err != nil {
		return nil, err
	}

	return installed, nil
}

// AddInstalled adds or updates an installed game in the config
func AddInstalled(slug string, info *InstallInfo) error {
	installed, err := LoadInstalled()
	if err != nil {
		return fmt.Errorf("failed to load installed config: %w", err)
	}

	installed[slug] = info
	return SaveInstalled(installed)
}

// RemoveInstalled removes a game from the installed config
func RemoveInstalled(slug string) error {
	installed, err := LoadInstalled()
	if err != nil {
		return fmt.Errorf("failed to load installed config: %w", err)
	}

	delete(installed, slug)
	return SaveInstalled(installed)
}

// GetInstalled returns the install info for a game, or nil if not installed
func GetInstalled(slug string) (*InstallInfo, error) {
	installed, err := LoadInstalled()
	if err != nil {
		return nil, fmt.Errorf("failed to load installed config: %w", err)
	}

	return installed[slug], nil
}

// manifestDir returns the directory for storing manifests
func manifestDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "manifests"), nil
}

// SaveManifest saves a manifest file for later verification
func SaveManifest(slug, version, manifestType string, data []byte) error {
	dir, err := manifestDir()
	if err != nil {
		return err
	}

	// Create directory structure: manifests/<slug>/<version>/
	manifestPath := filepath.Join(dir, slug, version)
	if err := os.MkdirAll(manifestPath, 0o700); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.csv", manifestType)
	return os.WriteFile(filepath.Join(manifestPath, filename), data, 0o600)
}

// LoadManifest loads a saved manifest file
func LoadManifest(slug, version, manifestType string) ([]byte, error) {
	dir, err := manifestDir()
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("%s.csv", manifestType)
	path := filepath.Join(dir, slug, version, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest not found: %s", path)
		}
		return nil, err
	}

	return data, nil
}

// RemoveManifests removes all saved manifests for a game
func RemoveManifests(slug string) error {
	dir, err := manifestDir()
	if err != nil {
		return err
	}

	slugDir := filepath.Join(dir, slug)

	// Check if directory exists
	if _, err := os.Stat(slugDir); os.IsNotExist(err) {
		return nil // Nothing to remove
	}

	return os.RemoveAll(slugDir)
}
