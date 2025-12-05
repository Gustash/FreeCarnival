// main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/download"
	"github.com/spf13/cobra"
)

// Default install path: ~/Games/freecarnival
func defaultInstallBasePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Games", "freecarnival")
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "freecarnival",
		Short: "CLI for IndieGala",
		Long:  "FreeCarnival is a native cross-platform CLI program to install and launch IndieGala games",
	}

	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newLogoutCmd())
	rootCmd.AddCommand(newSyncCmd())
	rootCmd.AddCommand(newInstallCmd())
	rootCmd.AddCommand(newUninstallCmd())
	rootCmd.AddCommand(newVerifyCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newLoginCmd() *cobra.Command {
	var email string
	var password string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to your IndieGala account",
		RunE: func(cmd *cobra.Command, args []string) error {

			if email == "" || password == "" {
				return fmt.Errorf("--email and --password are required")
			}

			client, res, err := auth.Login(cmd.Context(), email, password)
			_ = client // will be reused later

			if res != nil {
				fmt.Println("Received cookies:")
				for _, c := range res.Cookies {
					fmt.Printf(
						"  %s=%s; domain=%s; path=%s; secure=%v; httpOnly=%v\n",
						c.Name,
						c.Value,
						c.Domain,
						c.Path,
						c.Secure,
						c.HttpOnly,
					)
				}
				fmt.Println()
			}

			if err != nil {
				if res != nil {
					fmt.Fprintf(os.Stderr,
						"Login failed: message=%q (http=%d)\n",
						res.Message, res.StatusCode,
					)
				} else {
					fmt.Fprintf(os.Stderr, "Login request error: %v\n", err)
				}
				os.Exit(1)
			}

			fmt.Println("Login successful.")
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "IndieGala username/email (required)")
	cmd.Flags().StringVar(&password, "password", "", "IndieGala password (required)")
	cmd.MarkFlagRequired("email")
	cmd.MarkFlagRequired("password")

	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from your IndieGala account",
		Long:  "Removes the saved session, effectively logging you out. You will need to log in again to use authenticated commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.ClearSession(); err != nil {
				return fmt.Errorf("failed to clear session: %w", err)
			}
			fmt.Println("Logged out successfully.")
			return nil
		},
	}
}

