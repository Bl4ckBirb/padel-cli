package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"padel-cli/server"
	"padel-cli/storage"

	"github.com/spf13/cobra"
)

func serveCmd() *cobra.Command {
	var bind string
	var port int
	var binary string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the padel dashboard HTTP server",
		Long:  "Serves a dashboard of upcoming bookings, audit log, and a dry-run trigger. Reads ~/.config/padel/bookings.db. Localhost-only by default.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := storage.ConfigDir()
			if err != nil {
				return err
			}

			if binary == "" {
				resolved, err := os.Executable()
				if err == nil {
					binary = resolved
				} else {
					// Fall back to "padel" on PATH if we can't determine our own path.
					binary = "padel"
				}
			} else {
				if abs, err := filepath.Abs(binary); err == nil {
					binary = abs
				}
			}

			logger := log.New(os.Stdout, "serve ", log.LstdFlags)
			srv, err := server.New(bind, port, binary, configDir, logger)
			if err != nil {
				return err
			}
			defer srv.Close()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			return srv.ListenAndServe(ctx)
		},
	}

	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "Bind address (use 0.0.0.0 for LAN access)")
	cmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")
	cmd.Flags().StringVar(&binary, "binary", "", "Path to the padel binary used to spawn dry-runs (defaults to the current executable)")
	return cmd
}
