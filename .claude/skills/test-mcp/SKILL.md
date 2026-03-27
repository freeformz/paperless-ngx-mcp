---
name: test-mcp
description: Build and test the paperless-ngx-mcp server against a live Paperless-ngx instance
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob
argument-hint: "<paperless-url> <api-token>"
---

# Test MCP Server Locally

Build and test the paperless-ngx-mcp server against a live Paperless-ngx instance.

**Target:** `$0` with token `$1`

## Steps

1. **Build** the binary:

```
make build
```

2. **Test MCP protocol** by sending JSON-RPC requests over stdio. Use `printf` to send multiple newline-delimited messages in a single pipe. Responses arrive out of order — match by `id`.

The tool calls below cover every read-oriented tool category:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test-mcp-skill","version":"1.0"}}}\n{"jsonrpc":"2.0","method":"notifications/initialized"}\n{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"system_status","arguments":{}}}\n{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"remote_version","arguments":{}}}\n{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"statistics","arguments":{}}}\n{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"ui_settings_get","arguments":{}}}\n{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"config_list","arguments":{}}}\n{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"log_list","arguments":{}}}\n{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"task_list","arguments":{"page_size":5}}}\n{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"document_list","arguments":{"page_size":3}}}\n{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"document_next_asn","arguments":{}}}\n{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"tag_list","arguments":{}}}\n{"jsonrpc":"2.0","id":31,"method":"tools/call","params":{"name":"correspondent_list","arguments":{}}}\n{"jsonrpc":"2.0","id":32,"method":"tools/call","params":{"name":"document_type_list","arguments":{}}}\n{"jsonrpc":"2.0","id":33,"method":"tools/call","params":{"name":"storage_path_list","arguments":{}}}\n{"jsonrpc":"2.0","id":34,"method":"tools/call","params":{"name":"custom_field_list","arguments":{}}}\n{"jsonrpc":"2.0","id":40,"method":"tools/call","params":{"name":"saved_view_list","arguments":{}}}\n{"jsonrpc":"2.0","id":41,"method":"tools/call","params":{"name":"share_link_list","arguments":{}}}\n{"jsonrpc":"2.0","id":50,"method":"tools/call","params":{"name":"user_list","arguments":{}}}\n{"jsonrpc":"2.0","id":51,"method":"tools/call","params":{"name":"group_list","arguments":{}}}\n{"jsonrpc":"2.0","id":52,"method":"tools/call","params":{"name":"profile_get","arguments":{}}}\n{"jsonrpc":"2.0","id":60,"method":"tools/call","params":{"name":"mail_account_list","arguments":{}}}\n{"jsonrpc":"2.0","id":61,"method":"tools/call","params":{"name":"mail_rule_list","arguments":{}}}\n{"jsonrpc":"2.0","id":62,"method":"tools/call","params":{"name":"processed_mail_list","arguments":{}}}\n{"jsonrpc":"2.0","id":70,"method":"tools/call","params":{"name":"workflow_list","arguments":{}}}\n{"jsonrpc":"2.0","id":71,"method":"tools/call","params":{"name":"workflow_trigger_list","arguments":{}}}\n{"jsonrpc":"2.0","id":72,"method":"tools/call","params":{"name":"workflow_action_list","arguments":{}}}\n{"jsonrpc":"2.0","id":80,"method":"tools/call","params":{"name":"trash_list","arguments":{}}}\n' | PAPERLESS_URL=$0 PAPERLESS_TOKEN=$1 ./paperless-ngx-mcp mcp 2>/dev/null
```

3. **Parse and verify** each JSON-RPC response line. Responses are one JSON object per line, matched by `id`. Use python3 to parse and validate.

For each response, check:
- `result.isError` is absent or false (tool succeeded)
- `result.content[0].text` contains valid JSON

Build a results table organized by category:

**Protocol (id 1-2):**
- `id:1` — initialize: `serverInfo.name` = `paperless-ngx-mcp`, protocol version
- `id:2` — tools/list: count of tools in `result.tools` array

**System (id 10-16):**
- `id:10` — system_status: Paperless-ngx version, install type
- `id:11` — remote_version: available update version
- `id:12` — statistics: document count, inbox stats
- `id:13` — ui_settings_get: UI theme/settings
- `id:14` — config_list: configuration entries
- `id:15` — log_list: available log files
- `id:16` — task_list: background tasks

**Documents (id 20-21):**
- `id:20` — document_list: document count, pagination info
- `id:21` — document_next_asn: next archive serial number

**Metadata (id 30-34):**
- `id:30` — tag_list: tag count
- `id:31` — correspondent_list: correspondent count
- `id:32` — document_type_list: document type count
- `id:33` — storage_path_list: storage path count
- `id:34` — custom_field_list: custom field count

**Views & Links (id 40-41):**
- `id:40` — saved_view_list: saved view count
- `id:41` — share_link_list: share link count

**Users (id 50-52):**
- `id:50` — user_list: user count
- `id:51` — group_list: group count
- `id:52` — profile_get: current user profile

**Mail (id 60-62):**
- `id:60` — mail_account_list: mail account count
- `id:61` — mail_rule_list: mail rule count
- `id:62` — processed_mail_list: processed mail count

**Workflows (id 70-72):**
- `id:70` — workflow_list: workflow count
- `id:71` — workflow_trigger_list: trigger count
- `id:72` — workflow_action_list: action count

**Trash (id 80):**
- `id:80` — trash_list: trashed document count

4. **Get-by-ID tests**: If document_list returned results, pick the first document ID and test detail endpoints:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test-mcp-skill","version":"1.0"}}}\n{"jsonrpc":"2.0","method":"notifications/initialized"}\n{"jsonrpc":"2.0","id":90,"method":"tools/call","params":{"name":"document_get","arguments":{"id":DOC_ID}}}\n{"jsonrpc":"2.0","id":91,"method":"tools/call","params":{"name":"document_metadata","arguments":{"id":DOC_ID}}}\n{"jsonrpc":"2.0","id":92,"method":"tools/call","params":{"name":"document_suggestions","arguments":{"id":DOC_ID}}}\n{"jsonrpc":"2.0","id":93,"method":"tools/call","params":{"name":"document_note_list","arguments":{"id":DOC_ID}}}\n{"jsonrpc":"2.0","id":94,"method":"tools/call","params":{"name":"document_history","arguments":{"id":DOC_ID}}}\n' | PAPERLESS_URL=$0 PAPERLESS_TOKEN=$1 ./paperless-ngx-mcp mcp 2>/dev/null
```

Replace `DOC_ID` with the actual integer ID. Verify each returns valid data without errors.

Similarly, if tag_list returned results, pick the first tag ID and test `tag_get`. Do the same for `correspondent_get`, `document_type_get`, etc. if those lists returned results.

5. **Search tests**: Test the search tools:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test-mcp-skill","version":"1.0"}}}\n{"jsonrpc":"2.0","method":"notifications/initialized"}\n{"jsonrpc":"2.0","id":100,"method":"tools/call","params":{"name":"search_autocomplete","arguments":{"term":"invoice"}}}\n' | PAPERLESS_URL=$0 PAPERLESS_TOKEN=$1 ./paperless-ngx-mcp mcp 2>/dev/null
```

6. **Report** a summary table:

| Category | Tool | Status | Details |
|----------|------|--------|---------|
| Protocol | initialize | ... | ... |
| ... | ... | ... | ... |

Include:
- Total tools registered
- Count of passed / failed tool calls
- Any errors with full error text
