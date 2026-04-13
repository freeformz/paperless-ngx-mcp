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
	registerTagTools(srv, client)
	registerCorrespondentTools(srv, client)
	registerDocumentTypeTools(srv, client)
	registerStoragePathTools(srv, client)
	registerCustomFieldTools(srv, client)
	registerSearchTools(srv, client)
	registerBulkTools(srv, client)
	registerSavedViewTools(srv, client)
	registerShareLinkTools(srv, client)
	registerUserTools(srv, client)
	registerMailTools(srv, client)
	registerWorkflowTools(srv, client)
	registerSystemTools(srv, client)

	return srv
}

const serverInstructions = `Paperless-ngx MCP server — manage documents and metadata via AI agents.

## Tool Categories
- **Documents**: document_list (search/filter), document_get, document_update, document_delete, document_upload, document_download, document_metadata, document_suggestions, document_next_asn, document_share_links, document_history, document_email
- **Document Notes**: document_note_list, document_note_add, document_note_delete
- **Downloads**: document_download (fetch files to local temp), cleanup_downloads (remove downloaded files)
- **Bulk Operations**: document_bulk_edit, document_selection_data, bulk_edit_objects
- **Tags**: tag_list, tag_get, tag_create, tag_update, tag_delete
- **Correspondents**: correspondent_list, correspondent_get, correspondent_create, correspondent_update, correspondent_delete
- **Document Types**: document_type_list, document_type_get, document_type_create, document_type_update, document_type_delete
- **Storage Paths**: storage_path_list, storage_path_get, storage_path_create, storage_path_update, storage_path_delete, storage_path_test
- **Custom Fields**: custom_field_list, custom_field_get, custom_field_create, custom_field_update, custom_field_delete
- **Search**: search_autocomplete, search_global
- **Statistics**: statistics
- **Saved Views**: saved_view_list, saved_view_get, saved_view_create, saved_view_update, saved_view_delete
- **Share Links**: share_link_list, share_link_get, share_link_create, share_link_update, share_link_delete
- **Users & Groups**: user_list, user_get, user_create, user_update, user_delete, group_list, group_get, group_create, group_update, group_delete, profile_get, profile_update
- **Mail**: mail_account_list, mail_account_get, mail_account_create, mail_account_update, mail_account_delete, mail_account_test, mail_account_process, mail_rule_list, mail_rule_get, mail_rule_create, mail_rule_update, mail_rule_delete, processed_mail_list, processed_mail_get, processed_mail_bulk_delete
- **Workflows**: workflow_list, workflow_get, workflow_create, workflow_update, workflow_delete, workflow_trigger_list, workflow_trigger_get, workflow_trigger_create, workflow_trigger_update, workflow_trigger_delete, workflow_action_list, workflow_action_get, workflow_action_create, workflow_action_update, workflow_action_delete
- **Tasks**: task_list, task_get, task_acknowledge, task_run
- **System**: system_status, remote_version, ui_settings_get, config_list, config_get, config_update, log_list, log_get, trash_list, trash_action

## Pagination
All list endpoints support page (default: 1) and page_size (default: 25) parameters. Responses include count, next, previous, and results fields.

## Document Search
Use document_list with query for full-text search. Results include __search_hit__ with score, highlights, and rank.

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
4. For clearing a field, pass null (e.g., correspondent: null removes the correspondent)

## Bulk Operations
Use document_bulk_edit for batch operations across multiple documents. Methods: set_correspondent, set_document_type, set_storage_path, add_tag, remove_tag, modify_tags, delete, reprocess, set_permissions, modify_custom_fields, rotate, delete_pages, split, merge, edit_pdf.

## Document Downloads
Use document_download to fetch document files to local temp storage. Specify variant: archived (default, OCR'd PDF/A), original (as uploaded), or thumbnail. Returns file paths. Use cleanup_downloads to remove files when done.

## API Version
All requests use Paperless-ngx REST API version 9.`
