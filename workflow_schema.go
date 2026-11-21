package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
)

// The workflow tools' parameters come from schema/workflow.cue: it is the one
// source for both the input schema each tool advertises and the validation of
// every call. paperless-ngx silently ignores unknown workflow fields, so this
// is where a misspelled field is caught.

//go:embed schema/workflow.cue
var workflowCUE []byte

// The CUE definitions the tools use.
const (
	defWorkflowCreate = "#WorkflowCreate"
	defWorkflowUpdate = "#WorkflowUpdate"
	defTriggerPatch   = "#TriggerPatch"
	defActionPatch    = "#ActionPatch"
)

var workflowDefs = []string{defWorkflowCreate, defWorkflowUpdate, defTriggerPatch, defActionPatch}

// namesKey marks, in the internal copy of a schema, an enum whose values have
// names (trigger types, sources, matching algorithms, action types).
const namesKey = "x-names"

// workflowSchemas holds the compiled CUE and the schemas derived from it.
type workflowSchemas struct {
	defs     map[string]cue.Value
	internal map[string]map[string]any // per definition: resolved JSON Schema, enums annotated with namesKey
	names    map[string]map[string]int // kind -> accepted name (incl. aliases) -> number
	labels   map[string]map[int]string // kind -> number -> canonical name
	ctx      *cue.Context
	uses     int // validations since ctx was made; see refreshContext
}

// cueContextUses bounds how many calls share one CUE context: a context keeps
// every field name it has seen, and calls bring arbitrary keys (unknown
// fields, webhook params, custom field ids).
const cueContextUses = 500

// refreshContext replaces the CUE context and the definitions compiled in
// it. The caller holds cueMu.
func (ws *workflowSchemas) refreshContext() {
	ctx := cuecontext.New()
	root := ctx.CompileBytes(workflowCUE, cue.Filename("schema/workflow.cue"))
	if root.Err() != nil {
		return // it compiled at startup; keep the old context
	}
	defs := map[string]cue.Value{}
	for _, d := range workflowDefs {
		def := root.LookupPath(cue.ParsePath(d))
		if def.Err() != nil {
			return
		}
		defs[d] = def
	}
	ws.ctx, ws.defs, ws.uses = ctx, defs, 0
}

// cueMu serialises every use of the CUE runtime, which isn't safe for
// concurrent evaluation; tool calls run on a worker pool.
var cueMu sync.Mutex

var loadWorkflowSchemas = sync.OnceValues(func() (*workflowSchemas, error) {
	cueMu.Lock()
	defer cueMu.Unlock()
	ctx := cuecontext.New()
	root := ctx.CompileBytes(workflowCUE, cue.Filename("schema/workflow.cue"))
	if err := root.Err(); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	ws := &workflowSchemas{
		defs:     map[string]cue.Value{},
		internal: map[string]map[string]any{},
		names:    map[string]map[string]int{},
		labels:   map[string]map[int]string{},
		ctx:      ctx,
	}
	var canonical, aliases map[string]map[string]int
	if err := root.LookupPath(cue.ParsePath("names")).Decode(&canonical); err != nil {
		return nil, fmt.Errorf("names: %w", err)
	}
	if err := root.LookupPath(cue.ParsePath("aliases")).Decode(&aliases); err != nil {
		return nil, fmt.Errorf("aliases: %w", err)
	}
	for kind, m := range canonical {
		ws.names[kind] = maps.Clone(m)
		ws.labels[kind] = map[int]string{}
		for name, n := range m {
			ws.labels[kind][n] = name
		}
		for name, n := range aliases[kind] {
			ws.names[kind][name] = n
		}
	}
	for _, d := range workflowDefs {
		def := root.LookupPath(cue.ParsePath(d))
		if err := def.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", d, err)
		}
		expr, err := jsonschema.Generate(def, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: generate: %w", d, err)
		}
		raw, err := ctx.BuildExpr(expr).MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("%s: marshal: %w", d, err)
		}
		var gen map[string]any
		if err := json.Unmarshal(raw, &gen); err != nil {
			return nil, fmt.Errorf("%s: unmarshal: %w", d, err)
		}
		defsMap, _ := gen["$defs"].(map[string]any)
		resolved, ok := resolveSchema(gen, defsMap, 0).(map[string]any)
		if !ok || resolved["properties"] == nil {
			return nil, fmt.Errorf("%s: resolved schema has no properties", d)
		}
		if err := ws.annotateNames(resolved); err != nil {
			return nil, fmt.Errorf("%s: %w", d, err)
		}
		ws.defs[d] = def
		ws.internal[d] = resolved
	}
	return ws, nil
})

