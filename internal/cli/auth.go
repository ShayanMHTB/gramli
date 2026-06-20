package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shayanmahtabi/gramli/internal/accounts"
	"github.com/shayanmahtabi/gramli/internal/auth"
	"github.com/shayanmahtabi/gramli/internal/config"
	"github.com/shayanmahtabi/gramli/internal/instagram"
	"github.com/spf13/cobra"
)

func loginCmd(st *appState) *cobra.Command {
	var web bool
	var cookieFile, account string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Create or import an authenticated session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if web {
				headless, _ := cmd.Flags().GetBool("headless")
				timeout, _ := cmd.Flags().GetDuration("timeout")
				if account == "" {
					account = "default"
				}
				sessionDir := filepath.Join(st.settings.DataDir, "sessions")
				fmt.Fprintln(cmd.OutOrStdout(), "Opening Instagram in your browser.")
				fmt.Fprintln(cmd.OutOrStdout(), "Sign in with your email and password (plus 2FA if enabled).")
				fmt.Fprintf(cmd.OutOrStdout(), "The session will be saved to %s once login is detected.\n\n", sessionDir)

				cookies, err := auth.BrowserLogin(cmd.Context(), headless, timeout)
				if err != nil {
					return err
				}

				db, err := openMigratedDB(st)
				if err != nil {
					return err
				}
				defer db.Close()

				path, err := auth.SaveCookies(db.DB, sessionDir, cookies, account)
				if err != nil {
					return err
				}

				fmt.Fprintln(cmd.OutOrStdout(), "Login successful — session saved.")
				fmt.Fprintf(cmd.OutOrStdout(), "Account: %s\nSession: %s\n\n", account, path)
				fmt.Fprintln(cmd.OutOrStdout(), "Verify with: gramli auth status --check-remote")
				return nil
			}

			if cookieFile == "" {
				return fmt.Errorf("no login method selected; use --web or --cookie-file ./cookies.json")
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
	cmd.Flags().BoolVar(&web, "web", false, "Open browser login flow (requires Chrome or Chromium)")
	cmd.Flags().Bool("headless", false, "Run browser headlessly — for testing only, likely triggers bot detection")
	cmd.Flags().Duration("timeout", 5*time.Minute, "How long to wait for login to complete")
	cmd.Flags().StringVar(&cookieFile, "cookie-file", "", "Import cookies from a JSON file instead of opening a browser")
	cmd.Flags().StringVar(&account, "account", "", "Local alias for this session (default: \"default\")")
	cmd.Flags().Bool("force", false, "Replace existing session")
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
			now := time.Now().UTC()

			// Resolve which session scope we are logging out, capturing the
			// cookie file paths first so we can optionally delete them.
			var scopeSQL string
			var scopeArgs []any
			switch {
			case all:
				scopeSQL = ""
			case account != "":
				scopeSQL = ` WHERE account_id IN (SELECT id FROM accounts WHERE username = ?)`
				scopeArgs = []any{account}
			default:
				scopeSQL = ` WHERE id = (SELECT id FROM sessions ORDER BY updated_at DESC LIMIT 1)`
			}

			var paths []string
			if deleteFiles {
				rows, qerr := db.Query(`SELECT COALESCE(cookie_file_path,'') FROM sessions`+scopeSQL, scopeArgs...)
				if qerr != nil {
					return qerr
				}
				for rows.Next() {
					var p string
					if rows.Scan(&p) == nil && p != "" {
						paths = append(paths, p)
					}
				}
				rows.Close()
			}

			updateArgs := append([]any{now}, scopeArgs...)
			if _, err = db.Exec(`UPDATE sessions SET authenticated = 0, updated_at = ?`+scopeSQL, updateArgs...); err != nil {
				return err
			}

			deleted := 0
			for _, p := range paths {
				if err := os.Remove(p); err == nil {
					deleted++
				} else if !os.IsNotExist(err) {
					fmt.Fprintf(cmd.ErrOrStderr(), "Could not delete %s: %v\n", p, err)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out locally")
			if deleteFiles {
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d session file(s)\n", deleted)
			}
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
				return printJSON(cmd.OutOrStdout(), map[string]any{"account": session.Username, "sessionPath": session.CookieFilePath, "localSession": session.Exists, "authenticated": session.Authenticated, "lastChecked": session.LastCheckedAt, "remoteCheck": remote})
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
	var refreshAccount string
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Re-validate the session and refresh its stored status",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			session := auth.Status(db.DB, refreshAccount)
			if !session.Exists {
				return fmt.Errorf("AUTH_SESSION_MISSING: run gramli login first")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 25*time.Second)
			defer cancel()
			result, err := auth.CheckRemote(ctx, db.DB, session)
			if err != nil {
				return err
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), map[string]any{"account": session.Username, "authenticated": result.Authenticated, "message": result.Message, "checkedAt": result.CheckedAt})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Refreshed @%s\nStatus: %s\nChecked: %s\n", session.Username, result.Message, result.CheckedAt.Format("2006-01-02 15:04"))
			return nil
		},
	}
	refresh.Flags().StringVar(&refreshAccount, "account", "", "Account alias")
	cmd.AddCommand(refresh)
	cmd.AddCommand(&cobra.Command{Use: "cookies", Short: "Manage cookie files", RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "Session directory: %s\n", filepath.Join(st.settings.DataDir, "sessions"))
		return nil
	}})
	return cmd
}

func accountCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Manage accounts"}
	cmd.AddCommand(accountShowCmd(st))
	cmd.AddCommand(accountSyncCmd(st))
	var switchAccount string
	switchCmd := &cobra.Command{Use: "switch --account <alias>", Short: "Set active account", RunE: func(cmd *cobra.Command, args []string) error {
		if switchAccount == "" {
			return fmt.Errorf("--account is required")
		}
		db, err := openMigratedDB(st)
		if err != nil {
			return err
		}
		defer db.Close()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE username = ?`, switchAccount).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("ACCOUNT_NOT_FOUND: no local account aliased %q; run gramli login --account %s", switchAccount, switchAccount)
		}
		if err := config.SetValue(st.settings.ConfigPath, "app.active_account", switchAccount); err != nil {
			return fmt.Errorf("CONFIG_WRITE_FAILED: %w", err)
		}
		if st.settings.JSON {
			return printJSON(cmd.OutOrStdout(), map[string]any{"activeAccount": switchAccount})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Active account set to %q\n", switchAccount)
		return nil
	}}
	switchCmd.Flags().StringVar(&switchAccount, "account", "", "Account alias")
	cmd.AddCommand(switchCmd)
	return cmd
}

func accountShowCmd(st *appState) *cobra.Command {
	var alias string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show stored account profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			acc, err := accounts.Get(cmd.Context(), db.DB, alias)
			if err != nil {
				return err
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), acc)
			}
			handle := acc.Handle
			if handle == "" {
				handle = acc.Alias + " (alias; run gramli account sync)"
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Account: @%s\n", handle)
			if acc.FullName != "" {
				fmt.Fprintf(out, "Name: %s\n", acc.FullName)
			}
			if acc.ProfileSyncedAt == nil {
				fmt.Fprintln(out, "\nProfile not synced yet. Run: gramli account sync")
				return nil
			}
			privacy := "public"
			if acc.IsPrivate {
				privacy = "private"
			}
			verified := ""
			if acc.IsVerified {
				verified = " ✓ verified"
			}
			fmt.Fprintf(out, "Posts: %s   Followers: %s   Following: %s\n", formatCount(acc.MediaCount), formatCount(acc.FollowerCount), formatCount(acc.FollowingCount))
			fmt.Fprintf(out, "Account: %s%s\n", privacy, verified)
			if acc.Category != "" {
				fmt.Fprintf(out, "Category: %s\n", acc.Category)
			}
			if acc.ExternalURL != "" {
				fmt.Fprintf(out, "Link: %s\n", acc.ExternalURL)
			}
			if acc.Biography != "" {
				fmt.Fprintf(out, "Bio:\n%s\n", acc.Biography)
			}
			fmt.Fprintf(out, "\nLast synced: %s\n", acc.ProfileSyncedAt.Local().Format("2006-01-02 15:04"))
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "account", "", "Account alias")
	return cmd
}

func accountSyncCmd(st *appState) *cobra.Command {
	var alias, username string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch and store profile info for the logged-in account",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			session := auth.Status(db.DB, alias)
			if !session.Exists {
				return fmt.Errorf("AUTH_SESSION_MISSING: run gramli login first")
			}
			accountID, err := accounts.ActiveAccountID(db.DB, alias)
			if err != nil {
				return err
			}
			client := instagram.NewClient(session.CookieFilePath, filepath.Join(st.settings.DataDir, "cache", "profile"))

			var profile instagram.Profile
			if username != "" {
				profile, err = client.FetchProfileByUsername(cmd.Context(), username)
			} else {
				profile, err = client.FetchSelfProfile(cmd.Context())
			}
			if err != nil {
				return err
			}

			if st.settings.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would store profile for @%s (%d followers)\n", profile.Username, profile.FollowerCount)
				return nil
			}
			if err := accounts.SaveProfile(cmd.Context(), db.DB, accountID, profile); err != nil {
				return err
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), profile)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced @%s — %s posts, %s followers, %s following\n", profile.Username, formatCount(profile.MediaCount), formatCount(profile.FollowerCount), formatCount(profile.FollowingCount))
			fmt.Fprintln(cmd.OutOrStdout(), "View with: gramli account show")
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "account", "", "Account alias of the local session to use")
	cmd.Flags().StringVar(&username, "username", "", "Fetch a specific username instead of auto-detecting the logged-in account")
	return cmd
}

// formatCount renders large tallies compactly: 4200 -> 4.2K, 1500000 -> 1.5M.
func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
