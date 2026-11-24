package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// Filters for document_list and for the selection tools' filters object.
// paperless-ngx's bulk endpoints resolve all: true selections by running the
// document list's FilterSet over filters, but they ignore unknown keys and
// any value that fails to parse (django-filter drops it from cleaned_data;
// ObjectFilter returns the queryset unfiltered on a bad id list), so a typo
// selects every document. One table maps the MCP's friendly names to the raw
// query names, and the values are checked here before anything is sent.

type filterKind int

const (
	filterInt     filterKind = iota // a whole number
	filterIntList                   // ids, comma-separated or an array
	filterBool                      // true or false
	filterText                      // non-blank text
	filterDate                      // YYYY-MM-DD
	filterJSON                      // a JSON array expression (custom_field_query)
)

type filterParam struct {
	name string // the MCP name
	raw  string // the paperless-ngx query parameter
	kind filterKind
	desc string
}

// documentFilterParams are document_list's filters and the keys the selection
// tools' filters object accepts.
var documentFilterParams = []filterParam{
	{"text", "text", filterText, "Search: words (split on spaces, punctuation included) to find in title or content (indexed). Every word must appear, matched inside words (art finds party; in multi-word searches numbers match from a word start); correspondents, tags and notes aren't searched. Words of at most 128 bytes, at most 4096 characters"},
	{"title_search", "title_search", filterText, "Search: words to find in titles, matched like text. At most 4096 characters"},
	{"query", "query", filterText, "Advanced search with field syntax, e.g. tag:unpaid added:[-1 week to now]. AND/OR/NOT in capitals; a leading - does not exclude (use NOT); plain words don't search notes, custom fields or barcodes: use notes: (or notes.user:), custom_fields: (or custom_fields.name:), barcodes: (the singular note:/custom_field: match as plain text). See Document Search in the server instructions. At most 4096 characters; may be added to text or title_search as an extra filter"},
	{"more_like_id", "more_like_id", filterInt, "Documents similar to this document ID; can't be combined with text, title_search or query. Hits have score 0 and no highlights; an invalid or hidden ID is a 403"},
	{"correspondent_id", "correspondent__id", filterInt, "Filter by correspondent ID"},
	{"document_type_id", "document_type__id", filterInt, "Filter by document type ID"},
	{"storage_path_id", "storage_path__id", filterInt, "Filter by storage path ID"},
	{"tags_id_all", "tags__id__all", filterIntList, "Comma-separated tag IDs — document must have ALL"},
	{"tags_id_none", "tags__id__none", filterIntList, "Comma-separated tag IDs — document must have NONE"},
	{"tags_id_in", "tags__id__in", filterIntList, "Comma-separated tag IDs — document must have ANY"},
	{"is_tagged", "is_tagged", filterBool, "Filter by whether document has any tags"},
	{"is_in_inbox", "is_in_inbox", filterBool, "Filter by inbox status"},
	{"title", "title__icontains", filterText, "Title contains this text (case-insensitive database scan; title_search is the indexed search)"},
	{"content", "content__icontains", filterText, "Content contains this text (slow case-insensitive database scan; use text for normal search)"},
	{"custom_field_query", "custom_field_query", filterJSON, "Custom field filter expression, e.g. [\"AND\", [[1, \"exists\", true]]]; date values may be relative (today, in 30 days, 1 month ago); at most 10 levels deep and 20 conditions"},
	{"has_duplicates", "has_duplicates", filterBool, "Documents whose original or archive file matches another document you can see, including documents in the trash"},
	{"created_after", "created__date__gte", filterDate, "Filter by created date (gte, YYYY-MM-DD)"},
	{"created_before", "created__date__lte", filterDate, "Filter by created date (lte, YYYY-MM-DD)"},
	{"added_after", "added__date__gte", filterDate, "Filter by added date (gte, YYYY-MM-DD)"},
	{"added_before", "added__date__lte", filterDate, "Filter by added date (lte, YYYY-MM-DD)"},
	{"owner_id", "owner__id", filterInt, "Filter by owner"},
}

