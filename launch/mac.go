package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gustash/freecarnival/logger"
	"howett.net/plist"
)

// InfoPlist represents a minimal macOS Info.plist structure.
type InfoPlist struct {
	CFBundleExecutable string `plist:"CFBundleExecutable"`
}

// MacAppBundle represents a macOS application bundle.
type MacAppBundle struct {
	AppPath        string
	InfoPlistPath  string
	ExecutablePath string
}

// FindMacAppBundles finds all .app bundles in the given directory.
func FindMacAppBundles(installPath string) ([]*MacAppBundle, error) {
	var bundles []*MacAppBundle

	err := filepath.Walk(installPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && strings.HasSuffix(info.Name(), ".app") {
			bundle := &MacAppBundle{
				AppPath:       path,
				InfoPlistPath: filepath.Join(path, "Contents", "Info.plist"),
			}

			if _, err := os.Stat(bundle.InfoPlistPath); err == nil {
				executableName, err := parseInfoPlist(bundle.InfoPlistPath)
				if err == nil && executableName != "" {
					bundle.ExecutablePath = filepath.Join(path, "Contents", "MacOS", executableName)
					bundles = append(bundles, bundle)
				}
			}

			return filepath.SkipDir
		}

		return nil
	})

	return bundles, err
}

func parseInfoPlist(plistPath string) (string, error) {
	file, err := os.Open(plistPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var info InfoPlist
	decoder := plist.NewDecoder(file)
	if err := decoder.Decode(&info); err != nil {
		return "", err
	}

	return info.CFBundleExecutable, nil
}

// MarkAsExecutable sets the executable permission on the bundle's main executable.
func (b *MacAppBundle) MarkAsExecutable() error {
	if b.ExecutablePath == "" {
		return fmt.Errorf("no executable path set for bundle %s", b.AppPath)
	}

	if _, err := os.Stat(b.ExecutablePath); os.IsNotExist(err) {
		return fmt.Errorf("executable not found: %s", b.ExecutablePath)
	}

	if err := os.Chmod(b.ExecutablePath, 0o755); err != nil {
		return fmt.Errorf("failed to set executable permission on %s: %w", b.ExecutablePath, err)
	}

	return nil
}

// MarkMacExecutables finds and marks all Mac app executables in the install path.
func MarkMacExecutables(installPath string) error {
	bundles, err := FindMacAppBundles(installPath)
	if err != nil {
		return fmt.Errorf("failed to find Mac app bundles: %w", err)
	}

	if len(bundles) == 0 {
		return nil
	}

	for _, bundle := range bundles {
		logger.Debug("Marking executable", "path", bundle.ExecutablePath)
		if err := bundle.MarkAsExecutable(); err != nil {
			return err
		}
	}

	return nil
}

