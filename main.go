// main.go
package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gustash/freecarnival/logger"
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
	var logLevel string

	// Set up context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT (Ctrl+C) and SIGTERM for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Warn("\n\nReceived interrupt signal, shutting down gracefully...")
		cancel()
	}()

	rootCmd := &cobra.Command{
		Use:   "freecarnival",
		Short: "CLI for IndieGala",
		Long:  "FreeCarnival is a native cross-platform CLI program to install and launch IndieGala games",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Set log level based on flag
			switch logLevel {
			case "debug":
				logger.SetLevel(logger.LevelDebug)
			case "info":
				logger.SetLevel(logger.LevelInfo)
			case "warn":
				logger.SetLevel(logger.LevelWarn)
			case "error":
				logger.SetLevel(logger.LevelError)
			default:
				logger.SetLevel(logger.LevelInfo)
			}
		},
	}

	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newLogoutCmd())
	rootCmd.AddCommand(newSyncCmd())
	rootCmd.AddCommand(newInstallCmd())
	rootCmd.AddCommand(newUninstallCmd())
	rootCmd.AddCommand(newUpdateCmd())
	rootCmd.AddCommand(newListUpdatesCmd())
	rootCmd.AddCommand(newVerifyCmd())
	rootCmd.AddCommand(newInfoCmd())
	rootCmd.AddCommand(newLaunchCmd())

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
