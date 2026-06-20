package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func dbCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "db", Short: "Manage local SQLite database"}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show database status",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			info, _ := os.Stat(st.settings.DBPath)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), map[string]any{
					"path": st.settings.DBPath, "migrationVersion": db.MigrationVersion(), "accounts": db.Count("accounts"), "posts": db.Count("posts"), "media": db.Count("media"), "collections": db.Count("collections"), "sizeBytes": size,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "DB path: %s\nMigration version: %d\nAccounts: %d\nPosts: %d\nMedia items: %d\nCollections: %d\nDB size: %d bytes\n", st.settings.DBPath, db.MigrationVersion(), db.Count("accounts"), db.Count("posts"), db.Count("media"), db.Count("collections"), size)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "migrate",
		Short: "Run migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			fmt.Fprintf(cmd.OutOrStdout(), "Migration version: %d\n", db.MigrationVersion())
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Reset local database",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !st.settings.Yes {
				return fmt.Errorf("db reset is destructive; rerun with --yes")
			}
			if err := os.Remove(st.settings.DBPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			fmt.Fprintf(cmd.OutOrStdout(), "Reset database: %s\n", st.settings.DBPath)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "vacuum",
		Short: "Vacuum SQLite database",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()
			_, err = db.Exec("VACUUM")
			return err
		},
	})
	return cmd
}
