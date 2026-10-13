package aap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// JobVariable contains only sanitized display text, never raw API values.
type JobVariable struct {
	Name, Value string
}

// JobVariables holds sanitized job or job-template variables for display. A nil
// pointer means unsupported; an empty Values slice with no reason means empty.
type JobVariables struct {
	Values            []JobVariable
	UnavailableReason string
	// YAML contains the sanitized recorded mapping for job detail rendering.
	YAML string
}

const jobVariablesBytes = 64 * 1024

func (c *Client) jobVariables(kind JobType, raw json.RawMessage) *JobVariables {
	switch kind {
	case PlaybookJob, WorkflowJob, AdHocCommand, SystemJob:
	default:
		return nil
	}
	return c.variableMapping(raw, true)
}

func (c *Client) variableMapping(raw json.RawMessage, asYAML bool) *JobVariables {
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
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
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
		if asYAML {
			node, err := variableYAMLNode(value)
			if err != nil {
				return unavailable("Malformed variable data")
			}
			mapping.Content = append(mapping.Content, variableYAMLKey(label), node)
		}
	}
	if asYAML && len(mapping.Content) > 0 {
		var text strings.Builder
		encoder := yaml.NewEncoder(&text)
		encoder.SetIndent(2)
		if err := encoder.Encode(mapping); err != nil {
			return unavailable("Malformed variable data")
		}
		if err := encoder.Close(); err != nil {
			return unavailable("Malformed variable data")
		}
		result.YAML = c.CleanText(text.String())
		if len(result.YAML) > jobVariablesBytes {
			return unavailable("Variable display exceeds 64 KiB")
		}
	}
	return result
}

// Explicit numeric nodes preserve json.Number's exact text instead of allowing
// the YAML encoder to treat it as a string or round it through float64.
func variableYAMLNode(value any) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.ScalarNode}
	switch v := value.(type) {
	case json.Number:
		node.Tag, node.Value = "!!int", string(v)
		if strings.ContainsAny(string(v), ".eE") {
			node.Tag = "!!float"
		}
	case map[string]any:
		node.Kind, node.Tag = yaml.MappingNode, "!!map"
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child, err := variableYAMLNode(v[name])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, variableYAMLKey(name), child)
		}
	case []any:
		node.Kind, node.Tag = yaml.SequenceNode, "!!seq"
		for _, value := range v {
			child, err := variableYAMLNode(value)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
	case string:
		node.Tag, node.Value, node.Style = "!!str", v, yaml.DoubleQuotedStyle
		// Quoting newline-only strings avoids losing blank lines in the emitter.
		if strings.Contains(v, "\n") && strings.Trim(v, "\n") != "" {
			node.Style = yaml.LiteralStyle
		}
	case bool:
		node.Tag, node.Value = "!!bool", "false"
		if v {
			node.Value = "true"
		}
	case nil:
		node.Tag, node.Value = "!!null", "null"
	default:
		return nil, fmt.Errorf("unsupported variable value type")
	}
	return node, nil
}

func variableYAMLKey(name string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
	// A plain << key would be interpreted as YAML merge syntax on rereading.
	if name == "<<" {
		node.Style = yaml.DoubleQuotedStyle
	}
	return node
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
