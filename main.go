package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "paperless-ngx-mcp",
		Short: "MCP server for Paperless-ngx document management",
		Long:  "paperless-ngx-mcp is an MCP server that exposes the Paperless-ngx REST API as MCP tools for AI agents.",
	}
	rootCmd.Version = version
	rootCmd.AddCommand(mcpCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func mcpCmd() *cobra.Command {
	var downloadConcurrency int

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (stdio)",
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL := os.Getenv("PAPERLESS_URL")
			if baseURL == "" {
				return fmt.Errorf("PAPERLESS_URL environment variable is required")
			}

			token := os.Getenv("PAPERLESS_TOKEN")
			if token == "" {
				return fmt.Errorf("PAPERLESS_TOKEN environment variable is required")
			}

			dl, err := NewDownloader(downloadConcurrency)
			if err != nil {
				return fmt.Errorf("create downloader: %w", err)
			}
			defer os.RemoveAll(dl.Dir())

			client := NewClient(baseURL, token)
			srv := NewServer(client, dl)
			return server.ServeStdio(srv)
		},
	}

	cmd.Flags().IntVar(&downloadConcurrency, "download-concurrency", 5, "Max parallel document downloads")
	return cmd
}