// mustWorkflowSchemas returns the loaded schemas; the CUE is embedded, so a
// failure is a programming error.
func mustWorkflowSchemas() *workflowSchemas {
	ws, err := loadWorkflowSchemas()
	if err != nil {
		panic("internal: workflow schema: " + err.Error())
	}
	return ws
}

// resolveSchema inlines $refs, merges allOf branches and $ref siblings,
// collapses "T | null" to T (nulls are never sent: they mean "not given",
// and clearing goes through clear), and drops keywords the advertised schema
// doesn't carry (conditionals, prefixItems, $schema, $defs).
func resolveSchema(node any, defs map[string]any, depth int) any {
	if depth > 64 {
		panic("internal: workflow schema: $ref nesting too deep")
	}
	switch n := node.(type) {
	case bool:
		if n {
			return map[string]any{}
		}
		return n
	case map[string]any:
		out := map[string]any{}
		var base map[string]any
		if ref, ok := n["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/$defs/")
			if r, ok := resolveSchema(defs[name], defs, depth+1).(map[string]any); ok {
				base = r
			}
		}
		for k, v := range n {
			switch k {
			case "$ref", "$schema", "$defs", "if", "then", "else", "not", "prefixItems", "dependentRequired", "dependentSchemas":
			case "allOf":
				for _, b := range v.([]any) {
					if r, ok := resolveSchema(b, defs, depth+1).(map[string]any); ok {
						base = mergeSchemas(base, r)
					}
				}
			case "anyOf", "oneOf":
				var keep []any
				for _, b := range v.([]any) {
					r := resolveSchema(b, defs, depth+1)
					if m, ok := r.(map[string]any); ok && m["type"] == "null" && len(m) == 1 {
						continue
					}
					keep = append(keep, r)
				}
				if len(keep) == 1 {
					if r, ok := keep[0].(map[string]any); ok {
						base = mergeSchemas(base, r)
					}
				} else if len(keep) > 1 {
					out[k] = keep
				}
			case "properties":
				props := map[string]any{}
				for pk, pv := range v.(map[string]any) {
					props[pk] = resolveSchema(pv, defs, depth+1)
				}
				out[k] = props
			case "items":
				out[k] = resolveSchema(v, defs, depth+1)
			case "const":
				// An open struct comes out as const {}; it's any object.
				if m, ok := v.(map[string]any); ok && len(m) == 0 {
					out["type"] = "object"
					continue
				}
				out[k] = v
			case "additionalProperties":
				if b, ok := v.(bool); ok {
					out[k] = b
				} else {
					out[k] = resolveSchema(v, defs, depth+1)
				}
			default:
				out[k] = v
			}
		}
		if base != nil {
			out = mergeSchemas(base, out)
		}
		return out
	}
	return node
}

