package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "paperless-ngx-mcp",
		Short: "MCP server for Paperless-ngx document management",
		Long:  "paperless-ngx-mcp is an MCP server that exposes the Paperless-ngx REST API as MCP tools for AI agents.",
	}
	cmd.Version = version
	cmd.AddCommand(mcpCmd())
	return cmd
}

func mcpCmd() *cobra.Command {
	var downloadConcurrency int

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (stdio)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flags are parsed by now. Usage helps with a flag error, not with the
			// errors from here on, whose messages say what went wrong.
			cmd.SilenceUsage = true

			baseURL := os.Getenv("PAPERLESS_URL")
			if baseURL == "" {
				return fmt.Errorf("PAPERLESS_URL environment variable is required")
			}

			token := os.Getenv("PAPERLESS_TOKEN")
			if token == "" {
				return fmt.Errorf("PAPERLESS_TOKEN environment variable is required")
			}

			// Install signal handling before the temp download dir exists, so the
			// deferred removal below runs on every exit path the process can catch.
			// Go's default action for SIGINT, SIGTERM and SIGHUP exits without
			// running defers. So does SIGPIPE, raised when a response is written to
			// stdout after the host has gone; ignored, that write fails with EPIPE
			// instead. SIGHUP and SIGPIPE are never raised on Windows.
			signal.Ignore(syscall.SIGPIPE)
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			// Until stop runs, NotifyContext swallows every signal after the
			// first. Restoring the default action once shutdown starts lets a
			// second signal end a stalled shutdown immediately, at the cost of
			// skipping the cleanup below.
			context.AfterFunc(ctx, stop)

			dl, err := NewDownloader(downloadConcurrency, downloadDirFromEnv(os.Getenv("PAPERLESS_MCP_DOWNLOAD_DIR")))
			if err != nil {
				return fmt.Errorf("create downloader: %w", err)
			}
			defer os.RemoveAll(dl.Dir())

			client := NewClient(baseURL, token)
			srv := NewServer(client, dl)
			err = server.NewStdioServer(srv).Listen(ctx, os.Stdin, os.Stdout)
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				// Listen returns ctx's error once a signal cancels it. Shutting
				// down on request is a success, and there is nothing to report.
				return nil
			}
			return err
		},
	}

	cmd.Flags().IntVar(&downloadConcurrency, "download-concurrency", 5, "Max parallel document downloads")
	return cmd
}
