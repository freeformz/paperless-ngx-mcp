package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerCorrespondentTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("correspondent_list",
			mcp.WithDescription("List correspondents with optional filtering."),
			mcp.WithString("name", mcp.Description("Filter by name (icontains)")),
			mcp.WithString("ordering", mcp.Description("Sort field")),
			mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
			mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleCorrespondentList(client),
	)

	srv.AddTool(
		mcp.NewTool("correspondent_get",
			mcp.WithDescription("Get correspondent details."),
			mcp.WithNumber("id", mcp.Description("Correspondent ID"), mcp.Required()),
		),
		handleCorrespondentGet(client),
	)

	srv.AddTool(
		mcp.NewTool("correspondent_create",
			mcp.WithDescription("Create a new correspondent."),
			mcp.WithString("name", mcp.Description("Correspondent name"), mcp.Required()),
			mcp.WithNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
		),
		handleCorrespondentCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("correspondent_update",
			mcp.WithDescription("Update a correspondent."),
			mcp.WithNumber("id", mcp.Description("Correspondent ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Correspondent name")),
			mcp.WithNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
		),
		handleCorrespondentUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("correspondent_delete",
			mcp.WithDescription("Delete a correspondent."),
			mcp.WithNumber("id", mcp.Description("Correspondent ID"), mcp.Required()),
		),
		handleCorrespondentDelete(client),
	)
}

func handleCorrespondentList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "name", "name__icontains")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/correspondents/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleCorrespondentGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/correspondents/%d/", id)
		resp, err := client.Get(path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

func handleCorrespondentCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("name", "")
		if name == "" {
			return errResult("name is required"), nil
		}

		body := map[string]any{"name": name}
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

		path := "/api/correspondents/"
		resp, err := client.Post(path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleCorrespondentUpdate(client *Client) server.ToolHandlerFunc {
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

		path := fmt.Sprintf("/api/correspondents/%d/", id)
		resp, err := client.Patch(path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleCorrespondentDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/correspondents/%d/", id)
		resp, err := client.Delete(path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}
