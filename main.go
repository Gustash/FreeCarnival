// main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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
	// Set up context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT (Ctrl+C) and SIGTERM for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\n\nReceived interrupt signal, shutting down gracefully...")
		cancel()
	}()

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
	rootCmd.AddCommand(newInfoCmd())
	rootCmd.AddCommand(newLaunchCmd())

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
