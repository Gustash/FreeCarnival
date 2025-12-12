package main

import (
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

func newInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <slug>",
		Short: "Show information about a game",
		Long: `Display detailed information about a game from your library.

Shows the game name, available versions for each platform, and installation status.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			// Load library
			library, err := auth.LoadLibrary()
			if err != nil {
				return fmt.Errorf("could not load library: %w (try running 'library' first)", err)
			}

			// Find product
			product := auth.FindProductBySlug(library, slug)
			if product == nil {
				return fmt.Errorf("game '%s' not found in your library", slug)
			}

			// Check if installed
			installInfo, err := auth.GetInstalled(slug)
			if err != nil {
				return fmt.Errorf("failed to check installed games: %w", err)
			}

			// Display game info
			fmt.Printf("Name:      %s\n", product.Name)
			fmt.Printf("Slug:      %s\n", product.SluggedName)
			fmt.Printf("ID:        %d\n", product.ID)
			fmt.Printf("Namespace: %s\n", product.Namespace)

			client, _, err := auth.LoadSessionClient()

			if err == nil {
				// Display game details
				gameDetails, err := auth.FetchGameDetails(cmd.Context(), client, product.SluggedName)

				if err == nil {
					fmt.Println()

					exePath := "None"
					if gameDetails.ExePath != "" {
						exePath = gameDetails.ExePath
					}

					args := "None"
					if gameDetails.Args != "" {
						args = gameDetails.Args
					}

					cwd := "None"
					if gameDetails.Cwd != "" {
						cwd = gameDetails.Cwd
					}

					fmt.Printf("Exe Path: %s\n", exePath)
					fmt.Printf("Args:     %s\n", args)
					fmt.Printf("Cwd:      %s\n", cwd)
				} else {
					logger.Warn("Failed to fetch game details", "error", err)
				}
			} else {
				logger.Warn("Failed to get session client", "error", err)
			}

			// Installation status
			fmt.Println()
			if installInfo != nil {
				fmt.Printf("Installed: ✓ v%s (%s)\n", installInfo.Version, installInfo.OS)
				fmt.Printf("Path:      %s\n", installInfo.InstallPath)
			} else {
				fmt.Println("Installed: No")
			}

			// Group versions by OS
			versionsByOS := make(map[auth.BuildOS][]auth.ProductVersion)
			for _, v := range product.Versions {
				versionsByOS[v.OS] = append(versionsByOS[v.OS], v)
			}

			fmt.Printf("\nAvailable versions: %d\n", len(product.Versions))

			// Display versions grouped by OS
			osOrder := []auth.BuildOS{auth.BuildOSWindows, auth.BuildOSMac, auth.BuildOSLinux}
			osNames := map[auth.BuildOS]string{
				auth.BuildOSWindows: "Windows",
				auth.BuildOSMac:     "macOS",
				auth.BuildOSLinux:   "Linux",
			}

			for _, os := range osOrder {
				versions := versionsByOS[os]
				if len(versions) == 0 {
					continue
				}

				fmt.Printf("\n  %s:\n", osNames[os])
				for _, v := range versions {
					marker := "  "
					if installInfo != nil && installInfo.Version == v.Version && installInfo.OS == os {
						marker = "✓ "
					}
					status := ""
					if v.Enabled == 0 {
						status = " (disabled)"
					}
					fmt.Printf("    %s%s%s\n", marker, v.Version, status)
					if v.Date != "" {
						fmt.Printf("        Released: %s\n", v.Date)
					}
				}
			}

			return nil
		},
	}

	return cmd
}
