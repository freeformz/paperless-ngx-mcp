package main

// savedViewRuleType describes one saved view filter rule type: the rule_type
// code in a saved view's filter_rules, and what its value means.
type savedViewRuleType struct {
	ID        int    `json:"id"`
	Label     string `json:"label"`
	Value     string `json:"value"`
	Multi     bool   `json:"multi"`
	APIFilter string `json:"api_filter"`
	Notes     string `json:"notes,omitempty"`
}

// savedViewRuleTypes is paperless-ngx 3.5.1's rule type table. IDs and labels
// come from SavedViewFilterRule.RULE_TYPES (src/documents/models.py); value
// formats, multi-value use and the matching /api/documents/ filter come from
// the web UI's FILTER_RULE_TYPES (src-ui/src/app/data/filter-rule-type.ts) and
// its rule/query-parameter conversion (src-ui/src/app/utils/query-params.ts).
// Booleans are stored as "true" or "false" ("1" and "0" are also read).
var savedViewRuleTypes = []savedViewRuleType{
	{ID: 0, Label: "title contains", Value: "text", Multi: false, APIFilter: "title__icontains", Notes: "Deprecated; use 48. The web UI applies this as title_search (the same search as 48), not as a title__icontains substring match."},
	{ID: 1, Label: "content contains", Value: "text", Multi: false, APIFilter: "content__icontains"},
	{ID: 2, Label: "ASN is", Value: "integer", Multi: false, APIFilter: "archive_serial_number"},
	{ID: 3, Label: "correspondent is", Value: "correspondent ID", Multi: false, APIFilter: "correspondent__id", Notes: "Value null matches documents with no correspondent; \"-1\" matches documents with any correspondent."},
	{ID: 4, Label: "document type is", Value: "document type ID", Multi: false, APIFilter: "document_type__id", Notes: "Value null matches documents with no document type; \"-1\" matches documents with any document type."},
	{ID: 5, Label: "is in inbox", Value: "true or false", Multi: false, APIFilter: "is_in_inbox"},
	{ID: 6, Label: "has tag", Value: "tag ID", Multi: true, APIFilter: "tags__id__all", Notes: "One rule per tag; the document must have all of them."},
	{ID: 7, Label: "has any tag", Value: "true or false", Multi: false, APIFilter: "is_tagged"},
	{ID: 8, Label: "created before", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "created__date__lt", Notes: "Strictly before the date."},
	{ID: 9, Label: "created after", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "created__date__gt", Notes: "Strictly after the date."},
	{ID: 10, Label: "created year is", Value: "integer year", Multi: false, APIFilter: "created__year"},
	{ID: 11, Label: "created month is", Value: "integer month, 1 to 12", Multi: false, APIFilter: "created__month"},
	{ID: 12, Label: "created day is", Value: "integer day of month, 1 to 31", Multi: false, APIFilter: "created__day"},
	{ID: 13, Label: "added before", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "added__date__lt", Notes: "Strictly before the date."},
	{ID: 14, Label: "added after", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "added__date__gt", Notes: "Strictly after the date."},
	{ID: 15, Label: "modified before", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "modified__date__lt", Notes: "Strictly before the date."},
	{ID: 16, Label: "modified after", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "modified__date__gt", Notes: "Strictly after the date."},
	{ID: 17, Label: "does not have tag", Value: "tag ID", Multi: true, APIFilter: "tags__id__none", Notes: "One rule per tag; the document must have none of them."},
	{ID: 18, Label: "does not have ASN", Value: "true or false", Multi: false, APIFilter: "archive_serial_number__isnull"},
	{ID: 19, Label: "title or content contains", Value: "text", Multi: false, APIFilter: "title_content", Notes: "Deprecated; use 49. The web UI applies this as text (the same search as 49), not as title_content."},
	{ID: 20, Label: "fulltext query", Value: "full-text query", Multi: false, APIFilter: "query", Notes: "Same syntax as document_list's query. A view may use at most one of 48, 49, 20 and 21, except that 20 can be combined with 48 or 49 (0 and 19 count as 48 and 49); other combinations make document listing fail with a 400."},
	{ID: 21, Label: "more like this", Value: "document ID", Multi: false, APIFilter: "more_like_id", Notes: "Documents similar to this one. A view may use at most one of 48, 49, 20 and 21, except that 20 can be combined with 48 or 49 (0 and 19 count as 48 and 49); other combinations make document listing fail with a 400."},
	{ID: 22, Label: "has tags in", Value: "tag ID", Multi: true, APIFilter: "tags__id__in", Notes: "One rule per tag; the document must have at least one of them."},
	{ID: 23, Label: "ASN greater than", Value: "integer", Multi: false, APIFilter: "archive_serial_number__gt"},
	{ID: 24, Label: "ASN less than", Value: "integer", Multi: false, APIFilter: "archive_serial_number__lt"},
	{ID: 25, Label: "storage path is", Value: "storage path ID", Multi: false, APIFilter: "storage_path__id", Notes: "Value null matches documents with no storage path; \"-1\" matches documents with any storage path."},
	{ID: 26, Label: "has correspondent in", Value: "correspondent ID", Multi: true, APIFilter: "correspondent__id__in", Notes: "One rule per correspondent; any of them."},
	{ID: 27, Label: "does not have correspondent in", Value: "correspondent ID", Multi: true, APIFilter: "correspondent__id__none", Notes: "One rule per correspondent; none of them."},
	{ID: 28, Label: "has document type in", Value: "document type ID", Multi: true, APIFilter: "document_type__id__in", Notes: "One rule per document type; any of them."},
	{ID: 29, Label: "does not have document type in", Value: "document type ID", Multi: true, APIFilter: "document_type__id__none", Notes: "One rule per document type; none of them."},
	{ID: 30, Label: "has storage path in", Value: "storage path ID", Multi: true, APIFilter: "storage_path__id__in", Notes: "One rule per storage path; any of them."},
	{ID: 31, Label: "does not have storage path in", Value: "storage path ID", Multi: true, APIFilter: "storage_path__id__none", Notes: "One rule per storage path; none of them."},
	{ID: 32, Label: "owner is", Value: "user ID", Multi: false, APIFilter: "owner__id"},
	{ID: 33, Label: "has owner in", Value: "user ID", Multi: true, APIFilter: "owner__id__in", Notes: "One rule per user; any of them."},
	{ID: 34, Label: "does not have owner", Value: "true or false", Multi: false, APIFilter: "owner__isnull"},
	{ID: 35, Label: "does not have owner in", Value: "user ID", Multi: true, APIFilter: "owner__id__none", Notes: "One rule per user; none of them."},
	{ID: 36, Label: "has custom field value", Value: "text", Multi: false, APIFilter: "custom_fields__icontains", Notes: "Deprecated; use 42 (custom fields query)."},
	{ID: 37, Label: "is shared by me", Value: "user ID", Multi: true, APIFilter: "shared_by__id", Notes: "The value is the sharing user's ID."},
	{ID: 38, Label: "has custom fields", Value: "custom field ID", Multi: true, APIFilter: "custom_fields__id__all", Notes: "Legacy: when the web UI loads a view it turns these into a 42 rule (if 38 and 39 rules are both present, only the 38 rules are kept); the stored rules change only when the view is saved again. One rule per field; all of them."},
	{ID: 39, Label: "has custom field in", Value: "custom field ID", Multi: true, APIFilter: "custom_fields__id__in", Notes: "Legacy: when the web UI loads a view it turns these into a 42 rule (dropped if 38 rules are also present); the stored rules change only when the view is saved again. One rule per field; any of them."},
	{ID: 40, Label: "does not have custom field in", Value: "custom field ID", Multi: true, APIFilter: "custom_fields__id__none", Notes: "One rule per field; none of them."},
	{ID: 41, Label: "does not have custom field", Value: "true or false", Multi: false, APIFilter: "has_custom_fields", Notes: "Despite the label, the web UI applies this as has_custom_fields=<value>: true matches documents with any custom field, false those with none."},
	{ID: 42, Label: "custom fields query", Value: "custom field query JSON", Multi: false, APIFilter: "custom_field_query", Notes: "Same syntax as document_list's custom_field_query, as a JSON string; the 255-character value limit applies."},
	{ID: 43, Label: "created to", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "created__date__lte", Notes: "On or before the date."},
	{ID: 44, Label: "created from", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "created__date__gte", Notes: "On or after the date."},
	{ID: 45, Label: "added to", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "added__date__lte", Notes: "On or before the date."},
	{ID: 46, Label: "added from", Value: "date YYYY-MM-DD", Multi: false, APIFilter: "added__date__gte", Notes: "On or after the date."},
	{ID: 47, Label: "mime type is", Value: "MIME type, e.g. application/pdf", Multi: false, APIFilter: "mime_type", Notes: "Case-insensitive substring match, so \"pdf\" matches application/pdf."},
	{ID: 48, Label: "simple title search", Value: "text", Multi: false, APIFilter: "title_search", Notes: "Search in titles (document_list title_search). A view may use at most one of 48, 49, 20 and 21, except that 20 can be combined with 48 or 49 (0 and 19 count as 48 and 49); other combinations make document listing fail with a 400."},
	{ID: 49, Label: "simple text search", Value: "text", Multi: false, APIFilter: "text", Notes: "Search in title and content (document_list text). A view may use at most one of 48, 49, 20 and 21, except that 20 can be combined with 48 or 49 (0 and 19 count as 48 and 49); other combinations make document listing fail with a 400."},
	{ID: 50, Label: "has duplicates", Value: "true or false", Multi: false, APIFilter: "has_duplicates"},
}

