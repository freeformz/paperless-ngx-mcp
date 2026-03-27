package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerDocumentTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_list",
			mcp.WithDescription("List/search documents with filtering and full-text search. Returns paginated results."),
			mcp.WithString("query", mcp.Description("Full-text search query")),
			withNumber("more_like_id", mcp.Description("Find documents similar to this document ID")),
			withNumber("correspondent_id", mcp.Description("Filter by correspondent ID")),
			withNumber("document_type_id", mcp.Description("Filter by document type ID")),
			withNumber("storage_path_id", mcp.Description("Filter by storage path ID")),
			mcp.WithString("tags_id_all", mcp.Description("Comma-separated tag IDs — document must have ALL")),
			mcp.WithString("tags_id_none", mcp.Description("Comma-separated tag IDs — document must have NONE")),
			mcp.WithString("tags_id_in", mcp.Description("Comma-separated tag IDs — document must have ANY")),
			mcp.WithBoolean("is_tagged", mcp.Description("Filter by whether document has any tags")),
			mcp.WithBoolean("is_in_inbox", mcp.Description("Filter by inbox status")),
			mcp.WithString("title", mcp.Description("Filter by title (icontains)")),
			mcp.WithString("content", mcp.Description("Filter by content (icontains)")),
			mcp.WithString("custom_field_query", mcp.Description("JSON custom field filter expression")),
			mcp.WithString("created_after", mcp.Description("Filter by created date (gte, YYYY-MM-DD)")),
			mcp.WithString("created_before", mcp.Description("Filter by created date (lte, YYYY-MM-DD)")),
			mcp.WithString("added_after", mcp.Description("Filter by added date (gte, YYYY-MM-DD)")),
			mcp.WithString("added_before", mcp.Description("Filter by added date (lte, YYYY-MM-DD)")),
			withNumber("owner_id", mcp.Description("Filter by owner")),
			mcp.WithString("ordering", mcp.Description("Sort field (prefix - for descending)")),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25, max: 100000)")),
		),
		handleDocumentList(client),
	)

	srv.AddTool(
		mcp.NewTool("document_get",
			mcp.WithDescription("Get full document details including tags, correspondent, document type, custom fields, notes, and permissions."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("document_update",
			mcp.WithDescription("Update document metadata. Only specified fields are changed. Pass null for correspondent/document_type/storage_path to clear."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("created", mcp.Description("New created date (YYYY-MM-DD)")),
			withNumber("correspondent", mcp.Description("Correspondent ID (null to clear)")),
			withNumber("document_type", mcp.Description("Document type ID (null to clear)")),
			withNumber("storage_path", mcp.Description("Storage path ID (null to clear)")),
			mcp.WithString("tags", mcp.Description("JSON array of tag IDs (replaces all tags)")),
			withNumber("archive_serial_number", mcp.Description("Archive serial number (null to clear)")),
			mcp.WithString("custom_fields", mcp.Description("JSON array of custom field assignments")),
		),
		handleDocumentUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("document_delete",
			mcp.WithDescription("Delete a document (soft-delete to trash)."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/documents/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("document_upload",
			mcp.WithDescription("Upload a new document. Returns a task UUID for tracking consumption status."),
			mcp.WithString("file_path", mcp.Description("Local path to the file to upload"), mcp.Required()),
			mcp.WithString("title", mcp.Description("Document title")),
			mcp.WithString("created", mcp.Description("Created date")),
			withNumber("correspondent", mcp.Description("Correspondent ID")),
			withNumber("document_type", mcp.Description("Document type ID")),
			withNumber("storage_path", mcp.Description("Storage path ID")),
			mcp.WithString("tags", mcp.Description("JSON array of tag IDs")),
			withNumber("archive_serial_number", mcp.Description("Archive serial number")),
		),
		handleDocumentUpload(client),
	)

	srv.AddTool(
		mcp.NewTool("document_metadata",
			mcp.WithDescription("Get file metadata (checksums, sizes, MIME type) for a document."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/metadata/"),
	)

	srv.AddTool(
		mcp.NewTool("document_suggestions",
			mcp.WithDescription("Get AI suggestions for tags, correspondent, document type, and storage path."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/suggestions/"),
	)

	srv.AddTool(
		mcp.NewTool("document_next_asn",
			mcp.WithDescription("Get the next available archive serial number."),
		),
		handleSimpleGet(client, "/api/documents/next_asn/"),
	)

	srv.AddTool(
		mcp.NewTool("document_share_links",
			mcp.WithDescription("List share links for a specific document."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/share_links/"),
	)

	srv.AddTool(
		mcp.NewTool("document_history",
			mcp.WithDescription("Get audit trail for a document."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/history/"),
	)

	srv.AddTool(
		mcp.NewTool("document_email",
			mcp.WithDescription("Email one or more documents."),
			mcp.WithString("documents", mcp.Description("JSON array of document IDs"), mcp.Required()),
			mcp.WithString("subject", mcp.Description("Email subject")),
			mcp.WithString("body", mcp.Description("Email body")),
			mcp.WithString("to", mcp.Description("Recipient email address"), mcp.Required()),
		),
		handleDocumentEmail(client),
	)

	// Document Notes
	srv.AddTool(
		mcp.NewTool("document_note_list",
			mcp.WithDescription("List notes on a document."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/notes/"),
	)

	srv.AddTool(
		mcp.NewTool("document_note_add",
			mcp.WithDescription("Add a note to a document."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
			mcp.WithString("note", mcp.Description("Note content"), mcp.Required()),
		),
		handleDocumentNoteAdd(client),
	)

	srv.AddTool(
		mcp.NewTool("document_note_delete",
			mcp.WithDescription("Delete a note from a document."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
			withNumber("note_id", mcp.Description("Note ID"), mcp.Required()),
		),
		handleDocumentNoteDelete(client),
	)
}

func handleDocumentList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "query", "query")
		addIntParam(params, request, "more_like_id", "more_like_id")
		addIntParam(params, request, "correspondent_id", "correspondent__id")
		addIntParam(params, request, "document_type_id", "document_type__id")
		addIntParam(params, request, "storage_path_id", "storage_path__id")
		addStringParam(params, request, "tags_id_all", "tags__id__all")
		addStringParam(params, request, "tags_id_none", "tags__id__none")
		addStringParam(params, request, "tags_id_in", "tags__id__in")
		addBoolParam(params, request, "is_tagged", "is_tagged")
		addBoolParam(params, request, "is_in_inbox", "is_in_inbox")
		addStringParam(params, request, "title", "title__icontains")
		addStringParam(params, request, "content", "content__icontains")
		addStringParam(params, request, "custom_field_query", "custom_field_query")
		addStringParam(params, request, "created_after", "created__date__gte")
		addStringParam(params, request, "created_before", "created__date__lte")
		addStringParam(params, request, "added_after", "added__date__gte")
		addStringParam(params, request, "added_before", "added__date__lte")
		addIntParam(params, request, "owner_id", "owner__id")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/documents/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleDocumentUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		args := request.GetArguments()
		body := map[string]any{}

		if v := request.GetString("title", ""); v != "" {
			body["title"] = v
		}
		if v := request.GetString("created", ""); v != "" {
			body["created"] = v
		}

		setNullableInt(body, args, request, "correspondent")
		setNullableInt(body, args, request, "document_type")
		setNullableInt(body, args, request, "storage_path")
		setNullableInt(body, args, request, "archive_serial_number")

		if err := setJSONField(body, request, "tags"); err != nil {
			return errResult(err.Error()), nil
		}
		if err := setJSONField(body, request, "custom_fields"); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/documents/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleDocumentUpload(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, errRes := getRequiredString(request, "file_path")
		if errRes != nil {
			return errRes, nil
		}

		// Validate file path: resolve to absolute, check it's a regular file
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("invalid file path: %s", err)), nil
		}
		filePath = absPath

		info, err := os.Lstat(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("file not accessible: %s", err)), nil
		}
		if !info.Mode().IsRegular() {
			return errResult("file_path must be a regular file"), nil
		}

		fields := map[string]string{}
		if v := request.GetString("title", ""); v != "" {
			fields["title"] = v
		}
		if v := request.GetString("created", ""); v != "" {
			fields["created"] = v
		}

		args := request.GetArguments()
		if _, ok := args["correspondent"]; ok {
			fields["correspondent"] = strconv.Itoa(int(request.GetFloat("correspondent", 0)))
		}
		if _, ok := args["document_type"]; ok {
			fields["document_type"] = strconv.Itoa(int(request.GetFloat("document_type", 0)))
		}
		if _, ok := args["storage_path"]; ok {
			fields["storage_path"] = strconv.Itoa(int(request.GetFloat("storage_path", 0)))
		}
		if v := request.GetString("tags", ""); v != "" {
			fields["tags"] = v
		}
		if _, ok := args["archive_serial_number"]; ok {
			fields["archive_serial_number"] = strconv.Itoa(int(request.GetFloat("archive_serial_number", 0)))
		}

		path := "/api/documents/post_document/"
		resp, err := client.PostMultipart(ctx, path, fields, filePath)
		return doRequest(resp, err, "POST", path)
	}
}

func handleDocumentEmail(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}

		if err := setJSONField(body, request, "documents"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["documents"]; !ok {
			return errResult("documents is required"), nil
		}

		to, errRes := getRequiredString(request, "to")
		if errRes != nil {
			return errRes, nil
		}
		body["to"] = to

		if v := request.GetString("subject", ""); v != "" {
			body["subject"] = v
		}
		if v := request.GetString("body", ""); v != "" {
			body["body"] = v
		}

		path := "/api/documents/email/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleDocumentNoteAdd(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		note, errRes := getRequiredString(request, "note")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"note": note}
		path := fmt.Sprintf("/api/documents/%d/notes/", id)
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleDocumentNoteDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		noteID, errRes := getRequiredInt(request, "note_id")
		if errRes != nil {
			return errRes, nil
		}

		path := fmt.Sprintf("/api/documents/%d/notes/", id)
		params := url.Values{"id": {strconv.Itoa(noteID)}}
		resp, err := client.Delete(ctx, path, params)
		return doRequest(resp, err, "DELETE", path)
	}
}
