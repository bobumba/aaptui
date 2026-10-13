package aap

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"aaptui/internal/config"
)

// Synthetic fixtures model JobTemplateSerializer at the pinned AWX revision.
func TestTemplateVariablesBothModes(t *testing.T) {
	want := []JobVariable{
		{Name: "blank", Value: `""`}, {Name: "count", Value: "9007199254740993"},
		{Name: "enabled", Value: "false"},
		{Name: "nested", Value: "{\n  \"items\": [\n    0,\n    null,\n    \"[redacted]\"\n  ]\n}"},
		{Name: "notes", Value: "first\nlast\n"}, {Name: "password", Value: `"$encrypted$"`},
		{Name: "[redacted]_key", Value: `"[redacted]"`},
	}
	object := map[string]any{
		"blank": "", "count": json.Number("9007199254740993"), "enabled": false,
		"nested": map[string]any{"items": []any{0, nil, "sec\x1b[31mret"}},
		"notes":  "first\nlast\n", "password": "$encrypted$", "secret\x1b[31m_key": "secret",
	}
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, test := range []struct {
			name string
			vars any
		}{
			{"object", object}, {"json", string(data)},
			{"yaml", "blank: \"\"\ncount: 9007199254740993\nenabled: false\nnested:\n  items: [0, null, secret]\nnotes: |\n  first\n  last\npassword: $encrypted$\nsecret_key: secret\n"},
		} {
			t.Run(string(mode)+"/"+test.name, func(t *testing.T) {
				calls := 0
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != cPrefix(mode)+"job_templates/1/" || r.Method != http.MethodGet {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job_template", "name": "configured template", "extra_vars": test.vars}); err != nil {
						t.Error(err)
					}
				})
				d, err := c.Template(context.Background(), TemplateRef{ID: 1, Type: PlaybookTemplate})
				if err != nil || d.Name != "configured template" || calls != 1 || d.ExtraVars == nil || d.ExtraVars.UnavailableReason != "" || !reflect.DeepEqual(d.ExtraVars.Values, want) {
					t.Fatalf("unexpected template variables: %+v, %v", d.ExtraVars, err)
				}
			})
		}
	}
}

func TestTemplateVariableStatesRetainDetails(t *testing.T) {
	for _, test := range []struct {
		name, field, reason string
	}{
		{"missing", "", "Not returned by the server"},
		{"null", `,"extra_vars":null`, "Not returned by the server"},
		{"empty string", `,"extra_vars":""`, ""},
		{"empty object", `,"extra_vars":{}`, ""},
		{"malformed", `,"extra_vars":"password: [private"`, "Malformed variable data"},
		{"array", `,"extra_vars":[]`, "Malformed variable data"},
		{"duplicate", `,"extra_vars":{"key":1,"key":2}`, "Malformed variable data"},
		{"input budget", `,"extra_vars":{"value":"` + strings.Repeat("x", jobVariablesBytes) + `"}`, "Variable data exceeds 64 KiB"},
		{"display budget", `,"extra_vars":{"value":[` + strings.Repeat("0,", 15000) + `0]}`, "Variable display exceeds 64 KiB"},
		{"sanitized collision", `,"extra_vars":{"secret":1,"[redacted]":2}`, "Variable names are ambiguous after sanitization"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(`{"id":1,"type":"job_template","name":"retained"` + test.field + `}`)); err != nil {
					t.Error(err)
				}
			})
			d, err := c.Template(context.Background(), TemplateRef{ID: 1, Type: PlaybookTemplate})
			if err != nil || d.Name != "retained" || d.ExtraVars == nil || d.ExtraVars.UnavailableReason != test.reason || len(d.ExtraVars.Values) != 0 {
				t.Fatalf("lost details or wrong variable state: %+v, %v", d, err)
			}
		})
	}
	for _, kind := range []TemplateType{WorkflowTemplate, ProjectTemplate, InventoryTemplate, SystemTemplate, ApprovalTemplate, "future_template"} {
		t.Run(string(kind), func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": kind, "extra_vars": map[string]any{"value": "private"}}); err != nil {
					t.Error(err)
				}
			})
			d, err := c.Template(context.Background(), TemplateRef{ID: 1, Type: kind})
			if err != nil || d.ExtraVars != nil {
				t.Fatalf("decoded variables for unreviewed template type: %+v, %v", d.ExtraVars, err)
			}
		})
	}
}
