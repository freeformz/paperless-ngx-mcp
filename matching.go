package main

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// matchAuto is paperless-ngx's MATCH_AUTO: the classifier learns the match
// from documents that already have the object. Tags, correspondents,
// document types and storage paths take it; workflow triggers don't.
const matchAuto = 6

// matchingAlgorithmNames maps each accepted matching_algorithm name to its
// number for the tag, correspondent, document type and storage path tools:
// the workflow names and aliases from schema/workflow.cue plus auto. It is a
// copy, so auto doesn't leak into the workflow tools.
var matchingAlgorithmNames = sync.OnceValue(func() map[string]int {
	names := maps.Clone(mustWorkflowSchemas().names["matching_algorithm"])
	names["auto"] = matchAuto
	names["automatic"] = matchAuto
	return names
})

// matchingAlgorithmLabels lists the canonical names in number order, for
// error messages.
var matchingAlgorithmLabels = sync.OnceValue(func() string {
	labels := mustWorkflowSchemas().labels["matching_algorithm"]
	nums := slices.Sorted(maps.Keys(labels))
	parts := make([]string, 0, len(nums)+1)
	for _, n := range nums {
		parts = append(parts, labels[n])
	}
	return strings.Join(append(parts, "auto"), ", ")
})

const matchingAlgorithmDesc = "Matching algorithm, a name or number: none (0), any (1, any word), all (2), literal (3, exact), regex (4), fuzzy (5), auto (6, learned from your documents)"

// withMatchingAlgorithm defines the matching_algorithm parameter. Like
// withNumber it takes an integer or a string, but without the digits-only
// pattern, so names get through. extra is appended to the description.
func withMatchingAlgorithm(extra string) mcp.ToolOption {
	return mcp.WithAny("matching_algorithm", func(schema map[string]any) {
		schema["type"] = []string{"integer", "string"}
	}, mcp.Description(matchingAlgorithmDesc+extra))
}

// setMatchingAlgorithm copies matching_algorithm into body as a number when
// it is given. Names are resolved; numbers pass through unchecked, leaving
// the range to paperless-ngx.
func setMatchingAlgorithm(body map[string]any, args map[string]any) *mcp.CallToolResult {
	val := args["matching_algorithm"]
	if !argIsSet(val) {
		return nil
	}
	n, err := parseMatchingAlgorithm(val)
	if err != "" {
		return errResult(err)
	}
	body["matching_algorithm"] = n
	return nil
}

func parseMatchingAlgorithm(val any) (int, string) {
	const name = "matching_algorithm"
	switch x := val.(type) {
	case string:
		s := strings.TrimSpace(x)
		// A numeric string is a number, so "-1" isn't read as the name "_1".
		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			n, whole := wholeNumberUpTo(s, maxWholeNumber)
			if !whole {
				return 0, wholeNumberError(name, s, maxWholeNumber)
			}
			return n, ""
		}
		if n, ok := matchingAlgorithmNames()[choiceKey(s)]; ok {
			return n, ""
		}
	case float64, int, int64:
		n, whole := wholeNumberUpTo(x, maxWholeNumber)
		if !whole {
			return 0, wholeNumberError(name, x, maxWholeNumber)
		}
		return n, ""
	}
	got := fmt.Sprint(val)
	if s, ok := val.(string); ok {
		got = strconv.Quote(s)
	}
	return 0, fmt.Sprintf("%s must be a name (%s) or a number, got %s", name, matchingAlgorithmLabels(), got)
}

// choiceKey normalises a choice name: lower case, with runs of spaces and
// hyphens as underscores ("Regular  Expression" and "regular-expression" are
// regular_expression).
func choiceKey(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "-", " ")
	return strings.Join(strings.Fields(s), "_")
}
