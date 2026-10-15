package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// minVersionSavedViewOwnership is the first Paperless-ngx release whose saved
// view endpoints honour owner and set_permissions. Up to v2.20.15,
// SavedViewViewSet filters saved views by owner=request.user and forces the
// requesting user as owner in perform_create: owner: null is ignored on create,
// and an owner change on update makes the view unreachable for the token user.
// From v3.0.0 (first in v3.0.0-beta.rc1) it filters with
// ObjectOwnedOrGrantedPermissionsFilter and keeps the requested owner.
const minVersionSavedViewOwnership = "3.0.0"

func registerSavedViewTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("saved_view_list",
			mcp.WithDescription("List saved views."),
			withFullPerms(),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handlePaginatedList(client, "/api/saved_views/", addFullPerms),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_get",
			mcp.WithDescription("Get saved view details."),
			withNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
			withFullPerms(),
		),
		handleGetByID(client, "/api/saved_views/%d/", addFullPerms),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_create",
			mcp.WithDescription("Create a new saved view. owner and set_permissions require Paperless-ngx 3.0.0+; older servers ignore them for saved views, so the tool refuses them there."),
			mcp.WithString("name", mcp.Description("View name"), mcp.Required()),
			mcp.WithBoolean("show_on_dashboard", mcp.Description("Show on dashboard")),
			mcp.WithBoolean("show_in_sidebar", mcp.Description("Show in sidebar")),
			mcp.WithString("sort_field", mcp.Description("Sort field")),
			mcp.WithBoolean("sort_reverse", mcp.Description("Reverse sort order")),
			mcp.WithString("filter_rules", mcp.Description("JSON array of filter rule objects")),
			withOwnership(),
		),
		handleSavedViewCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_update",
			mcp.WithDescription("Update a saved view. owner and set_permissions require Paperless-ngx 3.0.0+; on older servers an owner change would make the view unreachable, so the tool refuses them there."),
			withNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("View name")),
			mcp.WithBoolean("show_on_dashboard", mcp.Description("Show on dashboard")),
			mcp.WithBoolean("show_in_sidebar", mcp.Description("Show in sidebar")),
			mcp.WithString("sort_field", mcp.Description("Sort field")),
			mcp.WithBoolean("sort_reverse", mcp.Description("Reverse sort order")),
			mcp.WithString("filter_rules", mcp.Description("JSON array of filter rule objects")),
			withOwnership(),
		),
		handleSavedViewUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_delete",
			mcp.WithDescription("Delete a saved view."),
			withNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/saved_views/%d/"),
	)
}

func handleSavedViewCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
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
		if err := setOwnership(body, request); err != nil {
			return errResult(err.Error()), nil
		}
		if errRes := requireSavedViewOwnershipSupport(ctx, client, body, "saved_view_create"); errRes != nil {
			return errRes, nil
		}

		path := "/api/saved_views/"
		resp, err := client.Post(ctx, path, body)
		result, _ := doRequest(resp, err, "POST", path)
		return checkSavedViewOwner(result, body, "created"), nil
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
		if err := setOwnership(body, request); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}
		if errRes := requireSavedViewOwnershipSupport(ctx, client, body, "saved_view_update"); errRes != nil {
			return errRes, nil
		}

		path := fmt.Sprintf("/api/saved_views/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		result, _ := doRequest(resp, err, "PATCH", path)
		return checkSavedViewOwner(result, body, "updated"), nil
	}
}

// requireSavedViewOwnershipSupport refuses owner and set_permissions on servers
// older than minVersionSavedViewOwnership. Other saved view fields still work
// there, so the check only applies when one of the two is being sent.
func requireSavedViewOwnershipSupport(ctx context.Context, client *Client, body map[string]any, tool string) *mcp.CallToolResult {
	_, hasOwner := body["owner"]
	_, hasPerms := body["set_permissions"]
	if !hasOwner && !hasPerms {
		return nil
	}
	return requireServerVersion(ctx, client, minVersionSavedViewOwnership, tool+" with owner or set_permissions")
}

// checkSavedViewOwner turns a successful response into an error when the
// returned owner differs from the requested one, which is how an older server
// that ignores owner shows up even if its version could not be determined.
// Responses without an owner field are passed through unchanged.
func checkSavedViewOwner(result *mcp.CallToolResult, body map[string]any, action string) *mcp.CallToolResult {
	want, requested := body["owner"]
	if !requested || result == nil || result.IsError || len(result.Content) == 0 {
		return result
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return result
	}
	var view map[string]any
	if json.Unmarshal([]byte(text.Text), &view) != nil {
		return result
	}
	got, present := view["owner"]
	if !present {
		return result
	}
	if f, isNum := got.(float64); isNum {
		got = int(f)
	}
	if got == want {
		return result
	}
	return errResult(fmt.Sprintf("saved view %v was %s with owner %s instead of the requested owner %s; this Paperless-ngx server does not honour owner on saved views (requires %s or later)",
		view["id"], action, ownerString(got), ownerString(want), minVersionSavedViewOwnership))
}

func ownerString(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprint(v)
}