func newSyncCmd() *cobra.Command {
	var installedOnly bool

	cmd := &cobra.Command{
		Use:     "library",
		Aliases: []string{"sync"},
		Short:   "Sync library with IndieGala",
		Long: `Sync your library with IndieGala and display your games.

Use --installed to show only installed games without syncing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load installed games
			installed, err := auth.LoadInstalled()
			if err != nil {
				return fmt.Errorf("failed to load installed games: %w", err)
			}

			// If --installed flag, just show installed games from local config
			if installedOnly {
				if len(installed) == 0 {
					fmt.Println("No games installed.")
					return nil
				}

				fmt.Printf("Installed games: %d\n\n", len(installed))
				i := 0
				for slug, info := range installed {
					i++
					fmt.Printf("%3d  %s  (v%s, %s)\n", i, slug, info.Version, info.OS)
					fmt.Printf("     %s\n", info.InstallPath)
				}
				return nil
			}

			// Sync with IndieGala
			client, _, err := auth.LoadSessionClient()
			if err != nil {
				return fmt.Errorf("could not load session: %w", err)
			}

			ui, products, err := auth.FetchUserInfo(cmd.Context(), client)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sync failed: %v\n", err)
				return err
			}

			// Display basic user info
			email := "<nil>"
			if ui.Email != nil {
				email = *ui.Email
			}
			username := "<nil>"
			if ui.Username != nil {
				username = *ui.Username
			}
			userID := "<nil>"
			if ui.UserID != nil {
				userID = *ui.UserID
			}

			fmt.Println("Sync successful.")
			fmt.Println("IndieGala Account:")
			fmt.Printf("  Email:    %s\n", email)
			fmt.Printf("  Username: %s\n", username)
			fmt.Printf("  User ID:  %s\n", userID)

			fmt.Printf("\nLibrary: %d products in user_collection\n\n", len(products))

			for i, p := range products {
				// Check if installed
				marker := "   "
				if _, ok := installed[p.SluggedName]; ok {
					marker = " ✓ "
				}
				fmt.Printf("%3d%s%s  (slug=%s)\n",
					i+1,
					marker,
					p.Name,
					p.SluggedName,
				)
			}

			if err := auth.SaveUserInfo(ui); err != nil {
				return fmt.Errorf("failed to save user info: %w", err)
			}

			if err := auth.SaveLibrary(products); err != nil {
				return fmt.Errorf("failed to save library: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&installedOnly, "installed", "i", false, "Show only installed games (no sync)")

	return cmd
}

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
				buildOS = auth.BuildOSWindows // Default to Windows if not specified
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

			fmt.Printf("Installing %s v%s (%s) to %s\n", product.Name, productVersion.Version, productVersion.OS, installPath)

			// Load session for authenticated downloads
			client, _, err := auth.LoadSessionClient()
			if err != nil {
				return fmt.Errorf("could not load session: %w (try running 'login' first)", err)
			}

			// Create download options
			opts := download.DownloadOptions{
				MaxDownloadWorkers: maxDownloadWorkers,
				MaxMemoryUsage:     maxMemoryUsage,
				SkipVerify:         skipVerify,
				InfoOnly:           infoOnly,
			}

			// Create downloader and start download
			downloader := download.NewDownloader(client, product, productVersion, opts)
			if err := downloader.Download(cmd.Context(), installPath); err != nil {
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
					fmt.Fprintf(os.Stderr, "Warning: failed to save install info: %v\n", err)
				}

				fmt.Printf("\nInstallation complete: %s\n", installPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&version, "version", "v", "", "Specific version to install (default: latest)")
	cmd.Flags().StringVar(&targetOS, "os", "", "Target OS: windows, linux, or mac (default: windows)")
	cmd.Flags().StringVar(&basePath, "base-path", "", "Base install path (game installed in subdirectory)")
	cmd.Flags().StringVar(&path, "path", "", "Exact install path (no subdirectory created)")
	cmd.Flags().IntVar(&maxDownloadWorkers, "workers", download.DefaultMaxDownloadWorkers, "Number of parallel download workers")
	cmd.Flags().IntVar(&maxMemoryUsage, "max-memory", download.DefaultMaxMemoryUsage, "Maximum memory usage for buffering chunks (bytes)")
	cmd.Flags().BoolVarP(&infoOnly, "info", "i", false, "Show download info without downloading")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip SHA verification of downloaded chunks")

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

			fmt.Printf("Uninstalling %s (v%s, %s)...\n",
				slug, installInfo.Version, installInfo.OS)

			// Remove game files unless --keep-files is set
			if !keepFiles {
				fmt.Printf("Removing files from %s...\n", installInfo.InstallPath)
				if err := os.RemoveAll(installInfo.InstallPath); err != nil {
					return fmt.Errorf("failed to remove game files: %w", err)
				}
			}

			// Remove from installed config
			if err := auth.RemoveInstalled(slug); err != nil {
				return fmt.Errorf("failed to update installed config: %w", err)
			}

			fmt.Printf("%s has been uninstalled.\n", slug)
			return nil
		},
	}

	cmd.Flags().BoolVar(&keepFiles, "keep-files", false, "Keep game files, only remove from config")

	return cmd
}

func newVerifyCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "verify <slug>",
		Short: "Verify file integrity for an installed game",
		Long: `Verify the integrity of an installed game by checking all file hashes
against the manifest. This ensures no files are corrupted or modified.`,
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

			fmt.Printf("Verifying %s (v%s, %s) at %s...\n",
				slug, installInfo.Version, installInfo.OS, installInfo.InstallPath)

			opts := download.VerifyOptions{
				Verbose: verbose,
			}

			valid, results, err := download.VerifyInstallation(slug, installInfo, opts)
			if err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}

			// Print summary
			var failed []download.VerifyResult
			for _, r := range results {
				if !r.Valid {
					failed = append(failed, r)
				}
			}

			fmt.Printf("\nVerified %d files\n", len(results))

			if valid {
				fmt.Printf("%s passed verification.\n", slug)
				return nil
			}

			fmt.Printf("\n%d files failed verification:\n", len(failed))
			for _, r := range failed {
				if r.Error != nil {
					fmt.Printf("  %s: %v\n", r.FilePath, r.Error)
				} else {
					fmt.Printf("  %s: hash mismatch\n", r.FilePath)
				}
			}
			fmt.Printf("\n%s is corrupted. Please reinstall.\n", slug)

			return fmt.Errorf("verification failed")
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show progress for each file")

	return cmd
}
