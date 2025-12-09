package main

import (
	"context"
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/launch"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

func newLaunchCmd() *cobra.Command {
	var (
		exeName    string
		dryRun     bool
		list       bool
		winePath   string
		winePrefix string
		noWine     bool
	)

	cmd := &cobra.Command{
		Use:   "launch <slug> [-- args...]",
		Short: "Launch an installed game",
		Long: `Launch an installed game by its slug.

If multiple executables are found, you can specify which one with --exe.
Use --list to see all available executables.
Any arguments after -- are passed to the game.

For Windows games on macOS/Linux, Wine is used automatically if available.
Use --wine to specify a custom Wine path, or --no-wine to disable Wine.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			// Get game arguments (everything after --)
			var gameArgs []string
			if cmd.ArgsLenAtDash() > 0 {
				gameArgs = args[cmd.ArgsLenAtDash():]
			}

			// Check if game is installed
			installInfo, err := auth.GetInstalled(slug)
			if err != nil {
				return fmt.Errorf("failed to check installed games: %w", err)
			}
			if installInfo == nil {
				return fmt.Errorf("%s is not installed", slug)
			}

			// Find executables
			executables, err := launch.FindExecutables(installInfo.InstallPath, installInfo.OS)
			if err != nil {
				return fmt.Errorf("failed to find executables: %w", err)
			}

			if len(executables) == 0 {
				return fmt.Errorf("no executables found in %s", installInfo.InstallPath)
			}

			// If --list flag, just show executables and exit
			if list {
				fmt.Printf("Executables for %s:\n\n", slug)
				for i, exe := range executables {
					fmt.Printf("  %d. %s\n", i+1, exe.Name)
					fmt.Printf("     %s\n", exe.Path)
				}
				return nil
			}

			// Select executable (use first one if multiple found and no --exe specified)
			var exe *launch.Executable
			if exeName != "" {
				exe, err = launch.SelectExecutable(executables, exeName)
				if err != nil {
					return err
				}
			} else {
				exe = &executables[0]
				if len(executables) > 1 {
					logger.Info("Multiple executables found, using first", "exe", exe.Name)
					logger.Info("Use --list to see all, --exe <name> to specify another")
				}
			}

			logger.Info("Launching game", "name", exe.Name, "path", exe.Path)
			if len(gameArgs) > 0 {
				logger.Debug("Game arguments", "args", gameArgs)
			}

			// Dry run mode - don't actually launch
			if dryRun {
				logger.Info("Dry-run mode, not launching")
				return nil
			}

			// Launch the game
			launchOpts := &launch.Options{
				WinePath:   winePath,
				WinePrefix: winePrefix,
				NoWine:     noWine,
			}
			if err := launch.Game(cmd.Context(), exe.Path, installInfo.OS, gameArgs, launchOpts); err != nil {
				// Context cancellation (Ctrl+C) is not an error - user intentionally killed the game
				if err == context.Canceled {
					return nil
				}
				return fmt.Errorf("failed to launch game: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&exeName, "exe", "", "Name of the executable to launch (if multiple found)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be launched without actually launching")
	cmd.Flags().BoolVarP(&list, "list", "l", false, "List all available executables")
	cmd.Flags().StringVar(&winePath, "wine", "", "Path to Wine executable (for Windows games on macOS/Linux)")
	cmd.Flags().StringVar(&winePrefix, "wine-prefix", "", "WINEPREFIX to use (optional)")
	cmd.Flags().BoolVar(&noWine, "no-wine", false, "Disable Wine even for Windows executables")

	return cmd
}
