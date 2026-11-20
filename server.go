package main

import (
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates and configures the MCP server with all tool registrations.
func NewServer(client *Client, dl *Downloader) *server.MCPServer {
	srv := server.NewMCPServer(
		"paperless-ngx-mcp",
		version,
		server.WithToolCapabilities(true),
		server.WithInstructions(serverInstructions),
	)

	registerDocumentTools(srv, client)
	registerDownloadTools(srv, client, dl)
	registerPageImageTools(srv, client)
	registerTagTools(srv, client)
	registerCorrespondentTools(srv, client)
	registerDocumentTypeTools(srv, client)
	registerStoragePathTools(srv, client)
	registerCustomFieldTools(srv, client)
	registerSearchTools(srv, client)
	registerBulkTools(srv, client)
	registerDocumentActionTools(srv, client)
	registerSavedViewTools(srv, client)
	registerShareLinkTools(srv, client)
	registerUserTools(srv, client)
	registerMailTools(srv, client)
	registerWorkflowTools(srv, client)
	registerSystemTools(srv, client)

	// Reads each tool's schema at call time, so registration order doesn't matter.
	srv.Use(dropNullArgs(srv))

	return srv
}

const serverInstructions = `Paperless-ngx MCP server — manage documents and metadata via AI agents. Requires Paperless-ngx 3.5.0 or newer (REST API v10).

## Tool Categories
- **Documents**: document_list (search/filter), document_get, document_update, document_delete, document_upload, document_download, document_metadata, document_suggestions, document_next_asn, document_share_links, document_history, document_email
- **Document Notes**: document_note_list, document_note_add, document_note_delete
- **Downloads**: document_download (fetch files, inline or disk), cleanup_downloads (remove downloaded files), document_page_image (render a page as a viewable image)
- **Bulk Operations**: document_bulk_edit (metadata), document_selection_data, bulk_edit_objects
- **Editing Documents**: document_rotate, document_merge, document_edit_pdf, document_remove_password, document_reprocess, document_bulk_delete
- **Tags**: tag_list, tag_get, tag_create, tag_update, tag_delete
- **Correspondents**: correspondent_list, correspondent_get, correspondent_create, correspondent_update, correspondent_delete
- **Document Types**: document_type_list, document_type_get, document_type_create, document_type_update, document_type_delete
- **Storage Paths**: storage_path_list, storage_path_get, storage_path_create, storage_path_update, storage_path_delete, storage_path_test
- **Custom Fields**: custom_field_list, custom_field_get, custom_field_create, custom_field_update, custom_field_delete
- **Search**: search_autocomplete, search_global
- **Statistics**: statistics
- **Saved Views**: saved_view_list, saved_view_get, saved_view_create, saved_view_update, saved_view_delete, saved_view_rule_types
- **Share Links**: share_link_list, share_link_get, share_link_create, share_link_delete (public URL: {PAPERLESS_URL}/share/{slug}; links can't be edited, so delete and recreate to change one)
- **Users & Groups**: user_list, user_get, user_create, user_update, user_delete, group_list, group_get, group_create, group_update, group_delete, profile_get, profile_update
- **Mail**: mail_account_list, mail_account_get, mail_account_create, mail_account_update, mail_account_delete, mail_account_test, mail_account_process, mail_rule_list, mail_rule_get, mail_rule_create, mail_rule_update, mail_rule_delete, processed_mail_list, processed_mail_get, processed_mail_bulk_delete
- **Workflows**: workflow_list, workflow_get, workflow_create, workflow_update, workflow_delete, workflow_trigger_list, workflow_trigger_get, workflow_trigger_create, workflow_trigger_update, workflow_trigger_delete, workflow_action_list, workflow_action_get, workflow_action_create, workflow_action_update, workflow_action_delete
- **Tasks**: task_list, task_get, task_wait, task_acknowledge, task_run, task_stats
- **System**: system_status, remote_version, ui_settings_get, config_list, config_get, config_update, log_list, log_get, trash_list, trash_action

## Pagination
All list endpoints support page (default: 1) and page_size (default: 25) parameters. Responses include count, next, previous, and results fields.

## Response Shaping
Responses are trimmed to stay compact. Document content in document_list results is truncated to a snippet (default 500 bytes, content_truncated: true marks truncated entries) — adjust with content_snippet_bytes (0 disables), pass full_content: true, or use document_get for full text. Notes in document_list results are replaced with a notes_count field (include_notes: true re-includes them; document_note_list and document_get return full notes). log_get returns only the last 100 lines by default (tail parameter). document_selection_data omits objects with zero matching documents. user_list omits inherited_permissions unless include_permissions: true. task_list trims result_data.traceback to its last 500 bytes (include_traceback: true keeps it; task_get returns it in full).

## Document Search
Use document_list with query for full-text search. Results include __search_hit__ with score, highlights, and rank. search_global is only a quick lookup across object types: at most 3 results per type, no pagination, a 3 to 4096 character query, and documents with only id, title and created.

## Tag Filtering
- tags_id_all: document must have ALL specified tags (comma-separated IDs)
- tags_id_none: document must have NONE of the specified tags
- tags_id_in: document must have ANY of the specified tags

## Custom Field Queries
Use custom_field_query parameter with a JSON filter expression on document_list.

## Metadata Workflow
1. Get document details with document_get
2. Review current metadata fields
3. Update specific fields with document_update (only changed fields need to be sent)
4. To clear a field, name it in clear (e.g., clear: ["correspondent"] removes the correspondent); a null value is ignored

## Bulk Operations
Use document_bulk_edit for metadata changes across multiple documents. Methods: set_correspondent, set_document_type, set_storage_path, add_tag, remove_tag, modify_tags, modify_custom_fields, set_permissions, run_workflow; its description lists each method's parameters. bulk_edit_objects sets permissions on, or deletes, tags, correspondents, document types and storage paths (owner, permissions, merge and clear are its own parameters).

## Selecting Documents by Filter
document_bulk_edit, document_selection_data, document_rotate, document_reprocess and document_bulk_delete take documents (explicit IDs) or all: true with filters (document_list's filter names, e.g. {"tags_id_all": [3], "created_before": "2020-01-01"}) and excluded_documents. all: true acts on every root document you can view that matches; without filters, every document. Unknown filter names and unusable values are rejected, because paperless-ngx would silently ignore them and select everything. Preview with document_list and the same filters (page_size: 1 gives count). One selected document you can't change fails the whole call with a 403. bulk_edit_objects takes all: true with name (all types), is_root (tags) or path (storage paths) filters; it acts on the objects you can change or delete, and for tags also on child tags. trash_action empty needs documents, or all: true to permanently empty the whole trash.

## Editing Documents
File operations have their own tools: document_rotate (adds a version), document_merge (a new document from several), document_edit_pdf (split, drop, reorder or rotate pages of one document: operations lists the output pages, and pages left out are dropped), document_remove_password, document_reprocess (re-run OCR) and document_bulk_delete (to the trash; trash_action restore undoes it). Each queues a task, like document_bulk_edit; use wait. edit_pdf and remove_password create new documents unless update_document is set, which adds a version instead. Every one needs change permission on each document; rotate, edit_pdf, remove_password, delete and merge with delete_originals also need ownership (or no owner); creating documents needs add permission and deleting needs delete permission. A 403 says only "Insufficient permissions".

## Document Downloads
Use document_download to fetch document files. By default, files are saved to local temp storage and file paths are returned — use cleanup_downloads to remove when done. Set content=true to return file content inline instead (no cleanup needed): image files come back as viewable MCP image content, other types as base64 JSON. Specify variant: archived (default, OCR'd PDF/A), original (as uploaded), or thumbnail. If the server is configured with PAPERLESS_MCP_DOWNLOAD_DIR, disk downloads go there (optionally into a dest_dir subdirectory) with their real filenames, and cleanup_downloads leaves them alone.

## Saved Views
A saved view is a named document filter: filter_rules is a list of {rule_type, value} combined with AND, and multi-value rule types take one rule per value. Call saved_view_rule_types for the codes, value formats and matching document_list filters instead of guessing. filter_rules is required on create ([] matches every document) and replaces all rules on update. Dashboard and sidebar visibility is a per-user web UI setting (ui_settings_get, settings.saved_views); these tools don't change it.

## Structured Parameters
Array and object parameters accept a native JSON value or the same value as a JSON-encoded string. A top-level null means "not given" in every tool (some clients send null for every unused parameter); to clear a field, name it in the tool's clear parameter (document_update, tag_update, saved_view_update, bulk_edit_objects).

## Tasks
Background work (consuming uploads, mail fetch, bulk edits, scheduled jobs) runs as tasks. status is pending, started, success, failure or revoked. task_get takes either the task UUID that document_upload, document_bulk_edit, the document editing tools and task_run return, or the integer id from task_list; task_acknowledge takes integer ids (or all: true). task_list filters by status, task_type and trigger_source (arrays: any of the values), and trims result_data.traceback to its last 500 bytes unless include_traceback is set. task_stats gives per-type summaries, status counts, or the currently active tasks.
document_upload, document_bulk_edit, the document editing tools and task_run queue a task and return its id at once, before anything has happened; errors such as missing permissions or a wrong PDF password show up only in the task. Pass wait: true (timeout in seconds, default 60, at most 600) to wait for the task and get its outcome, or call task_wait with the id later. A finished wait returns {task, waited_seconds}; a failed or revoked task is a tool error carrying the task, while a problem with waiting itself comes back as wait_error next to the queued response (don't resend it); if time runs out the task comes back with timed_out: true, and task_wait continues. The MCP host may end a long call before the timeout; task_wait picks it up again. An upload's task is the whole consume. document_bulk_edit's metadata methods and document_bulk_delete are done when their task is, while document_rotate, document_merge, document_edit_pdf and document_remove_password run consume_file tasks of their own, document_reprocess runs reprocess_document tasks, and merge with delete_originals, or edit_pdf or remove_password with delete_original (and no update_document), also run bulk_delete once those succeed; follow_on in the wait result says how to find them.

## Viewing Documents
Use document_page_image to actually see a document page — essential when OCR content is missing or garbled (handwriting, bad scans, stamps, diagrams). Render the full page first; if fine detail is illegible, call it again with region=[x0,y0,x1,y1] (fractions of page width/height, 0-1 — NOT pixels; e.g. [0,0,0.5,0.5] is the top-left quarter) to re-render that area at high resolution.

## API Version
All requests use Paperless-ngx REST API version 10 (Paperless-ngx 3.5.0 or newer).`
