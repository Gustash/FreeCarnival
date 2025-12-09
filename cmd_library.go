package main

import (
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

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
				logger.Error("Sync failed", "error", err)
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
