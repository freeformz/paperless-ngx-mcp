package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerShareLinkTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("share_link_list",
			mcp.WithDescription("List share links."),
			mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
			mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handlePaginatedList(client, "/api/share_links/"),
	)

	srv.AddTool(
		mcp.NewTool("share_link_get",
			mcp.WithDescription("Get share link details."),
			mcp.WithNumber("id", mcp.Description("Share link ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/share_links/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("share_link_create",
			mcp.WithDescription("Create a new share link for a document."),
			mcp.WithNumber("document", mcp.Description("Document ID"), mcp.Required()),
			mcp.WithString("expiration", mcp.Description("Expiration date (YYYY-MM-DD or ISO 8601)")),
			mcp.WithString("slug", mcp.Description("Custom URL slug")),
			mcp.WithBoolean("file_version_archive", mcp.Description("Share archived version (default: true)")),
		),
		handleShareLinkCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("share_link_update",
			mcp.WithDescription("Update a share link."),
			mcp.WithNumber("id", mcp.Description("Share link ID"), mcp.Required()),
			mcp.WithString("expiration", mcp.Description("Expiration date")),
		),
		handleShareLinkUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("share_link_delete",
			mcp.WithDescription("Delete a share link."),
			mcp.WithNumber("id", mcp.Description("Share link ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/share_links/%d/"),
	)
}

func handleShareLinkCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		docID, errRes := getRequiredInt(request, "document")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"document": docID}
		args := request.GetArguments()

		if v := request.GetString("expiration", ""); v != "" {
			body["expiration"] = v
		}
		if v := request.GetString("slug", ""); v != "" {
			body["slug"] = v
		}
		if _, ok := args["file_version_archive"]; ok {
			body["file_version"] = "archive"
			if !request.GetBool("file_version_archive", true) {
				body["file_version"] = "original"
			}
		}

		path := "/api/share_links/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleShareLinkUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		if v := request.GetString("expiration", ""); v != "" {
			body["expiration"] = v
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/share_links/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}
