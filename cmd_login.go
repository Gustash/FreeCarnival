package main

import (
	"fmt"
	"os"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/logger"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	var email string
	var password string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to your IndieGala account",
		RunE: func(cmd *cobra.Command, args []string) error {

			if email == "" || password == "" {
				return fmt.Errorf("--email and --password are required")
			}

			client, res, err := auth.Login(cmd.Context(), email, password)

			if res != nil {
				logger.Debug("Received cookies")
				for _, c := range res.Cookies {
					logger.Debug("Cookie",
						"name", c.Name,
						"domain", c.Domain,
						"path", c.Path,
						"secure", c.Secure,
						"httpOnly", c.HttpOnly)
				}
			}

			if err != nil {
				if res != nil {
					logger.Error("Login failed",
						"message", res.Message,
						"status", res.StatusCode)
				} else {
					logger.Error("Login request error", "error", err)
				}
				os.Exit(1)
			}

			logger.Info("Login successful. Syncing library...")

			ui, products, err := auth.FetchUserInfo(cmd.Context(), client)
			if err != nil {
				logger.Warn("Sync failed. Try running `sync` manually.", "error", err)
				return nil
			}

			if err := auth.SaveUserInfo(ui); err != nil {
				logger.Warn("failed to save user info", "error", err)
				return nil
			}

			if err := auth.SaveLibrary(products); err != nil {
				logger.Warn("failed to save library", "error", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "IndieGala username/email (required)")
	cmd.Flags().StringVar(&password, "password", "", "IndieGala password (required)")
	cmd.MarkFlagRequired("email")
	cmd.MarkFlagRequired("password")

	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from your IndieGala account",
		Long:  "Removes the saved session, effectively logging you out. You will need to log in again to use authenticated commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.ClearSession(); err != nil {
				return fmt.Errorf("failed to clear session: %w", err)
			}
			logger.Info("Logged out successfully")
			return nil
		},
	}
}
