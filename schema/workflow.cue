// Package workflow describes the request bodies of paperless-ngx 3.5.1's
// workflow endpoints (WorkflowSerializer, WorkflowTriggerSerializer,
// WorkflowActionSerializer and the email and webhook serializers in
// src/documents/serialisers.py). The MCP server derives the workflow tools'
// input schemas from these definitions and validates every call against them,
// because paperless-ngx silently ignores unknown fields.
//
// Only the request body is described: tool-only parameters such as id (the
// URL path) are added in Go. Keep the field lists in step with upstream;
// TestWorkflowCUEMatchesUpstream checks them against a fixture.
package workflow

import "strings"

// names maps each number-valued choice to its name. The tools accept either;
// Go turns names into numbers before validating.
names: {
	trigger_type: {consumption: 1, document_added: 2, document_updated: 3, scheduled: 4, manual: 5}
	source: {consume_folder: 1, api_upload: 2, mail_fetch: 3, web_ui: 4}
	matching_algorithm: {none: 0, any: 1, all: 2, literal: 3, regex: 4, fuzzy: 5}
	action_type: {assignment: 1, removal: 2, email: 3, webhook: 4, password_removal: 5, move_to_trash: 6, remote_ocr: 7, apply_ai_suggestions: 8}
}

// aliases are extra accepted names (upstream's labels).
aliases: {
	trigger_type: {consumption_started: 1, added: 2, updated: 3, schedule: 4}
	source: {consume: 1, consumption_folder: 1, api: 2, mail: 3, webui: 4}
	matching_algorithm: {any_word: 1, all_words: 2, exact: 3, exact_match: 3, regular_expression: 4, fuzzy_word: 5}
	action_type: {assign: 1, remove: 2, password: 5, trash: 6, ocr: 7, ai_suggestions: 8}
}

// clearable lists the single-value fields that clear may set to null.
// match can't be null upstream; clearing it sends "" instead.
clearable: {
	trigger: ["filter_path", "filter_filename", "filter_mailrule", "filter_custom_field_query", "filter_has_correspondent", "filter_has_document_type", "filter_has_storage_path", "schedule_date_custom_field", "match"]
	action: ["assign_title", "assign_correspondent", "assign_document_type", "assign_storage_path", "assign_owner", "assign_custom_fields_values"]
	webhook: ["params", "body", "headers"]
}

#ID: int & >=1
#Text256: string & strings.MaxRunes(256)

#TriggerType: or([for _, v in names.trigger_type {v}])
#Source: or([for _, v in names.source {v}])
#Matching: or([for _, v in names.matching_algorithm {v}])
#ActionType: or([for _, v in names.action_type {v}])
#ScheduleDateField: "added" | "created" | "modified" | "custom_field"
#AIField: "title" | "tags" | "correspondent" | "document_type" | "storage_path" | "created"

#NonBlank: string & =~"\\S"

