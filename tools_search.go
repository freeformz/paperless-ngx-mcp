package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerSearchTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("search_autocomplete",
			mcp.WithDescription("Autocomplete search terms. Returns suggested completions for partial queries, ordered by how many of your documents contain the word."),
			mcp.WithString("term", mcp.Description("Partial search term to autocomplete"), mcp.Required()),
			withNumber("limit", mcp.Description("Maximum number of suggestions, at least 1 (default: 10)")),
		),
		handleSearchAutocomplete(client),
	)

	srv.AddTool(
		mcp.NewTool("search_global",
			mcp.WithDescription("Quick lookup across object types (documents, saved views, tags, correspondents, document types, storage paths, users, groups, mail rules, mail accounts, workflows, custom fields). Returns at most 3 results per type and is not paginated. Documents carry only id, title and created; use document_get for a document's details, and document_list with text or query for real document search."),
			mcp.WithString("query", mcp.Description("Search query, 3 to 4096 characters"), mcp.Required()),
			mcp.WithBoolean("db_only", mcp.Description("Match documents by title in the database instead of the search index, e.g. while the index is stale or being rebuilt (default: false). Other object types always match by name in the database.")),
		),
		handleSearchGlobal(client),
	)

	srv.AddTool(
		mcp.NewTool("statistics",
			mcp.WithDescription("Get document and object statistics: documents_total, documents_inbox and inbox_tags (null when there is no inbox tag; inbox_tag is a legacy single id), document_file_type_counts, character_count, tag_count, correspondent_count, document_type_count, storage_path_count and current_asn. Only root documents are counted. A superuser or a holder of the view_global_statistics permission sees totals for everything; anyone else, only for what they can see."),
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
		limit, given, errRes := optionalInt(request, "limit")
		if errRes != nil {
			return errRes, nil
		}
		// 0 is not given, as some clients pad numbers with it; paperless-ngx
		// answers 400 "Invalid limit" for 0 or less.
		if given && limit < 0 {
			return errResult("limit must be at least 1"), nil
		}
		if given && limit > 0 {
			params.Set("limit", strconv.Itoa(limit))
		}

		path := "/api/search/autocomplete/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

// Query length limits of paperless-ngx's GlobalSearchView.
const (
	minGlobalSearchQueryLen = 3
	maxGlobalSearchQueryLen = 4096
)

func handleSearchGlobal(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, errRes := getRequiredString(request, "query")
		if errRes != nil {
			return errRes, nil
		}

		// paperless-ngx counts characters (code points), not bytes, and
		// returns a 400 for queries outside this range.
		if n := utf8.RuneCountInString(query); n < minGlobalSearchQueryLen || n > maxGlobalSearchQueryLen {
			return errResult(fmt.Sprintf("query must be %d to %d characters, got %d", minGlobalSearchQueryLen, maxGlobalSearchQueryLen, n)), nil
		}

		params := url.Values{"query": {query}}
		dbOnly, errRes := optionalBool(request, "db_only", false)
		if errRes != nil {
			return errRes, nil
		}
		// Upstream treats any non-empty db_only as true, "false" included,
		// so it is only ever sent as true.
		if dbOnly {
			params.Set("db_only", "true")
		}
		path := "/api/search/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}
