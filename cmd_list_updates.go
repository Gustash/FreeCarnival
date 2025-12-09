package main

import (
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

func newListUpdatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-updates",
		Short: "List available updates for installed games",
		Long: `Check all installed games for available updates.

Compares the installed version with the latest version available in your library
for each game.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load installed games
			installed, err := auth.LoadInstalled()
			if err != nil {
				return fmt.Errorf("failed to load installed games: %w", err)
			}

			if len(installed) == 0 {
				fmt.Println("No games installed.")
				return nil
			}

			// Load library
			library, err := auth.LoadLibrary()
			if err != nil {
				return fmt.Errorf("could not load library: %w (try running 'sync' first)", err)
			}

			// Check each installed game for updates
			var updatesAvailable []struct {
				slug           string
				name           string
				currentVersion string
				latestVersion  string
			}

			for slug, installInfo := range installed {
				logger.Debug("Checking for updates", "slug", slug)

				product := auth.FindProductBySlug(library, slug)
				if product == nil {
					logger.Warn("Game not found in library (try running 'sync')", "slug", slug)
					continue
				}

				latestVersion := product.GetLatestVersion(installInfo.OS)
				if latestVersion == nil {
					logger.Warn("No version found for OS", "slug", slug, "os", installInfo.OS)
					continue
				}

				if installInfo.Version != latestVersion.Version {
					updatesAvailable = append(updatesAvailable, struct {
						slug           string
						name           string
						currentVersion string
						latestVersion  string
					}{
						slug:           slug,
						name:           product.Name,
						currentVersion: installInfo.Version,
						latestVersion:  latestVersion.Version,
					})
				}
			}

			// Print results
			fmt.Println()
			if len(updatesAvailable) == 0 {
				fmt.Println("All games are up to date! ✓")
			} else {
				fmt.Printf("Updates available for %d game(s):\n\n", len(updatesAvailable))
				for _, update := range updatesAvailable {
					fmt.Printf("  %s (%s)\n", update.name, update.slug)
					fmt.Printf("    Current: v%s → Latest: v%s\n", update.currentVersion, update.latestVersion)
					fmt.Printf("    Run: freecarnival update %s\n\n", update.slug)
				}
			}

			return nil
		},
	}

	return cmd
}

