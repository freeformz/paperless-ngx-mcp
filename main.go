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
	return &cobra.Command{
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

			client := NewClient(baseURL, token)
			srv := NewServer(client)
			return server.ServeStdio(srv)
		},
	}
}