// savedViewIcons are SavedView.Icon's values in paperless-ngx 3.5.1.
var savedViewIcons = []string{"archive", "bank", "basket", "bell", "bookmark", "boxes", "briefcase", "building", "calculator", "calendar", "camera", "card-checklist", "cash", "chat-left-text", "check-circle", "clipboard", "clock-history", "credit-card", "download", "envelope", "exclamation-triangle", "file-earmark", "file-earmark-check", "file-earmark-lock", "file-earmark-medical", "file-earmark-person", "file-earmark-spreadsheet", "file-text", "files", "folder", "funnel", "gear", "globe2", "hash", "heart", "house", "inbox", "journals", "list-task", "newspaper", "paperclip", "people", "person", "printer", "receipt", "safe", "search", "send", "shop", "stack", "stars", "tag", "tags", "telephone", "truck", "upc-scan", "wallet2"}

// savedViewDisplayModes are SavedView.DisplayMode's values.
var savedViewDisplayModes = []string{"table", "smallCards", "largeCards"}

// savedViewDisplayFields are SavedView.DisplayFields' fixed values; a
// custom_field_<id> entry for an existing custom field is also accepted.
var savedViewDisplayFields = []string{"title", "created", "added", "tag", "correspondent", "documenttype", "storagepath", "note", "owner", "shared", "asn", "pagecount"}