// mergeSchemas combines two object or scalar schemas, b refining a: a
// property only b names, or names with constraints, wins; an empty
// property schema (CUE's _) keeps a's. required is the union and
// additionalProperties false wins.
func mergeSchemas(a, b map[string]any) map[string]any {
	if a == nil {
		return maps.Clone(b)
	}
	out := maps.Clone(a)
	for k, v := range b {
		switch k {
		case "properties":
			props, _ := out[k].(map[string]any)
			merged := maps.Clone(props)
			if merged == nil {
				merged = map[string]any{}
			}
			for pk, pv := range v.(map[string]any) {
				if m, ok := pv.(map[string]any); ok && len(m) == 0 {
					if _, have := merged[pk]; have {
						continue
					}
				}
				if old, ok := merged[pk].(map[string]any); ok {
					if nv, ok := pv.(map[string]any); ok {
						// A doc comment on the refining field is kept; the
						// base keeps its own otherwise.
						m := maps.Clone(nv)
						if _, has := m["description"]; !has && old["description"] != nil {
							m["description"] = old["description"]
						}
						if _, has := m["enum"]; !has && old["enum"] != nil && m["type"] == nil {
							m["enum"] = old["enum"]
						}
						merged[pk] = m
						continue
					}
				}
				merged[pk] = pv
			}
			out[k] = merged
		case "required":
			var req []string
			for _, r := range append(toStrings(out[k]), toStrings(v)...) {
				if !slices.Contains(req, r) {
					req = append(req, r)
				}
			}
			out[k] = req
		case "additionalProperties":
			if out[k] == false || v == false {
				out[k] = false
			} else {
				out[k] = v
			}
		case "type":
			// "integer" refines "number".
			if out[k] == "integer" && v == "number" {
				continue
			}
			out[k] = v
		default:
			out[k] = v
		}
	}
	return out
}

func toStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// annotateNames marks every integer enum whose values belong to a named
// choice set, so normalisation and error messages can use the names. An enum
// equal to a set takes that set; a strict subset (workflow_action_update's
// types) takes the only set containing all its values.
func (ws *workflowSchemas) annotateNames(node map[string]any) error {
	var walkErr error
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if enum, ok := n["enum"].([]any); ok {
			nums := []int{}
			for _, e := range enum {
				if f, ok := e.(float64); ok {
					nums = append(nums, int(f))
				}
			}
			if len(nums) == len(enum) && len(nums) > 0 {
				var exact string
				var supersets []string
				for kind, labels := range ws.labels {
					contains := true
					for _, x := range nums {
						if _, ok := labels[x]; !ok {
							contains = false
						}
					}
					if !contains {
						continue
					}
					if len(labels) == len(nums) {
						exact = kind
					}
					supersets = append(supersets, kind)
				}
				switch {
				case exact != "":
					n[namesKey] = exact
				case len(supersets) == 1:
					n[namesKey] = supersets[0]
				case len(supersets) > 1:
					walkErr = fmt.Errorf("enum %v matches several choice sets: %v", nums, supersets)
				}
			}
		}
		if props, ok := n["properties"].(map[string]any); ok {
			for _, p := range props {
				if m, ok := p.(map[string]any); ok {
					walk(m)
				}
			}
		}
		if items, ok := n["items"].(map[string]any); ok {
			walk(items)
		}
	}
	walk(node)
	return walkErr
}

// workflowToolSchema returns the input schema a workflow tool advertises for
// a definition, following the repo's conventions: top-level arrays and
// objects also accept a JSON-encoded string, integers also accept numeric
// strings, and named enums accept names too.
func workflowToolSchema(def string) mcp.ToolInputSchema {
	ws := mustWorkflowSchemas()
	internal := ws.internal[def]
	adv := advertise(deepCopy(internal).(map[string]any), ws)
	props, _ := adv["properties"].(map[string]any)
	for name, p := range props {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "array", "object":
			m["type"] = []string{m["type"].(string), "string"}
		case "integer":
			if _, named := internal["properties"].(map[string]any)[name].(map[string]any)[namesKey]; !named {
				m["type"] = []string{"integer", "string"}
				m["pattern"] = `^-?\d+$`
			}
		}
		// filter_custom_field_query is JSON text upstream; it also takes an array.
		if name == "filter_custom_field_query" {
			m["type"] = []string{"array", "string"}
		}
	}
	return mcp.ToolInputSchema{
		Type:       "object",
		Properties: props,
		Required:   toStrings(adv["required"]),
	}
}

