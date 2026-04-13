# paperless-ngx-mcp

MCP server for [Paperless-ngx](https://docs.paperless-ngx.com/) document management. Enables AI agents (Claude Code, CoWork) to search, browse, and manage documents and metadata in a Paperless-ngx instance.

## Features

- **100+ MCP tools** covering the full Paperless-ngx REST API v9
- **Document management** — list, search, get, update, delete, upload, download, email
- **Metadata CRUD** — tags, correspondents, document types, storage paths, custom fields
- **Full-text search** with tag filtering, date ranges, and custom field queries
- **Bulk operations** — batch edit, reprocess, merge, split, rotate, delete
- **Notes** — add, list, and delete notes on documents
- **Saved views, share links, mail, workflows** — full admin toolset
- **User & group management** — CRUD, profile, TOTP deactivation
- **System tools** — status, tasks, logs, config, trash management
- **In-memory caching** of metadata lists (tags, correspondents, document types, storage paths, custom fields) with automatic invalidation

## Installation

### CoWork / Claude Desktop (MCPB bundle)

Download the `.mcpb` bundle for your platform from the [latest release](https://github.com/freeformz/paperless-ngx-mcp/releases/latest) and install it as an extension. You'll be prompted for your Paperless-ngx URL and API token.

### Claude Code (CLI)

Add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "paperless": {
      "command": "/path/to/paperless-ngx-mcp",
      "args": ["mcp"],
      "env": {
        "PAPERLESS_URL": "http://localhost:8000",
        "PAPERLESS_TOKEN": "your-api-token"
      }
    }
  }
}
```

### Docker

```bash
docker run --rm -i \
  -e PAPERLESS_URL=http://localhost:8000 \
  -e PAPERLESS_TOKEN=your-api-token \
  ghcr.io/freeformz/paperless-ngx-mcp:latest mcp
```

### From source

```bash
go install github.com/freeformz/paperless-ngx-mcp@latest
```

## Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `PAPERLESS_URL` | yes | Base URL of the Paperless-ngx instance |
| `PAPERLESS_TOKEN` | yes | API authentication token |

Generate an API token in Paperless-ngx under **Settings > Administration > Auth Tokens**.

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--download-concurrency` | 5 | Max parallel document downloads |

## Tools

### Documents
`document_list`, `document_get`, `document_update`, `document_delete`, `document_upload`, `document_download`, `document_metadata`, `document_suggestions`, `document_next_asn`, `document_share_links`, `document_history`, `document_email`

### Document Notes
`document_note_list`, `document_note_add`, `document_note_delete`

### Downloads
`document_download` (disk or base64 inline), `cleanup_downloads`

### Tags
`tag_list`, `tag_get`, `tag_create`, `tag_update`, `tag_delete`

### Correspondents
`correspondent_list`, `correspondent_get`, `correspondent_create`, `correspondent_update`, `correspondent_delete`

### Document Types
`document_type_list`, `document_type_get`, `document_type_create`, `document_type_update`, `document_type_delete`

### Storage Paths
`storage_path_list`, `storage_path_get`, `storage_path_create`, `storage_path_update`, `storage_path_delete`, `storage_path_test`

### Custom Fields
`custom_field_list`, `custom_field_get`, `custom_field_create`, `custom_field_update`, `custom_field_delete`

### Search & Statistics
`search_autocomplete`, `search_global`, `statistics`

### Bulk Operations
`document_bulk_edit`, `document_selection_data`, `bulk_edit_objects`

### Saved Views
`saved_view_list`, `saved_view_get`, `saved_view_create`, `saved_view_update`, `saved_view_delete`

### Share Links
`share_link_list`, `share_link_get`, `share_link_create`, `share_link_update`, `share_link_delete`

### Users & Groups
`user_list`, `user_get`, `user_create`, `user_update`, `user_delete`, `user_deactivate_totp`, `group_list`, `group_get`, `group_create`, `group_update`, `group_delete`, `profile_get`, `profile_update`

### Mail
`mail_account_list`, `mail_account_get`, `mail_account_create`, `mail_account_update`, `mail_account_delete`, `mail_account_test`, `mail_account_process`, `mail_rule_list`, `mail_rule_get`, `mail_rule_create`, `mail_rule_update`, `mail_rule_delete`, `processed_mail_list`, `processed_mail_get`, `processed_mail_bulk_delete`

### Workflows
`workflow_list`, `workflow_get`, `workflow_create`, `workflow_update`, `workflow_delete`, `workflow_trigger_list`, `workflow_trigger_get`, `workflow_trigger_create`, `workflow_trigger_update`, `workflow_trigger_delete`, `workflow_action_list`, `workflow_action_get`, `workflow_action_create`, `workflow_action_update`, `workflow_action_delete`

### System
`system_status`, `remote_version`, `ui_settings_get`, `config_list`, `config_get`, `config_update`, `task_list`, `task_get`, `task_acknowledge`, `task_run`, `log_list`, `log_get`, `trash_list`, `trash_action`

## Development

```bash
make build          # build binary
make test           # run tests
make test-coverage  # tests with coverage report
make lint           # vet + staticcheck + test
```

## License

MIT
