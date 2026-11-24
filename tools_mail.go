package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// mailRuleFieldsNote lists the mail rule fields (paperless_mail/models.py MailRule).
const mailRuleFieldsNote = "Fields: folder (default INBOX), filter_from, filter_to, filter_subject, filter_body, filter_attachment_filename_include, filter_attachment_filename_exclude, maximum_age (days, default 30), action (1 delete, 2 move, 3 mark read, 4 flag, 5 tag; move and tag need action_parameter: for move the IMAP folder, for tag an IMAP keyword or Gmail label set on the mail (or apple:<color>), not a paperless tag), assign_title_from (1 subject, 2 filename, 3 none), assign_correspondent_from (1 none, 2 mail address, 3 name, 4 assign_correspondent), assign_correspondent, assign_tags, assign_document_type, assign_owner_from_rule, attachment_type (1 attachments only, 2 all files including inline), consumption_scope (1 attachments, 2 the mail as .eml, 3 both), pdf_layout (0 default, 1 text then HTML, 2 HTML then text, 3 HTML only, 4 text only), order, enabled, stop_processing."

func registerMailTools(srv *server.MCPServer, client *Client) {
	// Mail Accounts
	srv.AddTool(mcp.NewTool("mail_account_list",
		mcp.WithDescription("List mail accounts."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/mail_accounts/"))

	srv.AddTool(mcp.NewTool("mail_account_get",
		mcp.WithDescription("Get mail account details."),
		withNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleGetByID(client, "/api/mail_accounts/%d/"))

	srv.AddTool(mcp.NewTool("mail_account_create",
		mcp.WithDescription("Create a mail account."),
		mcp.WithString("name", mcp.Description("Account name"), mcp.Required()),
		mcp.WithString("imap_server", mcp.Description("IMAP server hostname"), mcp.Required()),
		withNumber("imap_port", mcp.Description("IMAP port"), mcp.Required()),
		withIMAPSecurity("imap_security"),
		mcp.WithString("username", mcp.Description("Username"), mcp.Required()),
		mcp.WithString("password", mcp.Description("Password"), mcp.Required()),
		mcp.WithString("character_set", mcp.Description("Character set (default: UTF-8)")),
	), handleMailAccountCreate(client))

	srv.AddTool(mcp.NewTool("mail_account_update",
		mcp.WithDescription("Update a mail account; only the fields given change, and "+nullsIgnoredHint+"."),
		withNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
		mcp.WithString("name", mcp.Description("Account name")),
		mcp.WithString("imap_server", mcp.Description("IMAP server hostname")),
		withNumber("imap_port", mcp.Description("IMAP port")),
		withIMAPSecurity("imap_security"),
		mcp.WithString("username", mcp.Description("Username")),
		mcp.WithString("password", mcp.Description("Password")),
	), handleMailAccountUpdate(client))

	srv.AddTool(mcp.NewTool("mail_account_delete",
		mcp.WithDescription("Delete a mail account."),
		withNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/mail_accounts/%d/"))

	srv.AddTool(mcp.NewTool("mail_account_test",
		mcp.WithDescription("Test mail account connectivity."),
		withNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleMailAccountTest(client))

	srv.AddTool(mcp.NewTool("mail_account_process",
		mcp.WithDescription("Queue a check of a mail account for new mail; returns {result: \"OK\"} at once, before any mail is read. See the outcome with processed_mail_list or task_list."),
		withNumber("id", mcp.Description("Mail account ID"), mcp.Required()),
	), handleMailAccountProcess(client))

	// Mail Rules
	srv.AddTool(mcp.NewTool("mail_rule_list",
		mcp.WithDescription("List mail rules."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/mail_rules/"))

	srv.AddTool(mcp.NewTool("mail_rule_get",
		mcp.WithDescription("Get mail rule details."),
		withNumber("id", mcp.Description("Mail rule ID"), mcp.Required()),
	), handleGetByID(client, "/api/mail_rules/%d/"))

	srv.AddTool(mcp.NewTool("mail_rule_create",
		mcp.WithDescription("Create a mail rule."),
		mcp.WithString("name", mcp.Description("Rule name"), mcp.Required()),
		withNumber("account", mcp.Description("Mail account ID"), mcp.Required()),
		withJSONObject("body", mcp.Description("Object with rule configuration. "+mailRuleFieldsNote), mcp.Required()),
	), handleMailRuleCreate(client))

	srv.AddTool(mcp.NewTool("mail_rule_update",
		mcp.WithDescription("Update a mail rule."),
		withNumber("id", mcp.Description("Mail rule ID"), mcp.Required()),
		withJSONObject("body", mcp.Description("Object with fields to update (fields and codes as in mail_rule_create's body)"), mcp.Required()),
	), handleMailRuleUpdate(client))

	srv.AddTool(mcp.NewTool("mail_rule_delete",
		mcp.WithDescription("Delete a mail rule."),
		withNumber("id", mcp.Description("Mail rule ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/mail_rules/%d/"))

	// Processed Mail
	srv.AddTool(mcp.NewTool("processed_mail_list",
		mcp.WithDescription("List processed mail records."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/processed_mail/"))

	srv.AddTool(mcp.NewTool("processed_mail_get",
		mcp.WithDescription("Get processed mail details."),
		withNumber("id", mcp.Description("Processed mail ID"), mcp.Required()),
	), handleGetByID(client, "/api/processed_mail/%d/"))

	srv.AddTool(mcp.NewTool("processed_mail_bulk_delete",
		mcp.WithDescription("Bulk delete processed mail records."),
		withJSONArray("ids", mcp.Description("Array of processed mail IDs to delete"), mcp.Required()),
	), handleProcessedMailBulkDelete(client))
}

func withIMAPSecurity(name string) mcp.ToolOption {
	return mcp.WithString(name,
		mcp.Description("Security: none (no encryption), ssl (the default on create), starttls. Sent to paperless-ngx as the integers 1, 2 and 3."),
		mcp.Enum("none", "ssl", "starttls"),
	)
}

func getIMAPSecurity(request mcp.CallToolRequest) (int, bool, *mcp.CallToolResult) {
	raw := request.GetArguments()["imap_security"]
	if !argIsSet(raw) {
		return 0, false, nil
	}
	if value, ok := imapSecurityValue(raw); ok {
		return value, true, nil
	}
	return 0, false, errResult("imap_security must be one of: none, ssl, starttls (or an integer 1-3)")
}

func imapSecurityValue(raw any) (int, bool) {
	switch v := raw.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "none":
			return 1, true
		case "ssl":
			return 2, true
		case "starttls":
			return 3, true
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil && n >= 1 && n <= 3
	case int:
		return v, v >= 1 && v <= 3
	case int64:
		return int(v), v >= 1 && v <= 3
	case float64:
		return int(v), v >= 1 && v <= 3 && v == float64(int(v))
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil && n >= 1 && n <= 3
	default:
		return 0, false
	}
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
		imapPort, errRes := getRequiredInt(request, "imap_port")
		if errRes != nil {
			return errRes, nil
		}
		imapSecurity, hasIMAPSecurity, errRes := getIMAPSecurity(request)
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{
			"name":        name,
			"imap_server": imapServer,
			"imap_port":   imapPort,
			"username":    username,
			"password":    password,
		}

		if hasIMAPSecurity {
			body["imap_security"] = imapSecurity
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
		imapSecurity, hasIMAPSecurity, errRes := getIMAPSecurity(request)
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
		if errRes := setOptionalInt(body, args, "imap_port"); errRes != nil {
			return errRes, nil
		}
		if hasIMAPSecurity {
			body["imap_security"] = imapSecurity
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

		// The test endpoint is not a detail route: it expects the account's
		// connection settings in the body. Passing the account id with an
		// obfuscated password makes the server use the stored credentials.
		getPath := fmt.Sprintf("/api/mail_accounts/%d/", id)
		resp, err := client.Get(ctx, getPath, nil)
		if err != nil {
			return errResult(fmt.Sprintf("request failed: %s", err)), nil
		}
		defer resp.Body.Close()
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return errResult(fmt.Sprintf("read response: %s", readErr)), nil
		}
		if resp.StatusCode >= 400 {
			return apiErrorResult(resp.StatusCode, data, "GET", getPath), nil
		}

		var account map[string]any
		if err := json.Unmarshal(data, &account); err != nil {
			return errResult(fmt.Sprintf("parse mail account: %s", err)), nil
		}

		body := map[string]any{"id": id, "password": "**********"}
		for _, field := range []string{"name", "imap_server", "imap_port", "imap_security", "username", "password", "character_set", "is_token", "account_type"} {
			if v, ok := account[field]; ok && v != nil {
				body[field] = v
			}
		}

		path := "/api/mail_accounts/test/"
		testResp, err := client.Post(ctx, path, body)
		return doRequest(testResp, err, "POST", path)
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
		} else if _, ok := body["body"]; ok {
			return errResult("body must be a JSON object"), nil
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
		} else if _, ok := body["body"]; ok {
			return errResult("body must be a JSON object"), nil
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
		ids, ok := body["ids"]
		if !ok {
			return errResult("ids is required"), nil
		}
		// The API reads "mail_ids" and silently deletes nothing for other keys.
		delete(body, "ids")
		body["mail_ids"] = ids

		path := "/api/processed_mail/bulk_delete/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}