#TriggerFields: {
	// Trigger type: 1 consumption (needs filter_filename, filter_path or filter_mailrule in every call), 2 document_added, 3 document_updated, 4 scheduled, 5 manual (run with workflow_run)
	type?: #TriggerType
	// Array of document sources (types 1-3): 1 consume_folder, 2 api_upload, 3 mail_fetch, 4 web_ui. [] means any; a new trigger without sources gets 1, 2 and 3
	sources?: [...#Source]
	// Path filter (wildcards *, case-insensitive)
	filter_path?: #Text256 | null
	// Filename filter (wildcards such as *.pdf, case-insensitive)
	filter_filename?: #Text256 | null
	// Mail rule ID (documents fetched by that rule)
	filter_mailrule?: #ID | null
	// Content match pattern, used with matching_algorithm
	match?: #Text256
	// Matching algorithm: 0 none, 1 any, 2 all, 3 literal, 4 regex, 5 fuzzy
	matching_algorithm?: #Matching
	// Case-insensitive matching (default true)
	is_insensitive?: bool
	// Array of tag IDs: any of them
	filter_has_tags?: [...#ID]
	// Array of tag IDs: all of them
	filter_has_all_tags?: [...#ID]
	// Array of tag IDs: none of them
	filter_has_not_tags?: [...#ID]
	// Array of custom field query terms, e.g. ["field_name", "exact", "value"] (custom_field_query syntax)
	filter_custom_field_query?: string | null
	// Array of correspondent IDs: any of them
	filter_has_any_correspondents?: [...#ID]
	// Array of correspondent IDs: none of them
	filter_has_not_correspondents?: [...#ID]
	// Array of document type IDs: any of them
	filter_has_any_document_types?: [...#ID]
	// Array of document type IDs: none of them
	filter_has_not_document_types?: [...#ID]
	// Array of storage path IDs: any of them
	filter_has_any_storage_paths?: [...#ID]
	// Array of storage path IDs: none of them
	filter_has_not_storage_paths?: [...#ID]
	// Correspondent ID the document must have
	filter_has_correspondent?: #ID | null
	// Document type ID the document must have
	filter_has_document_type?: #ID | null
	// Storage path ID the document must have
	filter_has_storage_path?: #ID | null
	// Type 4: days to offset the schedule by
	schedule_offset_days?: int
	// Type 4: repeat the schedule
	schedule_is_recurring?: bool
	// Type 4: days between recurring runs
	schedule_recurring_interval_days?: int & >=1
	// Type 4: the date field to schedule from
	schedule_date_field?: #ScheduleDateField
	// Type 4: custom field ID when schedule_date_field is custom_field
	schedule_date_custom_field?: #ID | null
}

#TriggerClear: {
	// Array of fields to clear
	clear?: [...or(clearable.trigger)]
}

#Email: {
	// Email subject (at most 256 characters)
	subject!: #Text256
	body!: string
	// Recipients, comma-separated
	to!: string
	include_document?: bool
}

#Webhook: {
	// Webhook URL (at most 256 characters)
	url!: #Text256
	// Send params instead of body (default true)
	use_params?: bool
	// Send the body as JSON (default false)
	as_json?: bool
	// Object with request parameters
	params?: {[string]: _} | null
	// Request body
	body?: string | null
	// Object with request headers
	headers?: {[string]: string} | null
	include_document?: bool
	// Array of fields to clear
	clear?: [...or(clearable.webhook)]
}

#ActionFields: {
	// Action type: 1 assignment, 2 removal, 3 email (needs email), 4 webhook (needs webhook), 5 password_removal (needs passwords), 6 move_to_trash, 7 remote_ocr, 8 apply_ai_suggestions (needs ai_suggestion_fields)
	type?: #ActionType
	// Title to assign: a Jinja2 template, may use custom fields (e.g. {{ custom_fields['Name'].value }})
	assign_title?: string | null
	// Array of tag IDs to assign
	assign_tags?: [...#ID] | null
	// Correspondent ID to assign
	assign_correspondent?: #ID | null
	// Document type ID to assign
	assign_document_type?: #ID | null
	// Storage path ID to assign
	assign_storage_path?: #ID | null
	// Owner user ID to assign
	assign_owner?: #ID | null
	// Array of user IDs to grant view permission
	assign_view_users?: [...#ID]
	// Array of group IDs to grant view permission
	assign_view_groups?: [...#ID]
	// Array of user IDs to grant change permission
	assign_change_users?: [...#ID]
	// Array of group IDs to grant change permission
	assign_change_groups?: [...#ID]
	// Array of custom field IDs to assign
	assign_custom_fields?: [...#ID]
	// Object with custom field values to assign, keyed by custom field ID
	assign_custom_fields_values?: {[=~"^[0-9]+$"]: _} | null
	remove_all_tags?: bool
	// Array of tag IDs to remove
	remove_tags?: [...#ID]
	remove_all_correspondents?: bool
	// Array of correspondent IDs to remove
	remove_correspondents?: [...#ID]
	remove_all_document_types?: bool
	// Array of document type IDs to remove
	remove_document_types?: [...#ID]
	remove_all_storage_paths?: bool
	// Array of storage path IDs to remove
	remove_storage_paths?: [...#ID]
	// Array of custom field IDs to remove
	remove_custom_fields?: [...#ID]
	remove_all_custom_fields?: bool
	remove_all_owners?: bool
	// Array of user IDs to remove as owner
	remove_owners?: [...#ID]
	remove_all_permissions?: bool
	// Array of user IDs to remove view permission from
	remove_view_users?: [...#ID]
	// Array of group IDs to remove view permission from
	remove_view_groups?: [...#ID]
	// Array of user IDs to remove change permission from
	remove_change_users?: [...#ID]
	// Array of group IDs to remove change permission from
	remove_change_groups?: [...#ID]
	// Array of passwords to try (type 5; non-empty)
	passwords?: [...#NonBlank] | null
	// Array of fields to fill from AI suggestions (type 8; non-empty)
	ai_suggestion_fields?: [...#AIField] | null
	// Type 8: create suggested objects that don't exist yet
	ai_create_missing?: bool
	// Type 8: overwrite values the document already has
	ai_overwrite_existing?: bool

	// Per-type requirements, checked only when type is given.
	if type != _|_ if type == 5 {
		passwords!: [#NonBlank, ...#NonBlank]
	}
	if type != _|_ if type == 8 {
		ai_suggestion_fields!: [#AIField, ...#AIField]
	}
}

#ActionClear: {
	// Array of fields to clear
	clear?: [...or(clearable.action)]
}

// Nested items in workflow_create: no ids (upstream would edit an existing row).

#TriggerCreateItem: {
	#TriggerFields
	#TriggerClear
	type!: #TriggerType
}

#ActionCreateItem: {
	#ActionFields
	#ActionClear
	type?: #ActionType
	// Email settings (type 3 email)
	email?: #Email
	// Webhook settings (type 4 webhook)
	webhook?: #Webhook
	if type != _|_ if type == 3 {
		email!: _
	}
	if type != _|_ if type == 4 {
		webhook!: _
	}
}

// Nested items in workflow_update: id updates that trigger or action in place,
// no id creates one. type is always required (upstream's workflow-level rules
// read it from the call).

#TriggerItem: {
	#TriggerFields
	#TriggerClear
	// ID of a trigger already attached to this workflow; leave out to add a new one
	id?: #ID
	type!: #TriggerType
}

#ActionItem: {
	#ActionFields
	#ActionClear
	// ID of an action already attached to this workflow; leave out to add a new one
	id?: #ID
	type!: #ActionType
	// Email settings (type 3 email)
	email?: {
		#Email
		// ID of this action's existing email settings
		id?: #ID
	}
	// Webhook settings (type 4 webhook)
	webhook?: {
		#Webhook
		// ID of this action's existing webhook settings
		id?: #ID
	}
	if type != _|_ if type == 3 {
		email!: _
	}
	if type != _|_ if type == 4 {
		webhook!: _
	}
}

// workflow_trigger_update: the trigger's fields, plus clear.

#TriggerPatch: {
	#TriggerFields
	#TriggerClear
}

// workflow_action_update: no email or webhook (only workflow_update can write
// them), and no types 3, 4, 7 or 8 (their rules are checked only on the
// workflow).

#ActionPatch: {
	#ActionFields
	#ActionClear
	// Action type: 1 assignment, 2 removal, 5 password_removal (needs passwords), 6 move_to_trash. Types 3, 4, 7 and 8 can only be set through workflow_update
	type?: 1 | 2 | 5 | 6
}

#WorkflowCreate: {
	// Workflow name (unique)
	name!: string & strings.MinRunes(1) & strings.MaxRunes(256)
	// Sort order (default 0)
	order?: int
	// Enabled (default true)
	enabled?: bool
	// Array of trigger objects (at least one): type plus that type's filters
	triggers!: [#TriggerCreateItem, ...#TriggerCreateItem]
	// Array of action objects (at least one): type plus that type's fields
	actions!: [#ActionCreateItem, ...#ActionCreateItem]
}

#WorkflowUpdate: {
	// Workflow name (unique)
	name?: string & strings.MinRunes(1) & strings.MaxRunes(256)
	// Sort order
	order?: int
	// Enabled
	enabled?: bool
	// Array of trigger objects replacing the workflow's triggers (at least one), each with the fields of workflow_create's triggers plus id to update an existing trigger; without id one is added, and triggers left out are deleted
	triggers?: [#TriggerItem, ...#TriggerItem]
	// Array of action objects replacing the workflow's actions (at least one), each with the fields of workflow_create's actions plus id to update an existing action (email and webhook may carry their id too); without id one is added, and actions left out are deleted
	actions?: [#ActionItem, ...#ActionItem]
}
