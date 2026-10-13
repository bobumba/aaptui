package aap

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

const LaunchInputBytes = 64 * 1024

// ParseExtraVars accepts one bounded JSON/YAML mapping. Parser errors deliberately
// exclude the input, which can contain passwords and other sensitive values.
func ParseExtraVars(input string) (map[string]any, error) {
	fail := func(reason string) (map[string]any, error) { return nil, fmt.Errorf("extra_vars: %s", reason) }
	if len(input) > LaunchInputBytes {
		return fail("input exceeds 64 KiB")
	}
	if strings.TrimSpace(input) == "" {
		return map[string]any{}, nil
	}
	dec := yaml.NewDecoder(strings.NewReader(input))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return fail("enter a valid JSON or YAML mapping; check syntax and duplicate keys")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fail("enter a mapping with string variable names")
	}
	if err := validateVariableNode(doc.Content[0], 0); err != nil {
		return fail(err.Error())
	}
	var next yaml.Node
	if err := dec.Decode(&next); err != io.EOF {
		return fail("enter exactly one document")
	}
	vars, err := variableNodeValue(doc.Content[0])
	if err != nil {
		return fail("enter JSON-compatible values")
	}
	// Normalize nested YAML scalars/maps into the exact JSON values sent to AAP.
	data, err := json.Marshal(vars)
	if err != nil {
		return fail("enter JSON-compatible values; numbers must be finite")
	}
	normalized := map[string]any{}
	j := json.NewDecoder(strings.NewReader(string(data)))
	j.UseNumber()
	if err := j.Decode(&normalized); err != nil {
		return fail("enter JSON-compatible values")
	}
	return normalized, nil
}

// Decode numeric scalars with the JSON decoder where possible so large integers
// and precise decimal values are not silently rounded by YAML's float64 decode.
func variableNodeValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.MappingNode:
		values := make(map[string]any, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			value, err := variableNodeValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			values[n.Content[i].Value] = value
		}
		return values, nil
	case yaml.SequenceNode:
		values := make([]any, len(n.Content))
		for i, child := range n.Content {
			value, err := variableNodeValue(child)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	default:
		var value any
		if (n.Tag == "!!int" || n.Tag == "!!float") && json.Valid([]byte(n.Value)) {
			dec := json.NewDecoder(strings.NewReader(n.Value))
			dec.UseNumber()
			if err := dec.Decode(&value); err != nil {
				return nil, err
			}
		} else if err := n.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
}

func validateVariableNode(n *yaml.Node, depth int) error {
	if depth > 64 {
		return fmt.Errorf("nesting exceeds 64 levels")
	}
	switch n.Kind {
	case yaml.MappingNode:
		if n.Tag != "!!map" {
			return fmt.Errorf("custom YAML tags are not supported")
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Value == "" {
				return fmt.Errorf("mapping keys must be nonempty strings; merge keys are not supported")
			}
			if seen[k.Value] {
				return fmt.Errorf("duplicate variable names are not allowed")
			}
			seen[k.Value] = true
			if err := validateVariableNode(n.Content[i+1], depth+1); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		if n.Tag != "!!seq" {
			return fmt.Errorf("custom YAML tags are not supported")
		}
		for _, child := range n.Content {
			if err := validateVariableNode(child, depth+1); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str", "!!int", "!!float", "!!bool", "!!null":
		default:
			return fmt.Errorf("use JSON-compatible scalars; quote dates and custom values")
		}
	default:
		return fmt.Errorf("YAML aliases are not supported")
	}
	return nil
}

// LaunchVariables validates and combines form values, never including values in
// errors. Survey keys are reserved for their controls rather than silently merged.
func LaunchVariables(r LaunchRequirements, answers []string, manual string) (map[string]any, error) {
	vars, err := ParseExtraVars(manual)
	if err != nil {
		return nil, err
	}
	if len(answers) != len(r.Questions) {
		return nil, fmt.Errorf("Survey changed; close and reopen the launch form")
	}
	size := len(manual)
	for i, q := range r.Questions {
		if _, exists := vars[q.Variable]; exists {
			return nil, fmt.Errorf("Survey question %d: define this variable in its survey field, not extra_vars", i+1)
		}
	}
	if len(vars) > 0 && !r.AskVariables {
		return nil, fmt.Errorf("extra_vars: manual variables require Prompt on launch; use the survey fields or enable that setting")
	}
	for i, q := range r.Questions {
		size += len(answers[i])
		if size > LaunchInputBytes {
			return nil, fmt.Errorf("Launch input exceeds 64 KiB")
		}
		value, present, err := surveyValue(q, answers[i])
		if err != nil {
			return nil, fmt.Errorf("Survey question %d: %w", i+1, err)
		}
		if present {
			vars[q.Variable] = value
		}
	}
	for _, variable := range r.VariablesNeeded {
		if _, exists := vars[variable]; !exists {
			return nil, fmt.Errorf("A required launch variable is missing; complete the survey or provide it in extra_vars")
		}
	}
	return vars, nil
}