// advertise turns the internal schema into what clients see: named enums
// list the names too and accept integers or strings.
func advertise(node map[string]any, ws *workflowSchemas) map[string]any {
	if kind, ok := node[namesKey].(string); ok {
		delete(node, namesKey)
		enum := node["enum"].([]any)
		nums := []int{}
		for _, e := range enum {
			nums = append(nums, int(e.(float64)))
		}
		slices.Sort(nums)
		values := []any{}
		for _, n := range nums {
			values = append(values, n)
		}
		for _, n := range nums {
			values = append(values, ws.labels[kind][n])
		}
		node["enum"] = values
		node["type"] = []string{"integer", "string"}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for k, p := range props {
			if m, ok := p.(map[string]any); ok {
				props[k] = advertise(m, ws)
				// JSON text upstream; it also takes an array.
				if k == "filter_custom_field_query" {
					props[k].(map[string]any)["type"] = []string{"array", "string"}
				}
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		node["items"] = advertise(items, ws)
	}
	return node
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = deepCopy(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopy(e)
		}
		return out
	case []string:
		return slices.Clone(x)
	}
	return v
}

// workflowInputError collects problems with a call, each with its path.
type workflowInputError struct{ problems []string }

func (e *workflowInputError) add(path, msg string) {
	e.problems = append(e.problems, strings.TrimPrefix(path+": "+msg, ": "))
}

// normalizeWorkflowInput cleans a call's arguments for def, the way the
// other tools treat their parameters: nulls and padding ("" and {} for
// non-text values, [] for triggers, actions and sources) are not given,
// numbers and booleans may come as strings, arrays and objects may come as
// JSON text at the top level, and named choices become numbers. Each
// object's clear list sets those fields to null. It returns the body to
// validate and send.
func (ws *workflowSchemas) normalizeWorkflowInput(def string, args map[string]any) (map[string]any, *workflowInputError) {
	errs := &workflowInputError{}
	out := ws.normalizeObject(args, ws.internal[def], "", true, errs)
	if len(errs.problems) > 0 {
		return nil, errs
	}
	return out, nil
}

func (ws *workflowSchemas) normalizeObject(in map[string]any, schema map[string]any, path string, top bool, errs *workflowInputError) map[string]any {
	props, _ := schema["properties"].(map[string]any)
	required := toStrings(schema["required"])
	out := map[string]any{}
	for k, v := range in {
		if k == "clear" {
			continue
		}
		p := joinPath(path, k)
		ps, known := props[k].(map[string]any)
		if !known {
			out[k] = v // CUE reports it as an unknown field
			continue
		}
		// Below the top level (where the middleware does it), the string
		// "null" in an optional text field is padding too, as at the top.
		if s, ok := v.(string); ok && !top && strings.TrimSpace(s) == "null" && !slices.Contains(required, k) {
			continue
		}
		nv, keep := ws.normalizeValue(k, v, ps, p, top, errs)
		if keep {
			out[k] = nv
		}
	}
	if _, has := props["clear"]; has {
		ws.applyObjectClear(in["clear"], props["clear"].(map[string]any), out, path, errs)
	} else if argIsSet(in["clear"]) {
		out["clear"] = in["clear"] // CUE reports it as an unknown field
	}
	return out
}

// applyObjectClear sets each field listed in an object's clear to null.
func (ws *workflowSchemas) applyObjectClear(raw any, schema map[string]any, out map[string]any, path string, errs *workflowInputError) {
	if !argIsSet(raw) {
		return
	}
	var names []string
	switch v := raw.(type) {
	case string:
		if s := strings.TrimSpace(v); strings.HasPrefix(s, "[") {
			if err := json.Unmarshal([]byte(s), &names); err != nil {
				errs.add(joinPath(path, "clear"), "must be an array of field names")
				return
			}
		} else {
			names = []string{s}
		}
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				errs.add(joinPath(path, "clear"), "must be an array of field names")
				return
			}
			names = append(names, s)
		}
	default:
		errs.add(joinPath(path, "clear"), "must be an array of field names")
		return
	}
	var allowed []string
	if items, ok := schema["items"].(map[string]any); ok {
		allowed = toStrings(items["enum"])
	}
	for _, n := range names {
		if !slices.Contains(allowed, n) {
			errs.add(joinPath(path, "clear"), fmt.Sprintf("%q can't be cleared; clearable fields: %s", n, strings.Join(allowed, ", ")))
			continue
		}
		if argIsSet(out[n]) {
			errs.add(joinPath(path, n), "is both given a value and listed in clear; pick one")
			continue
		}
		out[n] = nil
		if clearToEmpty[n] {
			out[n] = ""
		}
	}
}

