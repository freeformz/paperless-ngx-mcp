package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerDocumentTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_list", append(append([]mcp.ToolOption{
			mcp.WithDescription("List/search documents with filtering and full-text search. Returns paginated results; count is the number of matching documents (pass page_size: 1 to preview a selection's size before an all: true bulk call with the same filters). Document content is truncated to a snippet; use document_get or full_content for the complete text. null values are ignored."),
		}, documentListFilterOptions()...),
			mcp.WithString("ordering", mcp.Description("Sort field (prefix - for descending)")),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25, max: 100000)")),
			mcp.WithBoolean("full_content", mcp.Description("Include full document content in results instead of a truncated snippet (default: false)")),
			withNumber("content_snippet_bytes", mcp.Description("Max bytes of document content per result (default: 500; 0 disables truncation)")),
			mcp.WithBoolean("include_notes", mcp.Description("Include full note objects in results (default: false — notes are replaced with notes_count; use document_note_list for full notes)")),
		)...),
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
			mcp.WithDescription("Update document metadata. Only specified fields are changed. "+clearHint+": clear: [\"correspondent\"] removes the correspondent."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("content", mcp.Description("New document content text (OCR text); clear: [\"content\"] empties it (an empty string is ignored)")),
			mcp.WithString("created", mcp.Description("New created date (YYYY-MM-DD)")),
			withNumber("correspondent", mcp.Description("Correspondent ID (clear: [\"correspondent\"] removes it)")),
			withNumber("document_type", mcp.Description("Document type ID (clear: [\"document_type\"] removes it)")),
			withNumber("storage_path", mcp.Description("Storage path ID (clear: [\"storage_path\"] removes it)")),
			withJSONArray("tags", mcp.Description("Array of tag IDs (replaces all tags)")),
			withNumber("archive_serial_number", mcp.Description("Archive serial number (clear: [\"archive_serial_number\"] removes it)")),
			withNumber("owner", mcp.Description("Owner user ID (clear: [\"owner\"] removes the owner)")),
			withJSONArray("custom_fields", mcp.Description("Array of custom field assignments")),
			withClear(documentUpdateClearable...),
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
			append([]mcp.ToolOption{mcp.WithDescription("Upload a new document. Returns the consume task's UUID at once; pass wait: true to wait for consumption and get its outcome (result_data.document_id on success; a duplicate is reported as status failure with result_data.duplicate_of), or follow the id with task_wait or task_get. This task is the whole consume, so finished means the document exists."),
				mcp.WithString("file_path", mcp.Description("Local path to the file to upload"), mcp.Required()),
				mcp.WithString("title", mcp.Description("Document title")),
				mcp.WithString("created", mcp.Description("Created date")),
				withNumber("correspondent", mcp.Description("Correspondent ID")),
				withNumber("document_type", mcp.Description("Document type ID")),
				withNumber("storage_path", mcp.Description("Storage path ID")),
				withJSONArray("tags", mcp.Description("Array of tag IDs")),
				withNumber("archive_serial_number", mcp.Description("Archive serial number")),
			}, waitToolOptions()...)...,
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
			withJSONArray("documents", mcp.Description("Array of document IDs"), mcp.Required()),
			mcp.WithString("subject", mcp.Description("Email subject"), mcp.Required()),
			mcp.WithString("body", mcp.Description("Email message body"), mcp.Required()),
			mcp.WithString("to", mcp.Description("Recipient email address(es), comma-separated"), mcp.Required()),
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
		if errRes := addPaginationParams(params, request); errRes != nil {
			return errRes, nil
		}
		if errRes := addDocumentListFilters(params, request); errRes != nil {
			return errRes, nil
		}
		addStringParam(params, request, "ordering", "ordering")

		snippetLen, errRes := getContentSnippetLen(request)
		if errRes != nil {
			return errRes, nil
		}
		fullContent, errRes := optionalBool(request, "full_content", false)
		if errRes != nil {
			return errRes, nil
		}
		includeNotes, errRes := optionalBool(request, "include_notes", false)
		if errRes != nil {
			return errRes, nil
		}

		path := "/api/documents/"
		resp, err := client.Get(ctx, path, params)
		return doRequestJSON(resp, err, "GET", path, func(v any) any {
			m, ok := v.(map[string]any)
			if !ok {
				return v
			}
			if !fullContent {
				truncateContentFields(m["results"], snippetLen)
			}
			if !includeNotes {
				stripNotesFields(m["results"])
			}
			return v
		})
	}
}

// documentUpdateIDFields are document_update's id fields.
var documentUpdateIDFields = []string{"correspondent", "document_type", "storage_path", "archive_serial_number", "owner"}

// documentUpdateClearable are the document fields clear can remove; content
// is cleared to "" (it can't be null), the others to null.
var documentUpdateClearable = append(slices.Clone(documentUpdateIDFields), "content")

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
		// "" is not given (some clients pad with it); clear: ["content"] empties it.
		if v := request.GetString("content", ""); v != "" {
			body["content"] = v
		}
		if v := request.GetString("created", ""); v != "" {
			body["created"] = v
		}

		for _, name := range documentUpdateIDFields {
			if errRes := setOptionalInt(body, args, name); errRes != nil {
				return errRes, nil
			}
		}
		if errRes := applyClear(body, request, documentUpdateClearable); errRes != nil {
			return errRes, nil
		}
		if c, ok := body["content"]; ok && c == nil {
			body["content"] = ""
		}

		if err := setJSONField(body, request, "tags"); err != nil {
			return errResult(err.Error()), nil
		}
		if err := setJSONField(body, request, "custom_fields"); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update; " + clearHint), nil
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
		wait, timeout, errRes := getWaitOptions(request)
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

		fields := url.Values{}
		if v := request.GetString("title", ""); v != "" {
			fields.Set("title", v)
		}
		if v := request.GetString("created", ""); v != "" {
			fields.Set("created", v)
		}

		args := request.GetArguments()
		ids := map[string]any{}
		for _, name := range []string{"correspondent", "document_type", "storage_path", "archive_serial_number"} {
			if errRes := setOptionalInt(ids, args, name); errRes != nil {
				return errRes, nil
			}
			if n, ok := ids[name].(int); ok {
				fields.Set(name, strconv.Itoa(n))
			}
		}
		tagIDs, _, err := getJSONInts(request, "tags")
		if err != nil {
			return errResult(err.Error()), nil
		}
		// The API expects repeated form fields for list values, not a JSON array.
		for _, tagID := range tagIDs {
			fields.Add("tags", strconv.Itoa(tagID))
		}

		path := "/api/documents/post_document/"
		resp, err := client.PostMultipart(ctx, path, fields, filePath)
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, nil)
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
		body["addresses"] = to

		subject, errRes := getRequiredString(request, "subject")
		if errRes != nil {
			return errRes, nil
		}
		body["subject"] = subject

		message, errRes := getRequiredString(request, "body")
		if errRes != nil {
			return errRes, nil
		}
		body["message"] = message

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
