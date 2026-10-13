package aap

import (
	"encoding/json"
	"sort"
	"strings"
)

// JobVariable contains only sanitized display text, never raw API values.
type JobVariable struct {
	Name, Value string
}

// JobVariables holds the recorded execution variables. A nil *JobVariables means
// the job type is unsupported; an empty Values slice with no reason means empty.
type JobVariables struct {
	Values            []JobVariable
	UnavailableReason string
}

const jobVariablesBytes = 64 * 1024

func (c *Client) jobVariables(kind JobType, raw json.RawMessage) *JobVariables {
	switch kind {
	case PlaybookJob, WorkflowJob, AdHocCommand, SystemJob:
	default:
		return nil
	}
	unavailable := func(reason string) *JobVariables {
		return &JobVariables{UnavailableReason: reason}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return unavailable("Not returned by the server")
	}
	input := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &input); err != nil {
			return unavailable("Malformed variable data")
		}
	}
	if len(input) > jobVariablesBytes {
		return unavailable("Variable data exceeds 64 KiB")
	}
	vars, err := ParseExtraVars(input)
	if err != nil {
		return unavailable("Malformed variable data")
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	result := &JobVariables{}
	size := 0
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		label := c.cleanLabel(name)
		value, ok := c.cleanVariableValue(vars[name])
		if !ok || label == "" || seen[label] {
			return unavailable("Variable names are ambiguous after sanitization")
		}
		seen[label] = true
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return unavailable("Malformed variable data")
		}
		text := string(data)
		// Actual line breaks keep multiline survey answers readable in the
		// existing detail table; other strings retain quotes, including blanks.
		if s, ok := value.(string); ok && strings.Contains(s, "\n") {
			text = s
		}
		// Redact again after formatting in case escaping assembles the token.
		text = c.CleanText(text)
		size += len(label) + len(text)
		if size > jobVariablesBytes {
			return unavailable("Variable display exceeds 64 KiB")
		}
		result.Values = append(result.Values, JobVariable{Name: label, Value: text})
	}
	return result
}

func (c *Client) cleanVariableValue(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(c.CleanText(v), "\t", "    "), true
	case map[string]any:
		clean := make(map[string]any, len(v))
		for key, child := range v {
			key = c.cleanLabel(key)
			if _, exists := clean[key]; exists || key == "" {
				return nil, false
			}
			var ok bool
			clean[key], ok = c.cleanVariableValue(child)
			if !ok {
				return nil, false
			}
		}
		return clean, true
	case []any:
		clean := make([]any, len(v))
		for i, child := range v {
			var ok bool
			clean[i], ok = c.cleanVariableValue(child)
			if !ok {
				return nil, false
			}
		}
		return clean, true
	default:
		return value, true
	}
}
