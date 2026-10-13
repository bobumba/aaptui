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

// Synthetic responses model the pinned serializers documented in API_CONTRACT.md.
func TestJobVariablesBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, kind := range []JobType{PlaybookJob, WorkflowJob, AdHocCommand, SystemJob} {
			for _, object := range []bool{false, true} {
				t.Run(string(mode)+"/"+string(kind)+"/"+map[bool]string{false: "string", true: "object"}[object], func(t *testing.T) {
					vars := map[string]any{
						"survey_answer": "prod", "password": "$encrypted$", "blank": "",
						"enabled": false, "count": json.Number("9007199254740993"), "nothing": nil,
						"notes": "first\n\nlast\tline", "secret\x1b[31m_key": "secret\x1b]0;private-title\a visible",
						"nested": map[string]any{"secret\x1b[31m_key": []any{"sec\x1b[31mret", true, 0, map[string]any{"value": "secret"}}},
					}
					var extra any = vars
					if !object {
						data, err := json.Marshal(vars)
						if err != nil {
							t.Fatal(err)
						}
						extra = string(data)
					}
					c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != cPrefix(mode)+jobCollection(kind)+"/1/" {
							t.Error(r.URL)
						}
						if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": kind, "name": "recorded run", "status": "successful", "extra_vars": extra}); err != nil {
							t.Error(err)
						}
					})
					d, err := c.Job(context.Background(), JobRef{ID: 1, Type: kind})
					if err != nil {
						t.Fatal(err)
					}
					want := []JobVariable{
						{Name: "blank", Value: `""`}, {Name: "count", Value: "9007199254740993"},
						{Name: "enabled", Value: "false"},
						{Name: "nested", Value: "{\n  \"[redacted]_key\": [\n    \"[redacted]\",\n    true,\n    0,\n    {\n      \"value\": \"[redacted]\"\n    }\n  ]\n}"},
						{Name: "notes", Value: "first\n\nlast    line"}, {Name: "nothing", Value: "null"},
						{Name: "password", Value: `"$encrypted$"`}, {Name: "[redacted]_key", Value: `"[redacted] visible"`},
						{Name: "survey_answer", Value: `"prod"`},
					}
					if d.ExtraVars == nil || d.ExtraVars.UnavailableReason != "" || !reflect.DeepEqual(d.ExtraVars.Values, want) {
						t.Fatalf("unexpected recorded variables: %+v", d.ExtraVars)
					}
				})
			}
		}
	}
}

func TestJobVariableStatesRetainDetails(t *testing.T) {
	for _, test := range []struct {
		name, field, reason string
	}{
		{"missing", "", "Not returned by the server"},
		{"null", `,"extra_vars":null`, "Not returned by the server"},
		{"empty string", `,"extra_vars":""`, ""},
		{"empty mapping string", `,"extra_vars":"{}"`, ""},
		{"empty object", `,"extra_vars":{}`, ""},
		{"malformed string", `,"extra_vars":"password: [private"`, "Malformed variable data"},
		{"scalar", `,"extra_vars":42`, "Malformed variable data"},
		{"array", `,"extra_vars":[]`, "Malformed variable data"},
		{"duplicate", `,"extra_vars":{"key":1,"key":2}`, "Malformed variable data"},
		{"yaml", `,"extra_vars":"environment: prod\nnotes: |\n  first\n  last\n"`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(`{"id":1,"type":"job","name":"retained","status":"successful"` + test.field + `}`)); err != nil {
					t.Error(err)
				}
			})
			d, err := c.Job(context.Background(), JobRef{ID: 1, Type: PlaybookJob})
			if err != nil || d.Name != "retained" || d.ExtraVars == nil || d.ExtraVars.UnavailableReason != test.reason {
				t.Fatalf("lost details or wrong variable state: %+v, %v", d, err)
			}
			if test.name == "yaml" && !reflect.DeepEqual(d.ExtraVars.Values, []JobVariable{{Name: "environment", Value: `"prod"`}, {Name: "notes", Value: "first\nlast\n"}}) {
				t.Fatal(d.ExtraVars.Values)
			}
		})
	}
}

func TestJobVariableLimitsAndSanitization(t *testing.T) {
	c := &Client{token: "secret"}
	for _, test := range []struct {
		name, input, reason string
	}{
		{"input budget", `{"value":"` + strings.Repeat("x", jobVariablesBytes) + `"}`, "Variable data exceeds 64 KiB"},
		{"display budget", `{"value":[` + strings.Repeat("0,", 15000) + `0]}`, "Variable display exceeds 64 KiB"},
		{"deep nesting", `{"value":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}", "Malformed variable data"},
		{"alias", "value: &value [0]\nother: *value", "Malformed variable data"},
		{"tag", "value: !private password", "Malformed variable data"},
		{"top collision", `{"secret":1,"[redacted]":2}`, "Variable names are ambiguous after sanitization"},
		{"nested collision", `{"value":{"secret":1,"[redacted]":2}}`, "Variable names are ambiguous after sanitization"},
		{"empty label", `{"\u001b[31m":1}`, "Variable names are ambiguous after sanitization"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.input)
			if err != nil {
				t.Fatal(err)
			}
			got := c.jobVariables(PlaybookJob, raw)
			if got.UnavailableReason != test.reason || len(got.Values) != 0 {
				t.Fatalf("wrong safe failure: reason %q, %d values", got.UnavailableReason, len(got.Values))
			}
		})
	}
	// JSON-escaped tokens must be redacted before formatting nested values.
	c.token = `a"b\c`
	raw, err := json.Marshal(map[string]any{"value": map[string]any{"key": c.token}})
	if err != nil {
		t.Fatal(err)
	}
	got := c.jobVariables(WorkflowJob, raw)
	if len(got.Values) != 1 || got.Values[0].Value != "{\n  \"key\": \"[redacted]\"\n}" {
		t.Fatalf("escaped token was not redacted: %+v", got)
	}
	for _, kind := range []JobType{ProjectUpdate, InventoryUpdate, WorkflowApproval, "future_job"} {
		if got := c.jobVariables(kind, json.RawMessage(`{"value":"private"}`)); got != nil {
			t.Fatalf("decoded unreviewed type %s", kind)
		}
	}
}