// objectFilterParams are bulk_edit_objects' filters per object type, matching
// the *_list tools' parameters.
var objectFilterParams = map[string][]filterParam{
	"tags": {
		{"name", "name__icontains", filterText, "Name contains (case-insensitive)"},
		{"is_root", "is_root", filterBool, "Only top-level tags (true) or only child tags (false)"},
	},
	"correspondents": {{"name", "name__icontains", filterText, "Name contains (case-insensitive)"}},
	"document_types": {{"name", "name__icontains", filterText, "Name contains (case-insensitive)"}},
	"storage_paths": {
		{"name", "name__icontains", filterText, "Name contains (case-insensitive)"},
		{"path", "path__icontains", filterText, "Path template contains (case-insensitive)"},
	},
}

// selectionFilterDesc shortens long filter descriptions in the selection
// tools' filters schemas, which repeat the table in six tools;
// document_list carries the full text.
var selectionFilterDesc = map[string]string{
	"text":               "Search title and content for these words (see document_list)",
	"title_search":       "Search titles for these words (see document_list)",
	"query":              "Advanced search with field syntax (see document_list)",
	"more_like_id":       "Documents similar to this document ID (see document_list)",
	"custom_field_query": "Custom field filter expression (see document_list)",
	"has_duplicates":     "Documents with a duplicate file (see document_list)",
	"title":              "Title contains (case-insensitive)",
	"content":            "Content contains (case-insensitive; slow)",
}

// maxQueryLen is the longest search text paperless-ngx accepts, for each of
// text, title_search and query.
const maxQueryLen = 4096

// searchParams are document_list's search parameters (raw names), of which
// paperless-ngx accepts one, or query together with text or title_search.
var searchParams = []string{"text", "title_search", "query", "more_like_id"}

// isSearchText reports whether a filter is search text, limited to
// maxQueryLen characters.
func isSearchText(name string) bool {
	return name == "text" || name == "title_search" || name == "query"
}

// maxSimpleWordBytes is the longest word simple search (text, title_search)
// keeps: paperless-ngx splits on whitespace and silently drops words of 129
// bytes or more (remove_long), which would widen a search.
const maxSimpleWordBytes = 128

// longSimpleWord returns the first word of s that simple search would drop.
func longSimpleWord(s string) (string, bool) {
	for _, w := range strings.Fields(s) {
		if len(w) > maxSimpleWordBytes {
			return w, true
		}
	}
	return "", false
}

// checkSearchParams applies paperless-ngx's rule for the search parameters
// given (raw names): one at most, except query with text or title_search,
// where query is an extra filter. Anything else is a 400 upstream.
func checkSearchParams(has func(string) bool) error {
	var active []string
	for _, p := range searchParams {
		if has(p) {
			active = append(active, p)
		}
	}
	if len(active) <= 1 || (len(active) == 2 && active[1] == "query" && active[0] != "more_like_id") {
		return nil
	}
	return fmt.Errorf("use only one of text, title_search, query and more_like_id (got %s); query may be added to text or title_search as an extra filter", strings.Join(active, ", "))
}

// trimFilterText trims what Python's str.strip removes: Unicode whitespace
// and the separators U+001C to U+001F, which Go's TrimSpace keeps.
func trimFilterText(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
}

