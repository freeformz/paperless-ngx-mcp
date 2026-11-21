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
	{"query", "query", filterText, "Full-text search query"},
	{"more_like_id", "more_like_id", filterInt, "Find documents similar to this document ID"},
	{"correspondent_id", "correspondent__id", filterInt, "Filter by correspondent ID"},
	{"document_type_id", "document_type__id", filterInt, "Filter by document type ID"},
	{"storage_path_id", "storage_path__id", filterInt, "Filter by storage path ID"},
	{"tags_id_all", "tags__id__all", filterIntList, "Comma-separated tag IDs — document must have ALL"},
	{"tags_id_none", "tags__id__none", filterIntList, "Comma-separated tag IDs — document must have NONE"},
	{"tags_id_in", "tags__id__in", filterIntList, "Comma-separated tag IDs — document must have ANY"},
	{"is_tagged", "is_tagged", filterBool, "Filter by whether document has any tags"},
	{"is_in_inbox", "is_in_inbox", filterBool, "Filter by inbox status"},
	{"title", "title__icontains", filterText, "Filter by title (icontains)"},
	{"content", "content__icontains", filterText, "Filter by content (icontains)"},
	{"custom_field_query", "custom_field_query", filterJSON, "Custom field filter expression, e.g. [\"AND\", [[1, \"exists\", true]]]"},
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

// maxQueryLen is the longest full-text query paperless-ngx accepts.
const maxQueryLen = 4096

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
		if !strict {
			return s, s != "", nil
		}
		trimmed := trimFilterText(s)
		switch {
		case trimmed == "":
			return bad("non-blank text (blank text is ignored upstream, which would select everything)")
		case strings.ContainsRune(s, 0):
			return bad("text without NUL characters")
		case p.name == "query" && strings.Trim(trimmed, "*? \t") == "":
			return bad("a real search query; wildcards alone match everything, so leave query out to mean every document")
		case p.name == "query" && utf8.RuneCountInString(s) > maxQueryLen:
			return bad(fmt.Sprintf("at most %d characters", maxQueryLen))
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
	if params.Has("query") && params.Has("more_like_id") {
		return errResult("query and more_like_id can't be combined")
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
	if _, q := raw["query"]; q {
		if _, ml := raw["more_like_id"]; ml {
			return nil, true, errResult("filters: query and more_like_id can't be combined")
		}
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
				props[p.name] = map[string]any{"type": typ, "description": p.desc}
			}
		}
		schema["properties"] = props
		schema["additionalProperties"] = false
	}
}
