package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerSavedViewTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("saved_view_list",
			mcp.WithDescription("List saved views."),
			mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
			mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleSavedViewList(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_get",
			mcp.WithDescription("Get saved view details."),
			mcp.WithNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
		),
		handleSavedViewGet(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_create",
			mcp.WithDescription("Create a new saved view."),
			mcp.WithString("name", mcp.Description("View name"), mcp.Required()),
			mcp.WithBoolean("show_on_dashboard", mcp.Description("Show on dashboard")),
			mcp.WithBoolean("show_in_sidebar", mcp.Description("Show in sidebar")),
			mcp.WithString("sort_field", mcp.Description("Sort field")),
			mcp.WithBoolean("sort_reverse", mcp.Description("Reverse sort order")),
			mcp.WithString("filter_rules", mcp.Description("JSON array of filter rule objects")),
		),
		handleSavedViewCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_update",
			mcp.WithDescription("Update a saved view."),
			mcp.WithNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("View name")),
			mcp.WithBoolean("show_on_dashboard", mcp.Description("Show on dashboard")),
			mcp.WithBoolean("show_in_sidebar", mcp.Description("Show in sidebar")),
			mcp.WithString("sort_field", mcp.Description("Sort field")),
			mcp.WithBoolean("sort_reverse", mcp.Description("Reverse sort order")),
			mcp.WithString("filter_rules", mcp.Description("JSON array of filter rule objects")),
		),
		handleSavedViewUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_delete",
			mcp.WithDescription("Delete a saved view."),
			mcp.WithNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
		),
		handleSavedViewDelete(client),
	)
}

func handleSavedViewList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)

		path := "/api/saved_views/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleSavedViewGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/saved_views/%d/", id)
		resp, err := client.Get(path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

func handleSavedViewCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("name", "")
		if name == "" {
			return errResult("name is required"), nil
		}

		body := map[string]any{"name": name}
		args := request.GetArguments()

		if _, ok := args["show_on_dashboard"]; ok {
			body["show_on_dashboard"] = request.GetBool("show_on_dashboard", false)
		}
		if _, ok := args["show_in_sidebar"]; ok {
			body["show_in_sidebar"] = request.GetBool("show_in_sidebar", false)
		}
		if v := request.GetString("sort_field", ""); v != "" {
			body["sort_field"] = v
		}
		if _, ok := args["sort_reverse"]; ok {
			body["sort_reverse"] = request.GetBool("sort_reverse", false)
		}
		if err := setJSONField(body, request, "filter_rules"); err != nil {
			return errResult(err.Error()), nil
		}

		path := "/api/saved_views/"
		resp, err := client.Post(path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleSavedViewUpdate(client *Client) server.ToolHandlerFunc {
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
		if _, ok := args["show_on_dashboard"]; ok {
			body["show_on_dashboard"] = request.GetBool("show_on_dashboard", false)
		}
		if _, ok := args["show_in_sidebar"]; ok {
			body["show_in_sidebar"] = request.GetBool("show_in_sidebar", false)
		}
		if v := request.GetString("sort_field", ""); v != "" {
			body["sort_field"] = v
		}
		if _, ok := args["sort_reverse"]; ok {
			body["sort_reverse"] = request.GetBool("sort_reverse", false)
		}
		if err := setJSONField(body, request, "filter_rules"); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/saved_views/%d/", id)
		resp, err := client.Patch(path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleSavedViewDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/saved_views/%d/", id)
		resp, err := client.Delete(path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}