// filterValueText converts one filter value to the text paperless-ngx expects
// as a query parameter. strict is for selections, where an unusable value
// would widen the selection; in lenient mode (document_list) empty values and
// the string "null" mean not given, and string booleans are accepted.
func filterValueText(p filterParam, v any, strict bool) (text string, ok bool, err error) {
	bad := func(format string) (string, bool, error) {
		return "", false, fmt.Errorf("%s must be %s", p.name, format)
	}
	if v == nil {
		if strict {
			return bad("a value, not null; leave the key out instead")
		}
		return "", false, nil
	}
	if s, isString := v.(string); isString && !strict && p.kind != filterText && (s == "" || s == "null") {
		return "", false, nil
	}
	switch p.kind {
	case filterInt:
		n, whole := wholeNumber(v)
		if !whole {
			return bad("a whole number")
		}
		return strconv.Itoa(n), true, nil
	case filterIntList:
		ids, ok := parseIDList(v)
		if !ok {
			return bad("ids: a comma-separated string such as \"1,2\" or an array of integers, with no empty items")
		}
		return ids, true, nil
	case filterBool:
		switch b := v.(type) {
		case bool:
			return strconv.FormatBool(b), true, nil
		case string:
			if strict && b != "true" && b != "false" {
				return bad("true or false")
			}
			parsed, err := strconv.ParseBool(b)
			if err != nil {
				return bad("true or false")
			}
			return strconv.FormatBool(parsed), true, nil
		case float64, int, int64:
			// document_list (lenient) takes 1 and 0, like the other boolean
			// params; selection filters stay strict.
			if n, whole := wholeNumber(b); !strict && whole && (n == 0 || n == 1) {
				return strconv.FormatBool(n == 1), true, nil
			}
		}
		return bad("true or false")
	case filterDate:
		s, isString := v.(string)
		if !isString {
			return bad("a date, YYYY-MM-DD")
		}
		d, err := time.Parse("2006-01-02", strings.TrimSpace(s))
		if err != nil || d.Year() < 1 || d.Year() > 9999 {
			return bad("a valid date, YYYY-MM-DD")
		}
		return d.Format("2006-01-02"), true, nil
	case filterJSON:
		if s, isString := v.(string); isString {
			if !strict {
				return s, s != "", nil
			}
			var decoded any
			if json.Unmarshal([]byte(s), &decoded) != nil {
				return bad("a JSON array expression")
			}
			v = decoded
		}
		if _, isArray := v.([]any); !isArray {
			return bad("a JSON array expression, e.g. [\"AND\", [[1, \"exists\", true]]]")
		}
		b, err := json.Marshal(v)
		if err != nil {
			return bad("a JSON array expression")
		}
		return string(b), true, nil
	default: // filterText
		s, isString := v.(string)
		if !isString {
			return bad("text")
		}
		trimmed := trimFilterText(s)
		simple := p.name == "text" || p.name == "title_search"
		if !strict {
			// document_list: blank is not given; only the checks paperless-ngx
			// would fail on (or answer with nothing) apply.
			// A whitespace-only text or title_search is padding (upstream it
			// would match nothing); query keeps its old meaning.
			switch {
			case !isSearchText(p.name):
				return s, s != "", nil
			case s == "" || (simple && trimmed == ""):
				return "", false, nil
			case utf8.RuneCountInString(s) > maxQueryLen:
				return bad(fmt.Sprintf("at most %d characters", maxQueryLen))
			}
			if w, long := longSimpleWord(s); simple && long {
				return bad(fmt.Sprintf("words of at most %d bytes; paperless-ngx silently drops longer ones (%.20s…)", maxSimpleWordBytes, w))
			}
			return s, true, nil
		}
		switch {
		case trimmed == "" && simple:
			return bad("non-blank text (blank search text selects nothing)")
		case trimmed == "":
			return bad("non-blank text (blank text is ignored upstream, which would select everything)")
		case strings.ContainsRune(s, 0):
			return bad("text without NUL characters")
		case p.name == "query" && strings.Trim(trimmed, "*? \t") == "":
			return bad("a real search query; wildcards alone match everything, so leave query out to mean every document")
		case isSearchText(p.name) && utf8.RuneCountInString(s) > maxQueryLen:
			return bad(fmt.Sprintf("at most %d characters", maxQueryLen))
		}
		if w, long := longSimpleWord(s); simple && long {
			return bad(fmt.Sprintf("words of at most %d bytes; paperless-ngx silently drops longer ones, which widens the selection (%.20s…)", maxSimpleWordBytes, w))
		}
		return s, true, nil
	}
}

