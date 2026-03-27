package main

import (
	"context"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerSearchTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("search_autocomplete",
			mcp.WithDescription("Autocomplete search terms. Returns suggested completions for partial queries."),
			mcp.WithString("term", mcp.Description("Partial search term to autocomplete"), mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Maximum number of suggestions")),
		),
		handleSearchAutocomplete(client),
	)

	srv.AddTool(
		mcp.NewTool("search_global",
			mcp.WithDescription("Global search across all object types (documents, tags, correspondents, etc.)."),
			mcp.WithString("query", mcp.Description("Search query"), mcp.Required()),
			mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
			mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleSearchGlobal(client),
	)

	srv.AddTool(
		mcp.NewTool("statistics",
			mcp.WithDescription("Get system statistics including document counts, inbox status, and storage usage."),
		),
		handleStatistics(client),
	)
}

func handleSearchAutocomplete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		term := request.GetString("term", "")
		if term == "" {
			return errResult("term is required"), nil
		}

		params := url.Values{"term": {term}}
		addIntParam(params, request, "limit", "limit")

		path := "/api/search/autocomplete/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleSearchGlobal(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := request.GetString("query", "")
		if query == "" {
			return errResult("query is required"), nil
		}

		params := url.Values{"query": {query}}
		addPaginationParams(params, request)

		path := "/api/search/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleStatistics(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := "/api/statistics/"
		resp, err := client.Get(path, nil)
		return doRequest(resp, err, "GET", path)
	}
}
