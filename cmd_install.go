package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/download"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

// Re-export download constants for CLI flags
var (
	defaultMaxWorkers = download.DefaultMaxWorkers
	defaultMaxMemory  = download.DefaultMaxMemory
)

func newInstallCmd() *cobra.Command {
	var (
		version            string
		targetOS           string
		basePath           string
		path               string
		maxDownloadWorkers int
		maxMemoryUsage     int
		infoOnly           bool
		skipVerify         bool
		verbose            bool
	)

	cmd := &cobra.Command{
		Use:   "install <slug>",
		Short: "Install a game from your library",
		Long: `Install a game from your library by its slug (e.g., syberia-ii).

You can specify which version and OS to download. If not specified, 
the latest version for the current OS will be used.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			// Load library
			library, err := auth.LoadLibrary()
			if err != nil {
				return fmt.Errorf("could not load library: %w (try running 'sync' first)", err)
			}

			// Find product
			product := auth.FindProductBySlug(library, slug)
			if product == nil {
				return fmt.Errorf("game '%s' not found in your library", slug)
			}

			// Parse target OS
			var buildOS auth.BuildOS
			switch targetOS {
			case "windows", "win":
				buildOS = auth.BuildOSWindows
			case "linux", "lin":
				buildOS = auth.BuildOSLinux
			case "mac", "macos":
				buildOS = auth.BuildOSMac
			case "":
				// Default based on current OS
				// macOS: prefer native builds
				// Linux: prefer Windows builds (for Wine compatibility)
				if runtime.GOOS == "darwin" {
					buildOS = auth.BuildOSMac
				} else {
					buildOS = auth.BuildOSWindows
				}
			default:
				return fmt.Errorf("invalid OS '%s': must be windows, linux, or mac", targetOS)
			}

			// Find version
			var productVersion *auth.ProductVersion
			if version != "" {
				productVersion = product.FindVersion(version, buildOS)
				if productVersion == nil {
					return fmt.Errorf("version '%s' not found for %s (OS: %s)", version, slug, buildOS)
				}
			} else {
				productVersion = product.GetLatestVersion(buildOS)
				if productVersion == nil {
					return fmt.Errorf("no available version found for %s (OS: %s)", slug, buildOS)
				}
			}

			// Determine install path
			var installPath string
			if path != "" {
				installPath = path
			} else if basePath != "" {
				installPath = filepath.Join(basePath, slug)
			} else {
				installPath = filepath.Join(defaultInstallBasePath(), slug)
			}

			// Check if already installed (skip for --info)
			if !infoOnly {
				existingInstall, err := auth.GetInstalled(slug)
				if err != nil {
					return fmt.Errorf("failed to check installed games: %w", err)
				}
				if existingInstall != nil {
					if existingInstall.Version == productVersion.Version && existingInstall.OS == productVersion.OS {
						logger.Info("Game already installed",
							"name", product.Name,
							"version", existingInstall.Version,
							"os", existingInstall.OS,
							"path", existingInstall.InstallPath)
						logger.Info("Run 'verify' to check file integrity, or 'uninstall' first to reinstall")
						return nil
					}
					// Different version/OS - suggest user downgrades/updates game instead
					return fmt.Errorf("%s v%s (%s) is already installed at %s\nRun 'update %s -v %s' to install a different version",
						product.Name, existingInstall.Version, existingInstall.OS, existingInstall.InstallPath, slug, productVersion.Version)
				}
			}

			logger.Info("Installing game",
				"name", product.Name,
				"version", productVersion.Version,
				"os", productVersion.OS,
				"path", installPath)

			// Load session for authenticated downloads
			client, _, err := auth.LoadSessionClient()
			if err != nil {
				return fmt.Errorf("could not load session: %w (try running 'login' first)", err)
			}

			// Create download options
			opts := download.Options{
				MaxDownloadWorkers: maxDownloadWorkers,
				MaxMemoryUsage:     maxMemoryUsage,
				SkipVerify:         skipVerify,
				InfoOnly:           infoOnly,
				Verbose:            verbose,
			}

			// Create downloader and start download
			downloader := download.New(client, product, productVersion, opts)
			err = downloader.Download(cmd.Context(), installPath, nil, nil)

			// Check if download was cancelled (Ctrl+C)
			if errors.Is(err, context.Canceled) {
				return nil // Exit cleanly without error
			}

			if err != nil {
				return fmt.Errorf("download failed: %w", err)
			}

			if !infoOnly {
				// Save install info for later verification/updates
				installInfo := &auth.InstallInfo{
					InstallPath: installPath,
					Version:     productVersion.Version,
					OS:          productVersion.OS,
				}
				if err := auth.AddInstalled(slug, installInfo); err != nil {
					logger.Warn("Failed to save install info", "error", err)
				}

				logger.Info("Installation complete", "path", installPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&version, "version", "v", "", "Specific version to install (default: latest)")
	cmd.Flags().StringVar(&targetOS, "os", "", "Target OS: windows, linux, or mac (default: windows)")
	cmd.Flags().StringVar(&basePath, "base-path", "", "Base install path (game installed in subdirectory)")
	cmd.Flags().StringVar(&path, "path", "", "Exact install path (no subdirectory created)")
	cmd.Flags().IntVar(&maxDownloadWorkers, "workers", defaultMaxWorkers, "Number of parallel download workers")
	cmd.Flags().IntVar(&maxMemoryUsage, "max-memory", defaultMaxMemory, "Maximum memory usage for buffering chunks (bytes)")
	cmd.Flags().BoolVarP(&infoOnly, "info", "i", false, "Show download info without downloading")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip SHA verification of downloaded chunks")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show per-file progress")

	return cmd
}

func newUninstallCmd() *cobra.Command {
	var keepFiles bool

	cmd := &cobra.Command{
		Use:   "uninstall <slug>",
		Short: "Uninstall a game",
		Long: `Remove an installed game from your system.

By default, this removes both the game files and the configuration entry.
Use --keep-files to only remove the configuration entry without deleting files.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			// Check if game is installed
			installInfo, err := auth.GetInstalled(slug)
			if err != nil {
				return fmt.Errorf("failed to check installed games: %w", err)
			}
			if installInfo == nil {
				return fmt.Errorf("%s is not installed", slug)
			}

			logger.Info("Uninstalling game",
				"slug", slug,
				"version", installInfo.Version,
				"os", installInfo.OS)

			// Remove game files unless --keep-files is set
			if !keepFiles {
				logger.Info("Removing game files", "path", installInfo.InstallPath)
				if err := os.RemoveAll(installInfo.InstallPath); err != nil {
					return fmt.Errorf("failed to remove game files: %w", err)
				}
			}

			// Remove from installed config
			if err := auth.RemoveInstalled(slug); err != nil {
				return fmt.Errorf("failed to update installed config: %w", err)
			}

			logger.Info("Uninstall complete", "slug", slug)
			return nil
		},
	}

	cmd.Flags().BoolVar(&keepFiles, "keep-files", false, "Keep game files, only remove from config")

	return cmd
}
