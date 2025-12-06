package main

import (
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/verify"
	"github.com/spf13/cobra"
)

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

			opts := verify.Options{
				Verbose: verbose,
			}

			valid, results, err := verify.Installation(slug, installInfo, opts)
			if err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}

			// Print summary
			var failed []verify.Result
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