// clearToEmpty are clearable fields that can't be null upstream: clearing
// sends an empty string.
var clearToEmpty = map[string]bool{"match": true}

func (ws *workflowSchemas) normalizeValue(key string, v any, schema map[string]any, path string, top bool, errs *workflowInputError) (any, bool) {
	if v == nil {
		return nil, false
	}
	typ, _ := schema["type"].(string)
	if s, ok := v.(string); ok {
		t := strings.TrimSpace(s)
		// key is "" for list items, which are never padding.
		switch {
		case t == "" && key != "":
			return nil, false // padding; clearing goes through clear
		case typ == "string" && key != "filter_custom_field_query":
			return v, true
		case t == "null" && key != "":
			return nil, false
		case top && (typ == "array" || typ == "object" || key == "filter_custom_field_query") && (strings.HasPrefix(t, "[") || strings.HasPrefix(t, "{")):
			var decoded any
			if err := json.Unmarshal([]byte(t), &decoded); err != nil {
				errs.add(path, "invalid JSON: "+err.Error())
				return nil, false
			}
			if key == "filter_custom_field_query" {
				return t, true
			}
			v = decoded
		}
	}
	if key == "filter_custom_field_query" {
		if s, ok := v.(string); ok {
			var decoded []any
			if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &decoded); err != nil {
				errs.add(path, "must be a custom field query: an array, or its JSON text")
				return nil, false
			}
			return strings.TrimSpace(s), true
		}
		if arr, ok := v.([]any); ok {
			b, err := json.Marshal(arr)
			if err != nil {
				errs.add(path, err.Error())
				return nil, false
			}
			return string(b), true
		}
		return v, true
	}
	if kind, ok := schema[namesKey].(string); ok {
		return ws.normalizeNamed(kind, v), true
	}
	switch typ {
	case "integer":
		if n, whole := wholeNumber(v); whole {
			return n, true
		}
		return v, true
	case "boolean":
		switch x := v.(type) {
		case string:
			if b, err := strconv.ParseBool(strings.TrimSpace(x)); err == nil {
				return b, true
			}
		case float64:
			if x == 0 || x == 1 {
				return x == 1, true
			}
		}
		return v, true
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return v, true
		}
		if len(arr) == 0 && (key == "triggers" || key == "actions") {
			return nil, false
		}
		items, _ := schema["items"].(map[string]any)
		out := make([]any, len(arr))
		for i, e := range arr {
			ip := fmt.Sprintf("%s[%d]", path, i)
			if m, ok := e.(map[string]any); ok && items["properties"] != nil {
				out[i] = ws.normalizeObject(m, items, ip, false, errs)
				continue
			}
			if items != nil {
				nv, _ := ws.normalizeValue("", e, items, ip, false, errs)
				out[i] = nv
				continue
			}
			out[i] = e
		}
		return out, true
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return v, true
		}
		if schema["properties"] == nil {
			// Free-form JSON (params, headers, custom field values): as given;
			// {} is padding, and clearing goes through clear.
			if len(m) == 0 {
				return nil, false
			}
			return m, true
		}
		if len(m) == 0 {
			return nil, false
		}
		return ws.normalizeObject(m, schema, path, false, errs), true
	}
	return v, true
}

