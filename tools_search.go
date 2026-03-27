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
			withNumber("limit", mcp.Description("Maximum number of suggestions")),
		),
		handleSearchAutocomplete(client),
	)

	srv.AddTool(
		mcp.NewTool("search_global",
			mcp.WithDescription("Global search across all object types (documents, tags, correspondents, etc.)."),
			mcp.WithString("query", mcp.Description("Search query"), mcp.Required()),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleSearchGlobal(client),
	)

	srv.AddTool(
		mcp.NewTool("statistics",
			mcp.WithDescription("Get system statistics including document counts, inbox status, and storage usage."),
		),
		handleSimpleGet(client, "/api/statistics/"),
	)
}

func handleSearchAutocomplete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		term, errRes := getRequiredString(request, "term")
		if errRes != nil {
			return errRes, nil
		}

		params := url.Values{"term": {term}}
		addIntParam(params, request, "limit", "limit")

		path := "/api/search/autocomplete/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleSearchGlobal(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, errRes := getRequiredString(request, "query")
		if errRes != nil {
			return errRes, nil
		}

		params := url.Values{"query": {query}}
		addPaginationParams(params, request)

		path := "/api/search/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}
