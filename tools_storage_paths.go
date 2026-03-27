package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerStoragePathTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("storage_path_list",
			mcp.WithDescription("List storage paths with optional filtering."),
			mcp.WithString("name", mcp.Description("Filter by name (icontains)")),
			mcp.WithString("ordering", mcp.Description("Sort field")),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleStoragePathList(client),
	)

	srv.AddTool(
		mcp.NewTool("storage_path_get",
			mcp.WithDescription("Get storage path details."),
			withNumber("id", mcp.Description("Storage path ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/storage_paths/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("storage_path_create",
			mcp.WithDescription("Create a new storage path."),
			mcp.WithString("name", mcp.Description("Storage path name"), mcp.Required()),
			mcp.WithString("path", mcp.Description("Path template"), mcp.Required()),
			withNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
		),
		handleStoragePathCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("storage_path_update",
			mcp.WithDescription("Update a storage path."),
			withNumber("id", mcp.Description("Storage path ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Storage path name")),
			mcp.WithString("path", mcp.Description("Path template")),
			withNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
		),
		handleStoragePathUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("storage_path_delete",
			mcp.WithDescription("Delete a storage path."),
			withNumber("id", mcp.Description("Storage path ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/storage_paths/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("storage_path_test",
			mcp.WithDescription("Test a storage path template against a document to see the resulting path."),
			mcp.WithString("path", mcp.Description("Path template to test"), mcp.Required()),
			withNumber("document_id", mcp.Description("Document ID to test against")),
		),
		handleStoragePathTest(client),
	)
}

func handleStoragePathList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "name", "name__icontains")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/storage_paths/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleStoragePathCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}
		pathTemplate, errRes := getRequiredString(request, "path")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"name": name, "path": pathTemplate}
		args := request.GetArguments()

		if v := request.GetString("match", ""); v != "" {
			body["match"] = v
		}
		if _, ok := args["matching_algorithm"]; ok {
			body["matching_algorithm"] = int(request.GetFloat("matching_algorithm", 0))
		}
		if _, ok := args["is_insensitive"]; ok {
			body["is_insensitive"] = request.GetBool("is_insensitive", false)
		}

		p := "/api/storage_paths/"
		resp, err := client.Post(ctx, p, body)
		return doRequest(resp, err, "POST", p)
	}
}

func handleStoragePathUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		args := request.GetArguments()

		if v := request.GetString("name", ""); v != "" {
			body["name"] = v
		}
		if v := request.GetString("path", ""); v != "" {
			body["path"] = v
		}
		if v := request.GetString("match", ""); v != "" {
			body["match"] = v
		}
		if _, ok := args["matching_algorithm"]; ok {
			body["matching_algorithm"] = int(request.GetFloat("matching_algorithm", 0))
		}
		if _, ok := args["is_insensitive"]; ok {
			body["is_insensitive"] = request.GetBool("is_insensitive", false)
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		p := fmt.Sprintf("/api/storage_paths/%d/", id)
		resp, err := client.Patch(ctx, p, body)
		return doRequest(resp, err, "PATCH", p)
	}
}

func handleStoragePathTest(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pathTemplate, errRes := getRequiredString(request, "path")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"path": pathTemplate}
		args := request.GetArguments()
		if _, ok := args["document_id"]; ok {
			body["document_id"] = int(request.GetFloat("document_id", 0))
		}

		p := "/api/storage_paths/test/"
		resp, err := client.Post(ctx, p, body)
		return doRequest(resp, err, "POST", p)
	}
}
