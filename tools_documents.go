package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerDocumentTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_list", append(append([]mcp.ToolOption{
			mcp.WithDescription("List/search documents with filtering and full-text search. Search with text (title and content), title_search (titles) or query (advanced field syntax); use only one of them, or query together with text or title_search as an extra filter; more_like_id combines with none. Search results carry __search_hit__ (score, highlights, note_highlights, rank). Returns paginated results; count is the number of matching documents (pass page_size: 1 to preview a selection's size before an all: true bulk call with the same filters). Document content is truncated to a snippet; use document_get or full_content for the complete text. null values are ignored."),
		}, documentListFilterOptions()...),
			mcp.WithString("ordering", mcp.Description("Sort field, prefix - for descending. With a search (text, title_search, query): -score best match first (the default), score worst first, or created, added, modified, archive_serial_number, page_count, num_notes; other fields replace relevance with database order. Not allowed with more_like_id")),
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
			mcp.WithDescription("Get full document details including tags, correspondent, document type, custom fields, notes, and permissions. duplicate_documents lists other documents you can see with the same file (it is always [] in document_list results). versions lists the document's file versions, newest first; content and page_count are the latest version's unless version is given (the other file fields, such as mime_type and original_file_name, always describe the root). A version id as id is a 404: find its root with document_root."),
			withNumber("id", mcp.Description("Document ID (a root document)"), mcp.Required()),
			withVersion("Version id whose content and page_count to return (from document_version_list); default the latest version. A trashed version can be read this way too"),
		),
		handleGetByIDWithVersion(client, "/api/documents/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("document_update",
			mcp.WithDescription("Update document metadata. Only specified fields are changed. "+clearHint+": clear: [\"correspondent\"] removes the correspondent. Metadata belongs to the root document; content belongs to a file version, and edits go to the latest version unless version is given."),
			withNumber("id", mcp.Description("Document ID (a root document)"), mcp.Required()),
			withVersion("Version id whose content to change (from document_version_list); needs content or clear: [\"content\"]. Other fields given alongside still change the root document. A trashed version's content can be edited this way too"),
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
			mcp.WithDescription("Delete a document (soft-delete to trash), with all of its versions. A version id is a 404: delete one version with document_version_delete."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/documents/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("document_upload",
			append([]mcp.ToolOption{mcp.WithDescription("Upload a new document. Returns the consume task's UUID at once; pass wait: true to wait for consumption and get its outcome (result_data.document_id on success), or follow the id with task_wait or task_get. This task is the whole consume, so finished means the document exists. A file that duplicates an existing document is consumed as a new document, unless the server sets PAPERLESS_CONSUMER_DELETE_DUPLICATES; then the task fails with result_data.duplicate_of and duplicate_in_trash (documents in the trash count). Find duplicates with document_list has_duplicates: true."),
				mcp.WithString("file_path", mcp.Description("Local path to the file to upload"), mcp.Required()),
				mcp.WithString("title", mcp.Description("Document title")),
				mcp.WithString("created", mcp.Description("Created date")),
				withNumber("correspondent", mcp.Description("Correspondent ID")),
				withNumber("document_type", mcp.Description("Document type ID")),
				withNumber("storage_path", mcp.Description("Storage path ID")),
				withJSONArray("tags", mcp.Description("Array of tag IDs")),
				withNumber("archive_serial_number", mcp.Description("Archive serial number")),
				withCustomFieldIDs("custom_fields", mcp.Description("Custom fields to set: an array of custom field ids (each added with no value), or an object mapping field id to value, e.g. {\"3\": \"INV-1\", \"7\": 12.5}; a select field takes the option id, not its label. The server checks URL, integer, monetary, string, select, document link and date values and refuses a bad one (HTTP 400, before any task); boolean, float and long text values aren't checked there.")),
			}, waitToolOptions()...)...,
		),
		handleDocumentUpload(client),
	)

	srv.AddTool(
		mcp.NewTool("document_metadata",
			mcp.WithDescription("Get file metadata (checksums, sizes, MIME type) for a document's latest version, or for version. "+versionIDNote+" barcodes lists the barcodes found (page, value, format); it is empty unless the server stores barcode values (config_get barcode_store_values, or when that is null the PAPERLESS_CONSUMER_STORE_BARCODE_VALUES setting, off by default), and only for documents consumed or reprocessed since."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
			withVersion("Version id to describe (from document_version_list); default the latest version"),
		),
		handleGetByIDWithVersion(client, "/api/documents/%d/metadata/"),
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
			mcp.WithDescription("List share links for a specific document. Links are per file: a link made with a root id serves the root's own (original) file, one made with a version id serves that version. A root id lists only links made on the root; pass a version id (from document_version_list) to see that version's links, including links a merged source had."),
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
			mcp.WithDescription("Email one or more documents. A root document id sends the root's own file, which is the original (oldest) version; to send the latest, pass that version's id (from document_version_list)."),
			withJSONArray("documents", mcp.Description("Array of document IDs"), mcp.Required()),
			mcp.WithString("subject", mcp.Description("Email subject"), mcp.Required()),
			mcp.WithString("body", mcp.Description("Email message body"), mcp.Required()),
			mcp.WithString("to", mcp.Description("Recipient email address(es), comma-separated"), mcp.Required()),
			mcp.WithBoolean("use_archive_version", mcp.Description("Send the archived (OCR'd PDF) file where one exists (default: true); false sends the uploaded file instead of the archived PDF")),
		),
		handleDocumentEmail(client),
	)

	// Document Notes
	srv.AddTool(
		mcp.NewTool("document_note_list",
			mcp.WithDescription("List notes on a document. "+notesRootNote),
			withNumber("id", mcp.Description("Document ID, or a version id (its root's notes are listed)"), mcp.Required()),
		),
		handleDocumentNoteList(client),
	)

	srv.AddTool(
		mcp.NewTool("document_note_add",
			mcp.WithDescription("Add a note to a document. "+notesRootNote),
			withNumber("id", mcp.Description("Document ID, or a version id (the note goes on its root)"), mcp.Required()),
			mcp.WithString("note", mcp.Description("Note content"), mcp.Required()),
		),
		handleDocumentNoteAdd(client),
	)

	srv.AddTool(
		mcp.NewTool("document_note_delete",
			mcp.WithDescription("Delete a note from a document. "+notesRootNote+" A note stored on the version itself is found too."),
			withNumber("id", mcp.Description("Document ID, or a version id"), mcp.Required()),
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
		if errRes := addDocumentOrdering(params, request); errRes != nil {
			return errRes, nil
		}

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

// addDocumentOrdering adds document_list's ordering, refusing what
// paperless-ngx would silently ignore: score without a search or beside
// other fields, and any ordering with more_like_id (similarity order).
func addDocumentOrdering(params url.Values, request mcp.CallToolRequest) *mcp.CallToolResult {
	ordering := strings.TrimSpace(request.GetString("ordering", ""))
	if ordering == "" || ordering == "null" {
		return nil
	}
	if params.Has("more_like_id") {
		return errResult("ordering is ignored with more_like_id (results come most similar first); leave it out")
	}
	fields := strings.Split(ordering, ",")
	for _, f := range fields {
		if strings.TrimLeft(strings.TrimSpace(f), "-") != "score" {
			continue
		}
		if !params.Has("text") && !params.Has("title_search") && !params.Has("query") {
			return errResult("ordering by score needs a search: text, title_search or query")
		}
		if len(fields) > 1 {
			return errResult("score can't be combined with other ordering fields (paperless-ngx would drop it); use -score or score alone")
		}
	}
	params.Set("ordering", ordering)
	return nil
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
		if v := request.GetString("content", ""); strings.TrimSpace(v) != "" {
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

		version, errRes := optionalVersion(request)
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/documents/%d/", id)
		target := path
		if version != 0 {
			if _, ok := body["content"]; !ok {
				return errResult("version applies only to content (give content, or clear: [\"content\"]); other fields belong to the root document"), nil
			}
			target += "?" + versionParams(version).Encode()
		}
		resp, err := client.Patch(ctx, target, body)
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

		filePath, errRes = validateUploadPath(filePath)
		if errRes != nil {
			return errRes, nil
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
		// custom_fields is a JSON field upstream: one form field holding JSON.
		cf, errRes := uploadCustomFields(request)
		if errRes != nil {
			return errRes, nil
		}
		if cf != "" {
			fields.Set("custom_fields", cf)
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

		if errRes := setOptionalBool(body, request, "use_archive_version"); errRes != nil {
			return errRes, nil
		}

		path := "/api/documents/email/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

const customFieldsShape = "custom_fields must be a custom field id, an array of ids, or an object mapping field id to value"

// uploadCustomFields reads document_upload's custom_fields and returns it
// as JSON text for the form ("" when not given). paperless-ngx accepts a
// field id, a list of ids (each added with no value) or an object mapping
// id to value; ids are normalised and values are left to the server.
func uploadCustomFields(request mcp.CallToolRequest) (string, *mcp.CallToolResult) {
	if !argIsSet(request.GetArguments()["custom_fields"]) {
		return "", nil
	}
	v, present, err := getJSONArg(request, "custom_fields")
	if err != nil {
		return "", errResult(err.Error())
	}
	if !present {
		return "", nil
	}
	fieldID := func(x any) (int, bool) {
		n, ok := wholeNumberUpTo(x, maxWholeNumber)
		return n, ok && n >= 1
	}
	var out any
	switch x := v.(type) {
	case []any:
		ids := make([]int, 0, len(x))
		for _, e := range x {
			if _, isBool := e.(bool); isBool {
				return "", errResult(customFieldsShape)
			}
			if s, isString := e.(string); isString {
				e = strings.TrimSpace(s)
			}
			n, ok := fieldID(e)
			if !ok {
				return "", errResult(fmt.Sprintf("%s; %v is not a custom field id", customFieldsShape, e))
			}
			if !slices.Contains(ids, n) {
				ids = append(ids, n)
			}
		}
		if len(ids) == 0 {
			return "", nil
		}
		out = ids
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			n, ok := fieldID(strings.TrimSpace(k))
			if !ok {
				return "", errResult(fmt.Sprintf("%s; key %q is not a custom field id", customFieldsShape, k))
			}
			key := strconv.Itoa(n)
			if _, dup := m[key]; dup {
				return "", errResult(fmt.Sprintf("custom_fields names field %d twice", n))
			}
			m[key] = val
		}
		if len(m) == 0 {
			return "", nil
		}
		out = m
	case float64, int, int64:
		n, ok := fieldID(x)
		if !ok {
			return "", errResult(customFieldsShape)
		}
		out = []int{n}
	default:
		return "", errResult(customFieldsShape)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", errResult(fmt.Sprintf("invalid custom_fields: %s", err))
	}
	return string(b), nil
}

// validateUploadPath resolves an upload's file_path to an absolute path of a
// regular file.
func validateUploadPath(filePath string) (string, *mcp.CallToolResult) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", errResult(fmt.Sprintf("invalid file path: %s", err))
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return "", errResult(fmt.Sprintf("file not accessible: %s", err))
	}
	if !info.Mode().IsRegular() {
		return "", errResult("file_path must be a regular file")
	}
	return absPath, nil
}

// notesRootNote explains where notes live, shared by the note tools.
const notesRootNote = "Notes belong to the root document (that's where the web UI, search and document_get see them): a version id is mapped to its root, and the response then says so in a second text block. A 404 can mean the document is in the trash."
