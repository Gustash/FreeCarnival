package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/download"
	"github.com/gustash/freecarnival/logger"
	"github.com/gustash/freecarnival/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		version            string
		maxDownloadWorkers int
		maxMemoryUsage     int
		infoOnly           bool
		skipVerify         bool
		verbose            bool
	)

	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update an installed game",
		Long:  `Update an installed game to the latest version (or a specific version).`,
		Args:  cobra.ExactArgs(1),
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

			// Find old version
			oldVersion := product.FindVersion(installInfo.Version, installInfo.OS)
			if oldVersion == nil {
				return fmt.Errorf("installed version %s not found in library", installInfo.Version)
			}

			// Find new version
			var newVersion *auth.ProductVersion
			if version != "" {
				newVersion = product.FindVersion(version, installInfo.OS)
				if newVersion == nil {
					return fmt.Errorf("version '%s' not found for %s (OS: %s)", version, slug, installInfo.OS)
				}
			} else {
				newVersion = product.GetLatestVersion(installInfo.OS)
				if newVersion == nil {
					return fmt.Errorf("no available version found for %s (OS: %s)", slug, installInfo.OS)
				}
			}

			// Check if already up to date
			if !infoOnly && oldVersion.Version == newVersion.Version {
				logger.Info("Game is already up to date",
					"name", product.Name,
					"version", newVersion.Version)
				return nil
			}

			logger.Info("Updating game",
				"name", product.Name,
				"from", oldVersion.Version,
				"to", newVersion.Version,
				"os", newVersion.OS)

			// Load session for authenticated downloads
			client, _, err := auth.LoadSessionClient()
			if err != nil {
				return fmt.Errorf("could not load session: %w (try running 'login' first)", err)
			}

			// Create update options
			opts := download.Options{
				MaxDownloadWorkers: maxDownloadWorkers,
				MaxMemoryUsage:     maxMemoryUsage,
				SkipVerify:         skipVerify,
				InfoOnly:           infoOnly,
				Verbose:            verbose,
			}

			// Create updater and start update
			updater := update.New(client, product, oldVersion, newVersion, installInfo.InstallPath, opts)
			err = updater.Update(cmd.Context())

			// Check if update was cancelled (Ctrl+C)
			if errors.Is(err, context.Canceled) {
				return nil // Exit cleanly without error
			}

			if err != nil {
				return fmt.Errorf("update failed: %w", err)
			}

			if !infoOnly {
				// Update install info
				installInfo.Version = newVersion.Version
				installInfo.OS = newVersion.OS
				if err := auth.AddInstalled(slug, installInfo); err != nil {
					logger.Warn("Failed to update install info", "error", err)
				}

				logger.Info("Update complete",
					"name", product.Name,
					"version", newVersion.Version)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&version, "version", "v", "", "Specific version to update to (default: latest)")
	cmd.Flags().IntVar(&maxDownloadWorkers, "workers", defaultMaxWorkers, "Number of parallel download workers")
	cmd.Flags().IntVar(&maxMemoryUsage, "max-memory", defaultMaxMemory, "Maximum memory usage for buffering chunks (bytes)")
	cmd.Flags().BoolVarP(&infoOnly, "info", "i", false, "Show update info without updating")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip SHA verification of downloaded chunks")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show per-file progress")

	return cmd
}
