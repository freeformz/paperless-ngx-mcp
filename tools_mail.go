package main

import (
	"context"
	"fmt"
	"maps"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerMailTools(srv *server.MCPServer, client *Client) {
	// Mail Accounts
	srv.AddTool(mcp.NewTool("mail_account_list",
		mcp.WithDescription("List mail accounts."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/mail_accounts/"))

	srv.AddTool(mcp.NewTool("mail_account_get",
		mcp.WithDescription("Get mail account details."),
		mcp.WithNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleGetByID(client, "/api/mail_accounts/%d/"))

	srv.AddTool(mcp.NewTool("mail_account_create",
		mcp.WithDescription("Create a mail account."),
		mcp.WithString("name", mcp.Description("Account name"), mcp.Required()),
		mcp.WithString("imap_server", mcp.Description("IMAP server hostname"), mcp.Required()),
		mcp.WithNumber("imap_port", mcp.Description("IMAP port")),
		mcp.WithString("imap_security", mcp.Description("Security: none, ssl, starttls")),
		mcp.WithString("username", mcp.Description("Username"), mcp.Required()),
		mcp.WithString("password", mcp.Description("Password"), mcp.Required()),
		mcp.WithString("character_set", mcp.Description("Character set (default: UTF-8)")),
	), handleMailAccountCreate(client))

	srv.AddTool(mcp.NewTool("mail_account_update",
		mcp.WithDescription("Update a mail account."),
		mcp.WithNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
		mcp.WithString("name", mcp.Description("Account name")),
		mcp.WithString("imap_server", mcp.Description("IMAP server hostname")),
		mcp.WithNumber("imap_port", mcp.Description("IMAP port")),
		mcp.WithString("imap_security", mcp.Description("Security: none, ssl, starttls")),
		mcp.WithString("username", mcp.Description("Username")),
		mcp.WithString("password", mcp.Description("Password")),
	), handleMailAccountUpdate(client))

	srv.AddTool(mcp.NewTool("mail_account_delete",
		mcp.WithDescription("Delete a mail account."),
		mcp.WithNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/mail_accounts/%d/"))

	srv.AddTool(mcp.NewTool("mail_account_test",
		mcp.WithDescription("Test mail account connectivity."),
		mcp.WithNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleMailAccountTest(client))

	srv.AddTool(mcp.NewTool("mail_account_process",
		mcp.WithDescription("Manually process a mail account to check for new mail."),
		mcp.WithNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleMailAccountProcess(client))

	// Mail Rules
	srv.AddTool(mcp.NewTool("mail_rule_list",
		mcp.WithDescription("List mail rules."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/mail_rules/"))

	srv.AddTool(mcp.NewTool("mail_rule_get",
		mcp.WithDescription("Get mail rule details."),
		mcp.WithNumber("id", mcp.Description("Mail rule ID"), mcp.Required()),
	), handleGetByID(client, "/api/mail_rules/%d/"))

	srv.AddTool(mcp.NewTool("mail_rule_create",
		mcp.WithDescription("Create a mail rule."),
		mcp.WithString("name", mcp.Description("Rule name"), mcp.Required()),
		mcp.WithNumber("account", mcp.Description("Mail account ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with rule configuration"), mcp.Required()),
	), handleMailRuleCreate(client))

	srv.AddTool(mcp.NewTool("mail_rule_update",
		mcp.WithDescription("Update a mail rule."),
		mcp.WithNumber("id", mcp.Description("Mail rule ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleMailRuleUpdate(client))

	srv.AddTool(mcp.NewTool("mail_rule_delete",
		mcp.WithDescription("Delete a mail rule."),
		mcp.WithNumber("id", mcp.Description("Mail rule ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/mail_rules/%d/"))

	// Processed Mail
	srv.AddTool(mcp.NewTool("processed_mail_list",
		mcp.WithDescription("List processed mail records."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/processed_mail/"))

	srv.AddTool(mcp.NewTool("processed_mail_get",
		mcp.WithDescription("Get processed mail details."),
		mcp.WithNumber("id", mcp.Description("Processed mail ID"), mcp.Required()),
	), handleGetByID(client, "/api/processed_mail/%d/"))

	srv.AddTool(mcp.NewTool("processed_mail_bulk_delete",
		mcp.WithDescription("Bulk delete processed mail records."),
		mcp.WithString("ids", mcp.Description("JSON array of processed mail IDs to delete"), mcp.Required()),
	), handleProcessedMailBulkDelete(client))
}

func handleMailAccountCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}
		imapServer, errRes := getRequiredString(request, "imap_server")
		if errRes != nil {
			return errRes, nil
		}
		username, errRes := getRequiredString(request, "username")
		if errRes != nil {
			return errRes, nil
		}
		password, errRes := getRequiredString(request, "password")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{
			"name":        name,
			"imap_server": imapServer,
			"username":    username,
			"password":    password,
		}
		args := request.GetArguments()

		if _, ok := args["imap_port"]; ok {
			body["imap_port"] = int(request.GetFloat("imap_port", 993))
		}
		if v := request.GetString("imap_security", ""); v != "" {
			body["imap_security"] = v
		}
		if v := request.GetString("character_set", ""); v != "" {
			body["character_set"] = v
		}

		path := "/api/mail_accounts/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleMailAccountUpdate(client *Client) server.ToolHandlerFunc {
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
		if v := request.GetString("imap_server", ""); v != "" {
			body["imap_server"] = v
		}
		if _, ok := args["imap_port"]; ok {
			body["imap_port"] = int(request.GetFloat("imap_port", 993))
		}
		if v := request.GetString("imap_security", ""); v != "" {
			body["imap_security"] = v
		}
		if v := request.GetString("username", ""); v != "" {
			body["username"] = v
		}
		if v := request.GetString("password", ""); v != "" {
			body["password"] = v
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/mail_accounts/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleMailAccountTest(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/mail_accounts/%d/test/", id)
		resp, err := client.Post(ctx, path, nil)
		return doRequest(resp, err, "POST", path)
	}
}

func handleMailAccountProcess(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/mail_accounts/%d/process/", id)
		resp, err := client.Post(ctx, path, nil)
		return doRequest(resp, err, "POST", path)
	}
}

func handleMailRuleCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}
		accountID, errRes := getRequiredInt(request, "account")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"name": name, "account": accountID}
		if err := setJSONField(body, request, "body"); err != nil {
			return errResult(err.Error()), nil
		}
		// Merge body fields from the JSON body parameter into the top-level body
		if extra, ok := body["body"].(map[string]any); ok {
			delete(body, "body")
			maps.Copy(body, extra)
		}

		path := "/api/mail_rules/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleMailRuleUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		if err := setJSONField(body, request, "body"); err != nil {
			return errResult(err.Error()), nil
		}
		// Merge body fields from the JSON body parameter into the top-level body
		if extra, ok := body["body"].(map[string]any); ok {
			delete(body, "body")
			maps.Copy(body, extra)
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/mail_rules/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleProcessedMailBulkDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}
		if err := setJSONField(body, request, "ids"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["ids"]; !ok {
			return errResult("ids is required"), nil
		}

		path := "/api/processed_mail/bulk_delete/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}
