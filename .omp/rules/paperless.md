---
description: How to use the Paperless-ngx MCP tools (search, update, bulk, pagination, notes)
alwaysApply: true
---

## Paperless-ngx MCP Tools

You have access to a Paperless-ngx MCP server for managing documents and metadata.

### Getting Started

- Use `tag_list`, `correspondent_list`, `document_type_list`, and `custom_field_list` to understand the available metadata before searching or updating documents
- Use `document_list` with filters to find documents matching specific criteria
- Use `document_get` to get full details of a specific document

### Searching Documents

- **Search**: Use `document_list` with `text` (title and content) or `title_search` (titles) for normal search, and `query` for the advanced syntax (field prefixes, `NOT`, date ranges; see the server instructions). Only `query` may be combined with `text` or `title_search`
- **Tag filtering**: Use `tags_id_all` (must have ALL), `tags_id_none` (must have NONE), or `tags_id_in` (must have ANY) with comma-separated tag IDs
- **Date filtering**: Use `created_after`/`created_before` and `added_after`/`added_before` for date range queries
- **Custom field queries**: Use `custom_field_query` with a JSON filter expression

### Updating Documents

1. Get document details with `document_get` to see current metadata
2. Update only the fields you want to change with `document_update`
3. To clear a field (correspondent, document_type, storage_path, archive_serial_number, owner), name it in `clear` (e.g. `clear: ["correspondent"]`); a null value is ignored in every tool
4. For tags, pass a JSON array of tag IDs — this replaces all tags on the document
5. For custom fields, pass a JSON array of custom field assignments

### Bulk Operations

- Use `document_bulk_edit` for batch metadata changes across multiple documents
- Available methods: set_correspondent, set_document_type, set_storage_path, add_tag, remove_tag, modify_tags, modify_custom_fields, set_permissions (run workflows with workflow_run)
- File operations have their own tools: document_rotate, document_merge, document_edit_pdf (split, drop, reorder or rotate pages), document_remove_password, document_reprocess, document_bulk_delete
- Use `document_selection_data` to preview aggregated metadata counts before making bulk changes
- `document_bulk_edit`, `document_selection_data`, `document_rotate`, `document_reprocess` and `document_bulk_delete` take `documents` or `all: true` with `filters` (document_list's filter names) and `excluded_documents`; preview the match with `document_list` and the same filters (`page_size: 1` gives `count`). Without filters, `all: true` means every document

### Pagination

All list endpoints support `page` (default: 1) and `page_size` (default: 25) parameters. Responses include `count`, `next`, `previous`, and `results` fields. To retrieve all results, iterate through pages.

### Notes

- Use `document_note_add` to add notes to documents for tracking decisions or observations
- Notes are timestamped and include the user who created them
- Use `document_note_list` to see existing notes before adding duplicates
- Notes belong to the root document; a version id passed to the note tools is mapped to its root

### Versions

- A document's root holds the metadata; each version has its own file and content. `document_version_list` lists them, `document_root` finds a version's root, and `document_get`, `document_download`, `document_metadata` and `document_page_image` take `version`
- `document_merge_as_versions` can't be undone

### API Version

All requests use Paperless-ngx REST API version 10, which requires Paperless-ngx 3.5.0 or newer. This is handled automatically by the MCP server.
