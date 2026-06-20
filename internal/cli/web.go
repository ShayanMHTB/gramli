package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/shayanmahtabi/gramli/internal/web"
	"github.com/spf13/cobra"
)

func webCmd(st *appState) *cobra.Command {
	var host string
	var port int
	var open, noRemote bool
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Browse your local archive in a web UI",
		Long:  "Serve a local, read-only web interface over your Gramli database and downloaded media.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigratedDB(st)
			if err != nil {
				return err
			}
			defer db.Close()

			if host != "127.0.0.1" && host != "localhost" {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: binding to %s exposes your archive beyond this machine.\n", host)
			}

			srv, err := web.New(db.DB, web.Options{
				DataDir:        st.settings.DataDir,
				RemoteFallback: !noRemote,
			})
			if err != nil {
				return err
			}

			addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("WEB_LISTEN_FAILED: %w", err)
			}
			url := fmt.Sprintf("http://%s/", ln.Addr().String())

			httpSrv := &http.Server{Handler: srv.Handler()}
			errCh := make(chan error, 1)
			go func() { errCh <- httpSrv.Serve(ln) }()

			fmt.Fprintf(cmd.OutOrStdout(), "Gramli web UI: %s\n", url)
			fmt.Fprintln(cmd.OutOrStdout(), "Read-only. Press Ctrl+C to stop.")
			if open {
				openBrowser(url)
			}

			select {
			case <-cmd.Context().Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpSrv.Shutdown(shutdownCtx)
				fmt.Fprintln(cmd.OutOrStdout(), "\nStopped.")
				return nil
			case err := <-errCh:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			}
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host/interface to bind")
	cmd.Flags().IntVar(&port, "port", 8787, "Port to listen on (0 picks a free port)")
	cmd.Flags().BoolVar(&open, "open", false, "Open the UI in your default browser")
	cmd.Flags().BoolVar(&noRemote, "no-remote-thumbnails", false, "Never load remote thumbnails; show only local media")
	return cmd
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	_ = exec.Command(cmd, args...).Start()
}
