package download

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gustash/freecarnival/auth"
)

// Executable represents a launchable executable
type Executable struct {
	Path string
	Name string
}

// FindExecutables finds all launchable executables in the install path based on the build OS
func FindExecutables(installPath string, buildOS auth.BuildOS) ([]Executable, error) {
	switch buildOS {
	case auth.BuildOSMac:
		return findMacExecutables(installPath)
	case auth.BuildOSWindows:
		return findWindowsExecutables(installPath)
	case auth.BuildOSLinux:
		return findLinuxExecutables(installPath)
	default:
		return nil, fmt.Errorf("unsupported OS: %s", buildOS)
	}
}

// findMacExecutables finds all .app bundles and their executables
func findMacExecutables(installPath string) ([]Executable, error) {
	bundles, err := FindMacAppBundles(installPath)
	if err != nil {
		return nil, err
	}

	var executables []Executable
	for _, bundle := range bundles {
		if bundle.ExecutablePath != "" {
			// Verify the executable exists
			if _, err := os.Stat(bundle.ExecutablePath); err == nil {
				name := filepath.Base(bundle.AppPath)
				executables = append(executables, Executable{
					Path: bundle.ExecutablePath,
					Name: name,
				})
			}
		}
	}

	return executables, nil
}

// findWindowsExecutables finds all .exe files in the install path
func findWindowsExecutables(installPath string) ([]Executable, error) {
	var executables []Executable

	err := filepath.Walk(installPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Look for .exe files
		if strings.HasSuffix(strings.ToLower(info.Name()), ".exe") {
			// Skip common non-game executables
			lowerName := strings.ToLower(info.Name())
			if isIgnoredExecutable(lowerName) {
				return nil
			}

			relPath, _ := filepath.Rel(installPath, path)
			executables = append(executables, Executable{
				Path: path,
				Name: relPath,
			})
		}

		return nil
	})

	return executables, err
}

// findLinuxExecutables finds executable files in the install path
func findLinuxExecutables(installPath string) ([]Executable, error) {
	var executables []Executable

	err := filepath.Walk(installPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file is executable
		if info.Mode()&0o111 != 0 {
			// Skip common non-game files
			lowerName := strings.ToLower(info.Name())
			if isIgnoredExecutable(lowerName) {
				return nil
			}

			// Skip shell scripts and other common non-game executables
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if ext == ".sh" || ext == ".py" || ext == ".so" {
				return nil
			}

			relPath, _ := filepath.Rel(installPath, path)
			executables = append(executables, Executable{
				Path: path,
				Name: relPath,
			})
		}

		return nil
	})

	return executables, err
}

// isIgnoredExecutable returns true for common utility executables that aren't the main game
func isIgnoredExecutable(name string) bool {
	ignored := []string{
		"unins000.exe",
		"uninstall.exe",
		"uninst.exe",
		"crashhandler",
		"crashreporter",
		"crash_reporter",
		"ue4prereqsetup",
		"dxsetup",
		"vcredist",
		"dotnetfx",
		"directx",
		"physx",
		"redist",
		"setup",
		"installer",
	}

	lowerName := strings.ToLower(name)
	for _, ig := range ignored {
		if strings.Contains(lowerName, ig) {
			return true
		}
	}
	return false
}

// LaunchOptions configures how a game is launched
type LaunchOptions struct {
	// WinePath is the path to wine executable (default: "wine")
	WinePath string
	// WinePrefix is the WINEPREFIX to use (optional)
	WinePrefix string
	// NoWine disables Wine even for Windows executables
	NoWine bool
}

// LaunchGame launches the specified executable with optional arguments
func LaunchGame(executablePath string, buildOS auth.BuildOS, args []string, opts *LaunchOptions) error {
	// Verify executable exists
	if _, err := os.Stat(executablePath); os.IsNotExist(err) {
		return fmt.Errorf("executable not found: %s", executablePath)
	}

	if opts == nil {
		opts = &LaunchOptions{}
	}

	// On macOS, use 'open' command for .app bundles
	if runtime.GOOS == "darwin" && strings.Contains(executablePath, ".app/Contents/MacOS/") {
		// Extract the .app path
		appPath := executablePath
		if idx := strings.Index(executablePath, ".app/"); idx != -1 {
			appPath = executablePath[:idx+4]
		}

		cmdArgs := []string{"-a", appPath}
		if len(args) > 0 {
			cmdArgs = append(cmdArgs, "--args")
			cmdArgs = append(cmdArgs, args...)
		}

		cmd := exec.Command("open", cmdArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Check if we need Wine for Windows executables on non-Windows hosts
	needsWine := buildOS == auth.BuildOSWindows && runtime.GOOS != "windows" && !opts.NoWine

	if needsWine {
		return launchWithWine(executablePath, args, opts)
	}

	// For other platforms, run the executable directly
	cmd := exec.Command(executablePath, args...)
	cmd.Dir = filepath.Dir(executablePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Start()
}

// launchWithWine launches a Windows executable using Wine
func launchWithWine(executablePath string, args []string, opts *LaunchOptions) error {
	winePath := opts.WinePath
	if winePath == "" {
		winePath = findWine()
	}

	if winePath == "" {
		return fmt.Errorf("wine not found; install Wine or specify path with --wine")
	}

	// Create WINEPREFIX directory if it doesn't exist
	if opts.WinePrefix != "" {
		if err := os.MkdirAll(opts.WinePrefix, 0o755); err != nil {
			return fmt.Errorf("failed to create wine prefix directory: %w", err)
		}
	}

	// Build wine command: wine <exe> [args...]
	cmdArgs := append([]string{executablePath}, args...)
	cmd := exec.Command(winePath, cmdArgs...)
	cmd.Dir = filepath.Dir(executablePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set WINEPREFIX if specified
	if opts.WinePrefix != "" {
		cmd.Env = append(os.Environ(), "WINEPREFIX="+opts.WinePrefix)
	}

	return cmd.Start()
}

// findWine searches for Wine in common locations
func findWine() string {
	// Check PATH first
	if path, err := exec.LookPath("wine"); err == nil {
		return path
	}

	// Common Wine locations
	candidates := []string{
		"/usr/local/bin/wine",
		"/usr/bin/wine",
		"/opt/wine-stable/bin/wine",
		"/opt/wine-staging/bin/wine",
	}

	// macOS-specific locations
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Wine Stable.app/Contents/Resources/wine/bin/wine",
			"/Applications/Wine Staging.app/Contents/Resources/wine/bin/wine",
			"/opt/homebrew/bin/wine",
			"/usr/local/opt/wine/bin/wine",
		)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// SelectExecutable helps select an executable when multiple are available
// If exeName is provided, it tries to match it; otherwise returns the first one
func SelectExecutable(executables []Executable, exeName string) (*Executable, error) {
	if len(executables) == 0 {
		return nil, fmt.Errorf("no executables found")
	}

	// If a specific executable name was provided, try to find it
	if exeName != "" {
		lowerExeName := strings.ToLower(exeName)
		for i := range executables {
			lowerPath := strings.ToLower(executables[i].Path)
			lowerName := strings.ToLower(executables[i].Name)
			if strings.Contains(lowerPath, lowerExeName) || strings.Contains(lowerName, lowerExeName) {
				return &executables[i], nil
			}
		}
		return nil, fmt.Errorf("executable '%s' not found", exeName)
	}

	// Return the first executable if only one found
	if len(executables) == 1 {
		return &executables[0], nil
	}

	// Multiple executables found, return error with list
	return nil, fmt.Errorf("multiple executables found, please specify one with --exe")
}
