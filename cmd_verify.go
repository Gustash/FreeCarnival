package main

import (
	"fmt"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/download"
	"github.com/gustash/freecarnival/manifest"
	"github.com/gustash/freecarnival/verify"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	var (
		verbose            bool
		fix                bool
		maxDownloadWorkers int
		maxMemoryUsage     int
	)

	cmd := &cobra.Command{
		Use:   "verify <slug>",
		Short: "Verify file integrity for an installed game",
		Long: `Verify the integrity of an installed game by checking all file hashes
against the manifest. This ensures no files are corrupted or modified.

Use --fix to automatically redownload any corrupted or missing files.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Don't show usage for errors that occur during execution
			cmd.SilenceUsage = true

			ctx := cmd.Context()
			slug := args[0]

			// Check if game is installed
			installInfo, err := auth.GetInstalled(slug)
			if err != nil {
				return fmt.Errorf("failed to check installed games: %w", err)
			}
			if installInfo == nil {
				return fmt.Errorf("%s is not installed", slug)
			}

			// Load session client (needed for manifest fetch if not local)
			client, _, err := auth.LoadSessionClient()
			if err != nil {
				return fmt.Errorf("failed to load session: %w", err)
			}

			// Load library to get product info
			library, err := auth.LoadLibrary()
			if err != nil {
				return fmt.Errorf("failed to load library: %w", err)
			}

			product := auth.FindProductBySlug(library, slug)
			if product == nil {
				return fmt.Errorf("game '%s' not found in library", slug)
			}

			fmt.Printf("Verifying %s (v%s, %s) at %s...\n",
				slug, installInfo.Version, installInfo.OS, installInfo.InstallPath)

			// Load or fetch manifest
			version := product.FindVersion(installInfo.Version, installInfo.OS)
			if version == nil {
				return fmt.Errorf("version %s not found for %s", installInfo.Version, slug)
			}

			records, err := manifest.LoadOrFetchBuild(ctx, client, slug, product, version)
			if err != nil {
				return fmt.Errorf("failed to load manifest: %w", err)
			}

			opts := verify.Options{
				Verbose: verbose,
			}

			valid, results, err := verify.Installation(installInfo, records, opts)
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

			if !fix {
				fmt.Printf("\n%s has corrupted files. Run with --fix to repair.\n", slug)
				return fmt.Errorf("verification failed")
			}

			fmt.Printf("\nRepairing %d files...\n", len(failed))

			options := download.Options{
				MaxDownloadWorkers: maxDownloadWorkers,
				MaxMemoryUsage:     maxMemoryUsage,
				Verbose:            verbose,
			}
			downloader := download.New(client, product, version, options)
			if err := downloader.RepairFiles(ctx, slug, installInfo, failed); err != nil {
				return fmt.Errorf("repair failed: %w", err)
			}

			fmt.Printf("\nRepair complete. Running verification again...\n")

			// Verify again to confirm fix (reload manifest in case it changed)
			records, err = manifest.LoadOrFetchBuild(ctx, client, slug, product, version)
			if err != nil {
				return fmt.Errorf("failed to reload manifest: %w", err)
			}

			valid, _, err = verify.Installation(installInfo, records, opts)
			if err != nil {
				return fmt.Errorf("post-repair verification failed: %w", err)
			}

			if valid {
				fmt.Printf("%s has been repaired successfully.\n", slug)
				return nil
			}

			return fmt.Errorf("repair incomplete, some files still corrupted")
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show progress for each file")
	cmd.Flags().BoolVar(&fix, "fix", false, "Automatically redownload corrupted or missing files")
	cmd.Flags().IntVar(&maxDownloadWorkers, "workers", download.DefaultMaxWorkers, "Number of parallel download workers (for --fix)")
	cmd.Flags().IntVar(&maxMemoryUsage, "max-memory", download.DefaultMaxMemory, "Maximum memory usage for buffering chunks (for --fix)")

	return cmd
}
