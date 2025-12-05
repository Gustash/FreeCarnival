// main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

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
	rootCmd.AddCommand(newListUpdatesCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
