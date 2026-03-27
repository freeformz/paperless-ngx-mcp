package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerUserTools(srv *server.MCPServer, client *Client) {
	// Users
	srv.AddTool(
		mcp.NewTool("user_list",
			mcp.WithDescription("List users."),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handlePaginatedList(client, "/api/users/"),
	)
	srv.AddTool(
		mcp.NewTool("user_get",
			mcp.WithDescription("Get user details."),
			withNumber("id", mcp.Description("User ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/users/%d/"),
	)
	srv.AddTool(
		mcp.NewTool("user_create",
			mcp.WithDescription("Create a new user."),
			mcp.WithString("username", mcp.Description("Username"), mcp.Required()),
			mcp.WithString("password", mcp.Description("Password"), mcp.Required()),
			mcp.WithString("email", mcp.Description("Email address")),
			mcp.WithString("first_name", mcp.Description("First name")),
			mcp.WithString("last_name", mcp.Description("Last name")),
			mcp.WithBoolean("is_active", mcp.Description("Active status")),
			mcp.WithBoolean("is_staff", mcp.Description("Staff status")),
			mcp.WithBoolean("is_superuser", mcp.Description("Superuser status")),
			mcp.WithString("groups", mcp.Description("JSON array of group IDs")),
		),
		handleUserCreate(client),
	)
	srv.AddTool(
		mcp.NewTool("user_update",
			mcp.WithDescription("Update a user."),
			withNumber("id", mcp.Description("User ID"), mcp.Required()),
			mcp.WithString("username", mcp.Description("Username")),
			mcp.WithString("password", mcp.Description("Password")),
			mcp.WithString("email", mcp.Description("Email address")),
			mcp.WithString("first_name", mcp.Description("First name")),
			mcp.WithString("last_name", mcp.Description("Last name")),
			mcp.WithBoolean("is_active", mcp.Description("Active status")),
			mcp.WithBoolean("is_staff", mcp.Description("Staff status")),
			mcp.WithBoolean("is_superuser", mcp.Description("Superuser status")),
			mcp.WithString("groups", mcp.Description("JSON array of group IDs")),
		),
		handleUserUpdate(client),
	)
	srv.AddTool(
		mcp.NewTool("user_delete",
			mcp.WithDescription("Delete a user."),
			withNumber("id", mcp.Description("User ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/users/%d/"),
	)
	srv.AddTool(
		mcp.NewTool("user_deactivate_totp",
			mcp.WithDescription("Deactivate TOTP two-factor authentication for a user (admin only)."),
			withNumber("id", mcp.Description("User ID"), mcp.Required()),
		),
		handleUserDeactivateTotp(client),
	)

	// Groups
	srv.AddTool(
		mcp.NewTool("group_list",
			mcp.WithDescription("List groups."),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handlePaginatedList(client, "/api/groups/"),
	)
	srv.AddTool(
		mcp.NewTool("group_get",
			mcp.WithDescription("Get group details."),
			withNumber("id", mcp.Description("Group ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/groups/%d/"),
	)
	srv.AddTool(
		mcp.NewTool("group_create",
			mcp.WithDescription("Create a new group."),
			mcp.WithString("name", mcp.Description("Group name"), mcp.Required()),
			mcp.WithString("permissions", mcp.Description("JSON array of permission codenames")),
		),
		handleGroupCreate(client),
	)
	srv.AddTool(
		mcp.NewTool("group_update",
			mcp.WithDescription("Update a group."),
			withNumber("id", mcp.Description("Group ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Group name")),
			mcp.WithString("permissions", mcp.Description("JSON array of permission codenames")),
		),
		handleGroupUpdate(client),
	)
	srv.AddTool(
		mcp.NewTool("group_delete",
			mcp.WithDescription("Delete a group."),
			withNumber("id", mcp.Description("Group ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/groups/%d/"),
	)

	// Profile
	srv.AddTool(
		mcp.NewTool("profile_get",
			mcp.WithDescription("Get the current user's profile."),
		),
		handleSimpleGet(client, "/api/profile/"),
	)
	srv.AddTool(
		mcp.NewTool("profile_update",
			mcp.WithDescription("Update the current user's profile."),
			mcp.WithString("email", mcp.Description("Email address")),
			mcp.WithString("first_name", mcp.Description("First name")),
			mcp.WithString("last_name", mcp.Description("Last name")),
			mcp.WithString("password", mcp.Description("New password")),
		),
		handleProfileUpdate(client),
	)
}

func handleUserCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		username, errRes := getRequiredString(request, "username")
		if errRes != nil {
			return errRes, nil
		}
		password, errRes := getRequiredString(request, "password")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"username": username, "password": password}
		args := request.GetArguments()

		if v := request.GetString("email", ""); v != "" {
			body["email"] = v
		}
		if v := request.GetString("first_name", ""); v != "" {
			body["first_name"] = v
		}
		if v := request.GetString("last_name", ""); v != "" {
			body["last_name"] = v
		}
		if _, ok := args["is_active"]; ok {
			body["is_active"] = request.GetBool("is_active", true)
		}
		if _, ok := args["is_staff"]; ok {
			body["is_staff"] = request.GetBool("is_staff", false)
		}
		if _, ok := args["is_superuser"]; ok {
			body["is_superuser"] = request.GetBool("is_superuser", false)
		}
		if err := setJSONField(body, request, "groups"); err != nil {
			return errResult(err.Error()), nil
		}

		path := "/api/users/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleUserUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		args := request.GetArguments()

		if v := request.GetString("username", ""); v != "" {
			body["username"] = v
		}
		if v := request.GetString("password", ""); v != "" {
			body["password"] = v
		}
		if v := request.GetString("email", ""); v != "" {
			body["email"] = v
		}
		if v := request.GetString("first_name", ""); v != "" {
			body["first_name"] = v
		}
		if v := request.GetString("last_name", ""); v != "" {
			body["last_name"] = v
		}
		if _, ok := args["is_active"]; ok {
			body["is_active"] = request.GetBool("is_active", true)
		}
		if _, ok := args["is_staff"]; ok {
			body["is_staff"] = request.GetBool("is_staff", false)
		}
		if _, ok := args["is_superuser"]; ok {
			body["is_superuser"] = request.GetBool("is_superuser", false)
		}
		if err := setJSONField(body, request, "groups"); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/users/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleUserDeactivateTotp(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/users/%d/deactivate_totp/", id)
		resp, err := client.Post(ctx, path, nil)
		return doRequest(resp, err, "POST", path)
	}
}

func handleGroupCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"name": name}
		if err := setJSONField(body, request, "permissions"); err != nil {
			return errResult(err.Error()), nil
		}

		path := "/api/groups/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleGroupUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		if v := request.GetString("name", ""); v != "" {
			body["name"] = v
		}
		if err := setJSONField(body, request, "permissions"); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/groups/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleProfileUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}
		if v := request.GetString("email", ""); v != "" {
			body["email"] = v
		}
		if v := request.GetString("first_name", ""); v != "" {
			body["first_name"] = v
		}
		if v := request.GetString("last_name", ""); v != "" {
			body["last_name"] = v
		}
		if v := request.GetString("password", ""); v != "" {
			body["password"] = v
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := "/api/profile/"
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}