// normalizeNamed turns a choice name into its number; anything else is
// left for CUE to judge.
func (ws *workflowSchemas) normalizeNamed(kind string, v any) any {
	switch x := v.(type) {
	case string:
		key := strings.ToLower(strings.TrimSpace(x))
		key = strings.NewReplacer(" ", "_", "-", "_").Replace(key)
		if n, ok := ws.names[kind][key]; ok {
			return n
		}
		if n, err := strconv.Atoi(key); err == nil {
			return n
		}
	case float64:
		if n, whole := wholeNumber(x); whole {
			return n
		}
	}
	return v
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// validateWorkflowInput checks a normalised body against def and returns
// readable problems, or nil. CUE leaves out "field is required" errors while
// a value has other errors, so after each round the fields it reported as
// unknown are removed and the body is checked again; every round's problems
// are kept.
func (ws *workflowSchemas) validateWorkflowInput(def string, body map[string]any) []string {
	current := deepCopy(body).(map[string]any)
	var all []string
	seen := map[string]bool{}
	for range 4 {
		problems, unknown, err := ws.validateOnce(def, current)
		if err != nil {
			return []string{err.Error()}
		}
		for _, p := range problems {
			if !seen[p] {
				seen[p] = true
				all = append(all, p)
			}
		}
		if len(unknown) == 0 {
			break
		}
		for _, segs := range unknown {
			deleteAt(current, segs)
		}
	}
	sort.Strings(all)
	if len(all) > 10 {
		all = append(all[:10], fmt.Sprintf("… and %d more", len(all)-10))
	}
	return all
}

func (ws *workflowSchemas) validateOnce(def string, body map[string]any) (problems []string, unknown [][]string, err error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	cueMu.Lock()
	defer cueMu.Unlock()
	if ws.uses++; ws.uses > cueContextUses {
		ws.refreshContext()
	}
	data := ws.ctx.CompileBytes(raw, cue.Filename("arguments"))
	if err := data.Err(); err != nil {
		return nil, nil, err
	}
	verr := ws.defs[def].Unify(data).Validate(cue.Concrete(true))
	if verr == nil {
		return nil, nil, nil
	}
	problems, unknown = ws.formatCUEErrors(def, verr)
	if len(problems) == 0 {
		// Never hide an error the filters didn't recognise.
		problems = []string{verr.Error()}
	}
	return problems, unknown, nil
}

// deleteAt removes the value at a CUE error path (map keys and list indexes).
func deleteAt(node any, segs []string) {
	for i, s := range segs {
		last := i == len(segs)-1
		switch n := node.(type) {
		case map[string]any:
			if last {
				delete(n, s)
				return
			}
			node = n[s]
		case []any:
			idx, err := strconv.Atoi(s)
			if err != nil || idx < 0 || idx >= len(n) {
				return
			}
			if last {
				return
			}
			node = n[idx]
		default:
			return
		}
	}
}

// formatCUEErrors turns CUE's errors into one line per problem, with paths
// like triggers[1].type, and replaces CUE's disjunction noise with the
// allowed values.
func (ws *workflowSchemas) formatCUEErrors(def string, err error) ([]string, [][]string) {
	var unknown [][]string
	type problem struct{ path, msg string }
	seen := map[string]bool{}
	var out []problem
	enumDone := map[string]bool{}
	add := func(path, msg string) {
		key := path + "\x00" + msg
		if !seen[key] {
			seen[key] = true
			out = append(out, problem{path, msg})
		}
	}
	for _, e := range cueerrors.Errors(err) {
		segs := e.Path()
		if len(segs) > 0 && strings.HasPrefix(segs[0], "#") {
			segs = segs[1:]
		}
		path := formatCUEPath(segs)
		format, args := e.Msg()
		msg := fmt.Sprintf(format, args...)
		node := schemaAt(ws.internal[def], segs)
		field := ""
		if len(segs) > 0 {
			field = segs[len(segs)-1]
		}
		switch {
		case strings.Contains(msg, "errors in empty disjunction"):
			continue
		case strings.Contains(msg, "mismatched types") && strings.Contains(msg, "and null"):
			continue // the "| null" branch of a nullable field
		case strings.Contains(msg, "field not allowed"):
			unknown = append(unknown, segs)
			if def == defActionPatch && len(segs) > 1 && (segs[0] == "email" || segs[0] == "webhook") {
				add(segs[0], "unknown field"+unknownFieldHint(def, segs[:1]))
				continue
			}
			add(path, "unknown field"+unknownFieldHint(def, segs))
		case strings.Contains(msg, "field is required but not present"):
			add(path, "is required"+requiredFieldHint(field))
		case strings.Contains(msg, "incompatible list lengths"):
			add(path, "must not be empty"+requiredFieldHint(field))
		case node != nil && node["enum"] != nil && strings.Contains(msg, "conflicting values"):
			if !enumDone[path] {
				enumDone[path] = true
				add(path, "must be one of: "+ws.describeEnum(node)+enumHint(def, field))
			}
		case strings.Contains(msg, `out of bound =~"\\S"`):
			add(path, "must not be blank")
		case strings.Contains(msg, "strings.MaxRunes"):
			add(path, "is too long"+maxRunesHint(msg))
		case strings.Contains(msg, "strings.MinRunes"):
			add(path, "must not be empty")
		case strings.Contains(msg, "out of bound >="):
			add(path, "must be at least "+strings.TrimSuffix(msg[strings.Index(msg, ">=")+2:], ")"))
		case strings.Contains(msg, "mismatched types float and int"):
			add(path, "must be a whole number")
		case strings.Contains(msg, "mismatched types"):
			t, _ := node["type"].(string)
			if t == "object" && node["properties"] == nil && strings.Contains(msg, "and string") {
				// {[string]: string} with a non-string value: CUE reports the map.
				add(path, "values must be strings")
			} else if t != "" && t != "object" {
				add(path, "must be "+article(t)+" "+t)
			} else {
				add(path, msg)
			}
		default:
			add(path, msg)
		}
	}
	lines := make([]string, 0, len(out))
	for _, p := range out {
		lines = append(lines, strings.TrimPrefix(p.path+": "+p.msg, ": "))
	}
	return lines, unknown
}

// maxRunesHint pulls the limit out of a strings.MaxRunes(N) message.
func maxRunesHint(msg string) string {
	i := strings.Index(msg, "MaxRunes(")
	if i < 0 {
		return ""
	}
	rest := msg[i+len("MaxRunes("):]
	if j := strings.Index(rest, ")"); j > 0 {
		return ": at most " + rest[:j] + " characters"
	}
	return ""
}

func article(t string) string {
	if strings.ContainsRune("aeiou", rune(t[0])) {
		return "an"
	}
	return "a"
}

func formatCUEPath(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		if _, err := strconv.Atoi(s); err == nil {
			b.WriteString("[" + s + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteString(".")
		}
		b.WriteString(s)
	}
	return b.String()
}

// schemaAt walks the internal schema along a CUE error path.
func schemaAt(node map[string]any, segs []string) map[string]any {
	for _, s := range segs {
		if node == nil {
			return nil
		}
		if _, err := strconv.Atoi(s); err == nil {
			node, _ = node["items"].(map[string]any)
			continue
		}
		props, _ := node["properties"].(map[string]any)
		node, _ = props[s].(map[string]any)
	}
	return node
}

func (ws *workflowSchemas) describeEnum(node map[string]any) string {
	kind, _ := node[namesKey].(string)
	var parts []string
	for _, e := range node["enum"].([]any) {
		switch x := e.(type) {
		case float64:
			if label, ok := ws.labels[kind][int(x)]; ok {
				parts = append(parts, fmt.Sprintf("%d (%s)", int(x), label))
			} else {
				parts = append(parts, strconv.Itoa(int(x)))
			}
		default:
			parts = append(parts, fmt.Sprint(x))
		}
	}
	return strings.Join(parts, ", ")
}

func requiredFieldHint(field string) string {
	switch field {
	case "passwords":
		return " for type 5 (password_removal) actions, as non-blank strings"
	case "ai_suggestion_fields":
		return " for type 8 (apply_ai_suggestions) actions: some of title, tags, correspondent, document_type, storage_path, created"
	case "email":
		return " for type 3 (email) actions: subject, body and to"
	case "webhook":
		return " for type 4 (webhook) actions: url"
	case "type":
		return " on every trigger and action"
	}
	return ""
}

func unknownFieldHint(def string, segs []string) string {
	field := segs[len(segs)-1]
	switch {
	case def == defWorkflowCreate && field == "id":
		return " (workflow_create always adds new triggers, actions and email or webhook settings; ids are only for workflow_update)"
	case def == defActionPatch && (field == "email" || field == "webhook"):
		return " (email and webhook settings can only be written through workflow_update)"
	}
	return ""
}

func enumHint(def, field string) string {
	if def == defActionPatch && field == "type" {
		return " (types 3 email, 4 webhook, 7 remote_ocr and 8 apply_ai_suggestions can only be set through workflow_update)"
	}
	return ""
}
