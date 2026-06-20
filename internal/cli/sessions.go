package cli

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shayanmahtabi/gramli/internal/auth"
	"github.com/spf13/cobra"
)

func sessionsCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "List, archive, and remove local auth sessions"}
	cmd.AddCommand(sessionsListCmd(st), sessionsArchiveCmd(st), sessionsRemoveCmd(st), sessionsPruneCmd(st))
	return cmd
}

func (st *appState) archiveDir() string {
	return filepath.Join(st.settings.DataDir, "sessions", "archive")
}

func sessionsListCmd(st *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			sessions, err := auth.ListSessions(db.DB)
			if err != nil {
				return err
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), sessions)
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No sessions.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-12s %-6s %-7s %s\n", "Alias", "Type", "Active", "File", "Last checked")
			for _, s := range sessions {
				active := "no"
				if s.Authenticated {
					active = "yes"
				}
				file := "missing"
				if s.FileExists {
					file = "ok"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-12s %-6s %-7s %s\n", s.Alias, s.Type, active, file, s.LastChecked.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
}

func sessionsArchiveCmd(st *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <alias>",
		Short: "Archive a session: move its cookie file aside and drop the record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			matched, err := sessionsByAlias(db.DB, args[0])
			if err != nil {
				return err
			}
			if len(matched) == 0 {
				return fmt.Errorf("SESSION_NOT_FOUND: no session for alias %q", args[0])
			}
			now := time.Now().UTC()
			for _, s := range matched {
				dst, err := auth.ArchiveCookieFile(st.archiveDir(), s.CookieFilePath, s.Alias, now)
				if err != nil {
					return err
				}
				if err := auth.DeleteSession(db.DB, s.ID, "", false); err != nil {
					return err
				}
				if dst != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Archived %s -> %s\n", s.Alias, dst)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Archived %d session(s)\n", len(matched))
			return nil
		},
	}
}

func sessionsRemoveCmd(st *appState) *cobra.Command {
	var keepFiles bool
	cmd := &cobra.Command{
		Use:   "remove <alias>",
		Short: "Permanently remove a session record (and its cookie file)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			matched, err := sessionsByAlias(db.DB, args[0])
			if err != nil {
				return err
			}
			if len(matched) == 0 {
				return fmt.Errorf("SESSION_NOT_FOUND: no session for alias %q", args[0])
			}
			if st.settings.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would remove %d session(s)\n", len(matched))
				return nil
			}
			if !st.settings.Yes {
				return fmt.Errorf("refusing to remove %d session(s) without --yes (use --dry-run to preview)", len(matched))
			}
			for _, s := range matched {
				if err := auth.DeleteSession(db.DB, s.ID, s.CookieFilePath, !keepFiles); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %d session(s)\n", len(matched))
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepFiles, "keep-files", false, "Keep cookie files on disk")
	return cmd
}

func sessionsPruneCmd(st *appState) *cobra.Command {
	var archive bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Archive or remove all inactive (logged-out) sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			all, err := auth.ListSessions(db.DB)
			if err != nil {
				return err
			}
			var inactive []auth.SessionInfo
			for _, s := range all {
				if !s.Authenticated {
					inactive = append(inactive, s)
				}
			}
			if len(inactive) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No inactive sessions to prune.")
				return nil
			}
			if st.settings.DryRun {
				verb := "remove"
				if archive {
					verb = "archive"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Would %s %d inactive session(s)\n", verb, len(inactive))
				return nil
			}
			if !st.settings.Yes {
				return fmt.Errorf("refusing to prune %d session(s) without --yes (use --dry-run to preview)", len(inactive))
			}
			now := time.Now().UTC()
			for _, s := range inactive {
				if archive {
					if _, err := auth.ArchiveCookieFile(st.archiveDir(), s.CookieFilePath, s.Alias, now); err != nil {
						return err
					}
					if err := auth.DeleteSession(db.DB, s.ID, "", false); err != nil {
						return err
					}
				} else if err := auth.DeleteSession(db.DB, s.ID, s.CookieFilePath, true); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pruned %d inactive session(s)\n", len(inactive))
			return nil
		},
	}
	cmd.Flags().BoolVar(&archive, "archive", false, "Archive instead of delete")
	return cmd
}

func sessionsByAlias(db *sql.DB, alias string) ([]auth.SessionInfo, error) {
	all, err := auth.ListSessions(db)
	if err != nil {
		return nil, err
	}
	var out []auth.SessionInfo
	for _, s := range all {
		if s.Alias == alias {
			out = append(out, s)
		}
	}
	return out, nil
}
