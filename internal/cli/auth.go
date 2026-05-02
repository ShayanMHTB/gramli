package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shayanmahtabi/gramli/internal/auth"
	"github.com/spf13/cobra"
)

func loginCmd(st *appState) *cobra.Command {
	var web bool
	var cookieFile, account, browser string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Create or import an authenticated session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if web {
				return fmt.Errorf("AUTH_WEB_LOGIN_UNAVAILABLE: browser login is not automated in this build\n\nUse the current browser-assisted path:\n  1. Log in to Instagram in your normal browser.\n  2. Export your own Instagram cookies to a local JSON file.\n  3. Run: gramli login --cookie-file ./cookies.json --account personal\n\nGramli will store the imported session under %s and will not print cookie values", filepath.Join(st.settings.DataDir, "sessions"))
			}
			if cookieFile == "" {
				return fmt.Errorf("no login method selected; use --web or --cookie-file")
			}
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			path, err := auth.ImportCookieFile(db.DB, filepath.Join(st.settings.DataDir, "sessions"), cookieFile, account)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Login session imported")
			fmt.Fprintf(cmd.OutOrStdout(), "Session stored: %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&web, "web", false, "Open browser login flow")
	cmd.Flags().StringVar(&browser, "browser", "default", "Browser to use")
	cmd.Flags().Bool("headless", false, "Run browser automation headlessly if supported")
	cmd.Flags().Duration("timeout", 5*time.Minute, "Login timeout")
	cmd.Flags().StringVar(&cookieFile, "cookie-file", "", "Import cookies from file")
	cmd.Flags().StringVar(&account, "account", "", "Local account alias")
	cmd.Flags().Bool("force", false, "Replace existing session")
	_ = browser
	return cmd
}

func logoutCmd(st *appState) *cobra.Command {
	var account string
	var all, deleteFiles bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Mark local sessions inactive",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			if all {
				_, err = db.Exec(`UPDATE sessions SET authenticated = 0, updated_at = ?`, time.Now().UTC())
			} else if account != "" {
				_, err = db.Exec(`UPDATE sessions SET authenticated = 0, updated_at = ? WHERE account_id IN (SELECT id FROM accounts WHERE username = ?)`, time.Now().UTC(), account)
			} else {
				_, err = db.Exec(`UPDATE sessions SET authenticated = 0, updated_at = ? WHERE id = (SELECT id FROM sessions ORDER BY updated_at DESC LIMIT 1)`, time.Now().UTC())
			}
			if err != nil {
				return err
			}
			if deleteFiles {
				fmt.Fprintln(cmd.OutOrStdout(), "Session files were not deleted by this scaffold; remove files under .gramli/sessions if needed.")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out locally")
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "Account alias")
	cmd.Flags().BoolVar(&all, "all", false, "Logout all local sessions")
	cmd.Flags().BoolVar(&deleteFiles, "delete-session-files", false, "Delete local session files")
	return cmd
}

func authCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage authentication"}
	var account string
	var checkRemote bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Show session status",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			session := auth.Status(db.DB, account)
			var remote *auth.RemoteStatus
			if checkRemote && session.Exists {
				ctx, cancel := context.WithTimeout(cmd.Context(), 25*time.Second)
				defer cancel()
				result, err := auth.CheckRemote(ctx, db.DB, session)
				if err != nil {
					return err
				}
				remote = &result
				session.Authenticated = result.Authenticated
				session.LastCheckedAt = result.CheckedAt
			}
			if st.settings.JSON {
				return printJSON(map[string]any{"account": session.Username, "sessionPath": session.CookieFilePath, "localSession": session.Exists, "authenticated": session.Authenticated, "lastChecked": session.LastCheckedAt, "remoteCheck": remote})
			}
			if !session.Exists {
				return fmt.Errorf("AUTH_SESSION_MISSING: no authenticated session found\nRun:\n  gramli login --web")
			}
			remoteLine := "not checked"
			if remote != nil {
				remoteLine = remote.Message
			}
			localLine := "present"
			if !session.Authenticated {
				localLine = "present, not authenticated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Account: @%s\nLocal session: %s\nRemote check: %s\nLast checked: %s\n", session.Username, localLine, remoteLine, session.LastCheckedAt.Format("2006-01-02 15:04"))
			return nil
		},
	}
	status.Flags().StringVar(&account, "account", "", "Account alias")
	status.Flags().BoolVar(&checkRemote, "check-remote", false, "Perform lightweight authenticated request")
	cmd.AddCommand(status)
	cmd.AddCommand(&cobra.Command{Use: "refresh", Short: "Refresh session", RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("auth refresh needs browser login support; run gramli login --cookie-file for now")
	}})
	cmd.AddCommand(&cobra.Command{Use: "cookies", Short: "Manage cookie files", RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "Session directory: %s\n", filepath.Join(st.settings.DataDir, "sessions"))
		return nil
	}})
	return cmd
}

func accountCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Manage accounts"}
	cmd.AddCommand(&cobra.Command{Use: "show", Short: "Show account metadata", RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		session := auth.Status(db.DB, "")
		if st.settings.JSON {
			return printJSON(map[string]any{"username": session.Username, "sessionPath": session.CookieFilePath, "authenticated": session.Authenticated, "lastChecked": session.LastCheckedAt})
		}
		if !session.Exists {
			return fmt.Errorf("AUTH_SESSION_MISSING: no authenticated session found")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Account: @%s\nSession: %s\n", session.Username, session.CookieFilePath)
		return nil
	}})
	var switchAccount string
	switchCmd := &cobra.Command{Use: "switch --account <alias>", Short: "Set active account", RunE: func(cmd *cobra.Command, args []string) error {
		if switchAccount == "" {
			return fmt.Errorf("--account is required")
		}
		return fmt.Errorf("account switch is not implemented yet")
	}}
	switchCmd.Flags().StringVar(&switchAccount, "account", "", "Account alias")
	cmd.AddCommand(switchCmd)
	return cmd
}