// parseIDList parses ids given as "1,2" or [1, 2] and returns them joined by
// commas. Empty items, empty lists and non-integers fail: paperless-ngx would
// ignore the whole filter.
func parseIDList(v any) (string, bool) {
	var items []string
	if s, ok := v.(string); ok && strings.HasPrefix(strings.TrimSpace(s), "[") {
		// A JSON-encoded array, as the instructions allow for array params.
		var decoded []any
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return "", false
		}
		v = decoded
	}
	switch x := v.(type) {
	case string:
		items = strings.Split(x, ",")
	case []any:
		for _, e := range x {
			n, whole := wholeNumber(e)
			if _, isNum := e.(float64); !isNum || !whole {
				return "", false
			}
			items = append(items, strconv.Itoa(n))
		}
	default:
		return "", false
	}
	if len(items) == 0 {
		return "", false
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		n, err := strconv.Atoi(strings.TrimSpace(it))
		if err != nil {
			return "", false
		}
		out = append(out, strconv.Itoa(n))
	}
	return strings.Join(out, ","), true
}

// addDocumentListFilters adds document_list's filter parameters in lenient
// mode: absent, null and empty values are skipped, and values that don't
// parse are an error rather than a silent 0 or false.
func addDocumentListFilters(params url.Values, request mcp.CallToolRequest) *mcp.CallToolResult {
	args := request.GetArguments()
	for _, p := range documentFilterParams {
		v, present := args[p.name]
		if !present {
			continue
		}
		text, ok, err := filterValueText(p, v, false)
		if err != nil {
			return errResult(err.Error())
		}
		if ok {
			params.Set(p.raw, text)
		}
	}
	if err := checkSearchParams(params.Has); err != nil {
		return errResult(err.Error())
	}
	return nil
}

// documentListFilterOptions declares document_list's filter parameters.
func documentListFilterOptions() []mcp.ToolOption {
	opts := make([]mcp.ToolOption, 0, len(documentFilterParams))
	for _, p := range documentFilterParams {
		switch p.kind {
		case filterInt:
			opts = append(opts, withNumber(p.name, mcp.Description(p.desc)))
		case filterBool:
			opts = append(opts, mcp.WithBoolean(p.name, mcp.Description(p.desc)))
		case filterJSON:
			opts = append(opts, withJSONArray(p.name, mcp.Description(p.desc)))
		default:
			opts = append(opts, mcp.WithString(p.name, mcp.Description(p.desc)))
		}
	}
	return opts
}

// parseSelectionFilters reads a filters object in strict mode and returns the
// raw parameters to send. present is false when filters is absent or null.
func parseSelectionFilters(request mcp.CallToolRequest, table []filterParam, label string) (raw map[string]any, present bool, errRes *mcp.CallToolResult) {
	v, present, err := getJSONArg(request, "filters")
	if err != nil {
		return nil, false, errResult(err.Error())
	}
	if !present || v == nil {
		return nil, false, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, true, errResult("filters must be an object of " + label + " filters, e.g. {\"" + table[0].name + "\": ...}")
	}
	names := make([]string, len(table))
	for i, p := range table {
		names[i] = p.name
	}
	raw = map[string]any{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		i := slices.IndexFunc(table, func(p filterParam) bool { return p.name == k })
		if i < 0 {
			return nil, true, errResult(fmt.Sprintf("filters: unknown filter %q; %s filters are: %s", k, label, strings.Join(names, ", ")))
		}
		text, _, err := filterValueText(table[i], m[k], true)
		if err != nil {
			return nil, true, errResult("filters: " + err.Error())
		}
		raw[table[i].raw] = text
	}
	if err := checkSearchParams(func(name string) bool { _, ok := raw[name]; return ok }); err != nil {
		return nil, true, errResult("filters: " + err.Error())
	}
	return raw, true, nil
}

// filtersSchema describes a filters object for clients that validate it.
func filtersSchema(tables ...[]filterParam) func(map[string]any) {
	return func(schema map[string]any) {
		props := map[string]any{}
		for _, table := range tables {
			for _, p := range table {
				var typ any
				switch p.kind {
				case filterInt:
					typ = []string{"integer", "string"}
				case filterIntList, filterJSON:
					typ = []string{"array", "string"}
				case filterBool:
					typ = []string{"boolean", "string"}
				default:
					typ = "string"
				}
				desc := p.desc
				if short, ok := selectionFilterDesc[p.name]; ok {
					desc = short
				}
				props[p.name] = map[string]any{"type": typ, "description": desc}
			}
		}
		schema["properties"] = props
		schema["additionalProperties"] = false
	}
}
