// main.go
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

var (
	// Version is set at build time via -ldflags
	Version = "dev"

	//go:embed CODENAME
	codename string
)

// versionString returns the formatted version string with codename
func versionString() string {
	name := strings.TrimSpace(codename)
	return fmt.Sprintf("FreeCarnival %s - %s", Version, name)
}

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
	var showVersion bool
	var configPath string

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
		Long:  fmt.Sprintf("%s\n\nFreeCarnival is a native cross-platform CLI program to install and launch IndieGala games", versionString()),
		Run: func(cmd *cobra.Command, args []string) {
			// If --version flag is set, show version and exit
			if showVersion {
				fmt.Println(versionString())
				return
			}
			// Otherwise show help
			cmd.Help()
		},
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

			// Set custom config path based on flag
			if configPath != "" {
				configPath, err := filepath.Abs(configPath)
				if err != nil {
					logger.Error("Could not get absolute path to custom config", "error", err)
				} else {
					logger.Debug("Using custom config path", "configPath", configPath)
					auth.OverrideConfigDir(configPath)
				}
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

	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "Print version information")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config-path", "", "Use custom config directory")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
