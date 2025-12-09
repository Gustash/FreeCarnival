// Package launch handles game executable discovery and launching.
package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/logger"
)

// Executable represents a launchable executable.
type Executable struct {
	Path string
	Name string
}

// Options configures how a game is launched.
type Options struct {
	WinePath   string // Path to wine executable (default: "wine")
	WinePrefix string // WINEPREFIX to use (optional)
	NoWine     bool   // Disable Wine even for Windows executables
	Wrapper    string // Custom wrapper command to use (replaces Wine if set)
}

// FindExecutables finds all launchable executables in the install path based on the build OS.
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

func findMacExecutables(installPath string) ([]Executable, error) {
	bundles, err := FindMacAppBundles(installPath)
	if err != nil {
		return nil, err
	}

	var executables []Executable
	for _, bundle := range bundles {
		if bundle.ExecutablePath != "" {
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

func findWindowsExecutables(installPath string) ([]Executable, error) {
	var executables []Executable

	err := filepath.Walk(installPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(info.Name()), ".exe") {
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

func findLinuxExecutables(installPath string) ([]Executable, error) {
	var executables []Executable

	err := filepath.Walk(installPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if info.Mode()&0o111 != 0 {
			lowerName := strings.ToLower(info.Name())
			if isIgnoredExecutable(lowerName) {
				return nil
			}

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

// Game launches the specified executable with optional arguments.
// It waits for the process to complete and kills it if the context is cancelled.
func Game(ctx context.Context, executablePath string, buildOS auth.BuildOS, args []string, opts *Options) error {
	if _, err := os.Stat(executablePath); os.IsNotExist(err) {
		return fmt.Errorf("executable not found: %s", executablePath)
	}

	if opts == nil {
		opts = &Options{}
	}

	// If a custom wrapper is specified, use it
	if opts.Wrapper != "" {
		return launchWithWrapper(ctx, executablePath, args, opts)
	}

	needsWine := buildOS == auth.BuildOSWindows && runtime.GOOS != "windows" && !opts.NoWine

	if needsWine {
		return launchWithWine(ctx, executablePath, args, opts)
	}

	return launchNative(ctx, executablePath, args)
}

func launchWithWrapper(ctx context.Context, executablePath string, args []string, opts *Options) error {
	// Split wrapper to support commands with arguments (e.g., "proton run")
	wrapperParts := strings.Fields(opts.Wrapper)
	if len(wrapperParts) == 0 {
		return fmt.Errorf("wrapper command is empty")
	}

	// Build command: wrapper [wrapper args...] executable [game args...]
	cmdArgs := append(wrapperParts[1:], executablePath)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(wrapperParts[0], cmdArgs...)
	cmd.Dir = filepath.Dir(executablePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Pass through Wine-related environment variables if set
	if opts.WinePrefix != "" {
		cmd.Env = append(os.Environ(), "WINEPREFIX="+opts.WinePrefix)
	}

	return launchProcess(ctx, cmd)
}

func launchNative(ctx context.Context, executablePath string, args []string) error {
	cmd := exec.Command(executablePath, args...)
	cmd.Dir = filepath.Dir(executablePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return launchProcess(ctx, cmd)
}

func launchWithWine(ctx context.Context, executablePath string, args []string, opts *Options) error {
	winePath := opts.WinePath
	if winePath == "" {
		winePath = findWine()
	}

	if winePath == "" {
		return fmt.Errorf("wine not found; install Wine or specify path with --wine")
	}

	if opts.WinePrefix != "" {
		if err := os.MkdirAll(opts.WinePrefix, 0o755); err != nil {
			return fmt.Errorf("failed to create wine prefix directory: %w", err)
		}
	}

	cmdArgs := append([]string{executablePath}, args...)
	cmd := exec.Command(winePath, cmdArgs...)
	cmd.Dir = filepath.Dir(executablePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if opts.WinePrefix != "" {
		cmd.Env = append(os.Environ(), "WINEPREFIX="+opts.WinePrefix)
	}

	return launchProcess(ctx, cmd)
}

func launchProcess(ctx context.Context, cmd *exec.Cmd) error {
	// Set up process group (platform-specific)
	setupProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait for process or context cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		logger.Info("Terminating game process...")
		if err := killProcessGroup(cmd); err != nil {
			logger.Warn("Failed to kill process group", "error", err)
		}
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// killProcessGroup kills the process and its entire process group
var defaultWineCandidates = []string{
	"/usr/local/bin/wine",
	"/usr/bin/wine",
	"/opt/wine-stable/bin/wine",
	"/opt/wine-staging/bin/wine",
}

var macWineCandidates = []string{
	"/Applications/Wine Stable.app/Contents/Resources/wine/bin/wine",
	"/Applications/Wine Staging.app/Contents/Resources/wine/bin/wine",
	"/opt/homebrew/bin/wine",
	"/usr/local/opt/wine/bin/wine",
}

func findWine() string {
	// First check if wine is in PATH
	if path, err := exec.LookPath("wine"); err == nil {
		return path
	}

	// Otherwise check candidate paths
	candidates := defaultWineCandidates
	if runtime.GOOS == "darwin" {
		candidates = append(append([]string{}, defaultWineCandidates...), macWineCandidates...)
	}
	return findWineInCandidates(candidates)
}

func findWineInCandidates(candidates []string) string {
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// SelectExecutable helps select an executable when multiple are available.
func SelectExecutable(executables []Executable, exeName string) (*Executable, error) {
	if len(executables) == 0 {
		return nil, fmt.Errorf("no executables found")
	}

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

	if len(executables) == 1 {
		return &executables[0], nil
	}

	return nil, fmt.Errorf("multiple executables found, please specify one with --exe")
}
