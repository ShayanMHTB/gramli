package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shayanmahtabi/gramli/internal/config"
	"github.com/spf13/cobra"
)

func initCmd(st *appState) *cobra.Command {
	var force, skipDB bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local Gramli workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.EnsureDataDirs(st.settings.DataDir); err != nil {
				return fmt.Errorf("DATA_DIR_NOT_WRITABLE: %w", err)
			}
			if err := config.WriteDefaultConfig(st.settings.ConfigPath, st.settings.DataDir, force); err != nil {
				return fmt.Errorf("CONFIG_NOT_FOUND: %w", err)
			}
			if !skipDB {
				db, err := openMigratedDB(st)
				if err != nil {
					return err
				}
				_ = db.Close()
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Initialized Gramli workspace")
			fmt.Fprintf(cmd.OutOrStdout(), "Data dir: %s\n", st.settings.DataDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Database: %s\n", st.settings.DBPath)
			fmt.Fprintf(cmd.OutOrStdout(), "Config: %s\n", st.settings.ConfigPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config")
	cmd.Flags().Bool("with-sample-config", false, "Create sample config")
	cmd.Flags().BoolVar(&skipDB, "skip-db", false, "Skip database creation")
	return cmd
}

func configCmd(st *appState) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print active config path",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), st.settings.ConfigPath)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show active configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := os.ReadFile(st.settings.ConfigPath)
			if err != nil {
				return fmt.Errorf("CONFIG_NOT_FOUND: run gramli init first: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(b)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(st.settings.ConfigPath); err != nil {
				return fmt.Errorf("CONFIG_NOT_FOUND: run gramli init first")
			}
			key, value := args[0], args[1]
			if err := config.SetValue(st.settings.ConfigPath, key, value); err != nil {
				return fmt.Errorf("CONFIG_WRITE_FAILED: %w", err)
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), map[string]any{"key": key, "value": value})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", key, value)
			return nil
		},
	})
	return cmd
}

func doctorCmd(st *appState) *cobra.Command {
	var checkDB bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local Gramli setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			type check struct {
				Name   string `json:"name"`
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
			}
			checks := []check{}
			if info, err := os.Stat(st.settings.DataDir); err == nil && info.IsDir() {
				checks = append(checks, check{"data_dir", true, st.settings.DataDir})
			} else {
				checks = append(checks, check{"data_dir", false, "run gramli init"})
			}
			if _, err := os.Stat(st.settings.ConfigPath); err == nil {
				checks = append(checks, check{"config", true, st.settings.ConfigPath})
			} else {
				checks = append(checks, check{"config", false, "missing"})
			}
			if checkDB {
				db, err := openMigratedDB(st)
				if err != nil {
					checks = append(checks, check{"database", false, err.Error()})
				} else {
					checks = append(checks, check{"database", true, st.settings.DBPath})
					_ = db.Close()
				}
			}
			downloadDir := filepath.Join(st.settings.DataDir, "downloads")
			if err := os.MkdirAll(downloadDir, 0o755); err == nil {
				checks = append(checks, check{"downloads", true, downloadDir})
			} else {
				checks = append(checks, check{"downloads", false, err.Error()})
			}
			if st.settings.JSON {
				return printJSON(cmd.OutOrStdout(), map[string]any{"checks": checks})
			}
			for _, c := range checks {
				status := "ok"
				if !c.OK {
					status = "fail"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %s  %s\n", c.Name, status, c.Detail)
			}
			return nil
		},
	}
	cmd.Flags().Bool("check-auth", false, "Check authentication")
	cmd.Flags().BoolVar(&checkDB, "check-db", true, "Check database")
	cmd.Flags().Bool("check-network", false, "Check network")
	cmd.Flags().Bool("check-browser", false, "Check browser dependencies")
	return cmd
}
