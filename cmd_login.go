package main

import (
	"fmt"
	"os"

	"github.com/gustash/freecarnival/auth"
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
			_ = client // will be reused later

			if res != nil {
				fmt.Println("Received cookies:")
				for _, c := range res.Cookies {
					fmt.Printf(
						"  %s=%s; domain=%s; path=%s; secure=%v; httpOnly=%v\n",
						c.Name,
						c.Value,
						c.Domain,
						c.Path,
						c.Secure,
						c.HttpOnly,
					)
				}
				fmt.Println()
			}

			if err != nil {
				if res != nil {
					fmt.Fprintf(os.Stderr,
						"Login failed: message=%q (http=%d)\n",
						res.Message, res.StatusCode,
					)
				} else {
					fmt.Fprintf(os.Stderr, "Login request error: %v\n", err)
				}
				os.Exit(1)
			}

			fmt.Println("Login successful.")
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
			fmt.Println("Logged out successfully.")
			return nil
		},
	}
}
