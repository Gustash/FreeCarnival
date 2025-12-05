package main

import (
	"fmt"
	"os"

	"github.com/gustash/freecarnival/auth"
	"github.com/spf13/cobra"
)

func newListUpdatesCmd() *cobra.Command {
	var checkOnline bool

	cmd := &cobra.Command{
		Use:     "list-updates",
		Aliases: []string{"updates"},
		Short:   "List available updates for installed games",
		Long: `Compare installed game versions against available versions.

By default, uses the cached library. Use --check to sync with IndieGala first.`,
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

			// Load or fetch library
			var products []auth.Product
			if checkOnline {
				client, _, err := auth.LoadSessionClient()
				if err != nil {
					return fmt.Errorf("could not load session: %w", err)
				}

				_, products, err = auth.FetchUserInfo(cmd.Context(), client)
				if err != nil {
					return fmt.Errorf("failed to sync library: %w", err)
				}

				// Save updated library
				if err := auth.SaveLibrary(products); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to save library: %v\n", err)
				}
			} else {
				products, err = auth.LoadLibrary()
				if err != nil {
					return fmt.Errorf("could not load library: %w (try running 'library' first or use --check)", err)
				}
			}

			// Check each installed game for updates
			type updateInfo struct {
				slug             string
				name             string
				installedVersion string
				latestVersion    string
				os               auth.BuildOS
			}

			var updates []updateInfo
			var upToDate int

			for slug, info := range installed {
				product := auth.FindProductBySlug(products, slug)
				if product == nil {
					fmt.Fprintf(os.Stderr, "Warning: %s not found in library (may have been removed)\n", slug)
					continue
				}

				// Get latest version for the installed OS
				latest := product.GetLatestVersion(info.OS)
				if latest == nil {
					fmt.Fprintf(os.Stderr, "Warning: no versions found for %s (%s)\n", slug, info.OS)
					continue
				}

				if latest.Version != info.Version {
					updates = append(updates, updateInfo{
						slug:             slug,
						name:             product.Name,
						installedVersion: info.Version,
						latestVersion:    latest.Version,
						os:               info.OS,
					})
				} else {
					upToDate++
				}
			}

			// Display results
			if len(updates) == 0 {
				fmt.Printf("All %d installed games are up to date.\n", upToDate)
				return nil
			}

			fmt.Printf("Updates available: %d (up to date: %d)\n\n", len(updates), upToDate)

			for _, u := range updates {
				fmt.Printf("  %s\n", u.name)
				fmt.Printf("    Slug: %s\n", u.slug)
				fmt.Printf("    Installed: %s → Available: %s (%s)\n\n", u.installedVersion, u.latestVersion, u.os)
			}

			fmt.Println("Run 'freecarnival update <slug>' to update a game.")

			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnline, "check", false, "Sync with IndieGala before checking (requires login)")

	return cmd
}
