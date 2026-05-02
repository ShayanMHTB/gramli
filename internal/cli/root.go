package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shayanmahtabi/gramli/internal/config"
	"github.com/shayanmahtabi/gramli/internal/logging"
	"github.com/shayanmahtabi/gramli/internal/storage"
	"github.com/shayanmahtabi/gramli/internal/version"
	"github.com/spf13/cobra"
)

type appState struct {
	settings config.Settings
}

func NewRootCommand() *cobra.Command {
	st := &appState{}
	root := &cobra.Command{
		Use:           "gramli",
		Short:         "Local-first Instagram saved-post archive CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			st.settings = config.Resolve(st.settings)
			f, err := logging.Setup(st.settings.LogLevel, st.settings.LogFile, st.settings.Quiet)
			if err != nil {
				return err
			}
			if f != nil {
				cmd.SetContext(context.WithValue(cmd.Context(), "logFile", f))
			}
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if f, ok := cmd.Context().Value("logFile").(*os.File); ok {
				_ = f.Close()
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			showVersion, _ := cmd.Flags().GetBool("version")
			if showVersion {
				fmt.Fprintf(cmd.OutOrStdout(), "gramli version %s\ncommit: %s\nbuilt: %s\n", version.Version, version.Commit, version.Built)
				return nil
			}
			return cmd.Help()
		},
	}
	root.SetVersionTemplate(fmt.Sprintf("gramli version %s\ncommit: %s\nbuilt: %s\n", version.Version, version.Commit, version.Built))
	root.Flags().Bool("version", false, "Print version")
	root.PersistentFlags().StringVar(&st.settings.ConfigPath, "config", "", "Path to config file")
	root.PersistentFlags().StringVar(&st.settings.DataDir, "data-dir", config.DefaultDataDir, "Gramli data directory")
	root.PersistentFlags().StringVar(&st.settings.DBPath, "db", "", "SQLite database path")
	root.PersistentFlags().StringVar(&st.settings.LogLevel, "log-level", "info", "debug, info, warn, error")
	root.PersistentFlags().StringVar(&st.settings.LogFile, "log-file", "", "Optional log file path")
	root.PersistentFlags().BoolVar(&st.settings.Quiet, "quiet", false, "Suppress non-essential output")
	root.PersistentFlags().BoolVar(&st.settings.Verbose, "verbose", false, "Enable verbose output")
	root.PersistentFlags().BoolVar(&st.settings.JSON, "json", false, "Output machine-readable JSON")
	root.PersistentFlags().BoolVar(&st.settings.NoColor, "no-color", false, "Disable colored terminal output")
	root.PersistentFlags().BoolVar(&st.settings.Yes, "yes", false, "Auto-confirm prompts")
	root.PersistentFlags().BoolVar(&st.settings.DryRun, "dry-run", false, "Show what would happen without changing anything")

	root.AddCommand(initCmd(st), configCmd(st), doctorCmd(st), dbCmd(st), authCmd(st), loginCmd(st), logoutCmd(st), accountCmd(st), postsCmd(st), collectionsCmd(st), downloadCmd(st), exportCmd(st))
	root.AddCommand(&cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	})
	return root
}

func openMigratedDB(st *appState) (*storage.DB, error) {
	db, err := storage.Open(st.settings.DBPath)
	if err != nil {
		return nil, fmt.Errorf("DB_OPEN_FAILED: %w", err)
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("DB_MIGRATION_FAILED: %w", err)
	}
	return db, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func displayPath(p string) string {
	if rel, err := filepath.Rel(".", p); err == nil && rel != "." && rel[:1] != "." {
		return rel
	}
	return p
}
