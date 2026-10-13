package aap

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseExtraVarsJSONAndYAML(t *testing.T) {
	want := map[string]any{"count": json.Number("2"), "flag": true, "title": "deployment", "nested": map[string]any{"items": []any{"x", nil, json.Number("1.5")}}}
	for _, input := range []string{`{"count":2,"flag":true,"title":"deployment","nested":{"items":["x",null,1.5]}}`, "count: 2\nflag: true\ntitle: deployment\nnested:\n  items: [x, null, 1.5]"} {
		got, err := ParseExtraVars(input)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, err)
		}
	}
	for _, input := range []string{"", "  ", "{}", "---\n{}\n"} {
		got, err := ParseExtraVars(input)
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
}

func TestExtraVarsPreservesNumericPrecision(t *testing.T) {
	for _, input := range []string{
		`{"integer":123456789012345678901234567890,"decimal":1.234567890123456789}`,
		"integer: 123456789012345678901234567890\ndecimal: 1.234567890123456789",
	} {
		vars, err := ParseExtraVars(input)
		if err != nil || vars["integer"] != json.Number("123456789012345678901234567890") || vars["decimal"] != json.Number("1.234567890123456789") {
			t.Fatal("numeric precision changed", err)
		}
	}
}

func TestParseExtraVarsRejectsUnsafeOrAmbiguousValues(t *testing.T) {
	for _, input := range []string{
		"secret", "null", "[secret]", "secret: [", "a: secret\na: other", `{"a":"secret","a":"other"}`, "a: {x: secret, x: other}",
		"1: secret", "a: {2: secret}", "a: &value secret\nb: *value", "a: !custom secret", "a: .inf", "a: .nan", "a: 2026-10-12", "a: secret\n---\nb: other", "<<: {a: secret}", strings.Repeat("x", LaunchInputBytes+1), "a: " + strings.Repeat("[", 65) + "secret" + strings.Repeat("]", 65),
	} {
		_, err := ParseExtraVars(input)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("input accepted or leaked: %v", err)
		}
	}
	if _, err := ParseExtraVars(`date: "2026-10-12"`); err != nil {
		t.Fatal(err)
	}
}

func bound(n float64) *float64 { return &n }

func TestSurveyValueValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		q       SurveyQuestion
		answer  string
		want    any
		present bool
		bad     bool
	}{
		{"unicode length", SurveyQuestion{Type: "text", Min: bound(1), Max: bound(2)}, "部署", "部署", true, false},
		{"required", SurveyQuestion{Type: "text", Required: true}, "", nil, false, true},
		{"min length", SurveyQuestion{Type: "text", Min: bound(3)}, "hi", nil, false, true},
		{"max length", SurveyQuestion{Type: "text", Max: bound(2)}, "abc", nil, false, true},
		{"optional empty text", SurveyQuestion{Type: "text", Min: bound(0)}, "", "", true, false},
		{"optional omitted text", SurveyQuestion{Type: "textarea", Min: bound(1)}, "", nil, false, false},
		{"optional number", SurveyQuestion{Type: "integer"}, "", nil, false, false},
		{"integer", SurveyQuestion{Type: "integer", Min: bound(1), Max: bound(3)}, "2", int64(2), true, false},
		{"integer type", SurveyQuestion{Type: "integer"}, "2.5", nil, false, true},
		{"integer below", SurveyQuestion{Type: "integer", Min: bound(1)}, "0", nil, false, true},
		{"integer above", SurveyQuestion{Type: "integer", Max: bound(3)}, "4", nil, false, true},
		{"float", SurveyQuestion{Type: "float", Min: bound(1), Max: bound(2)}, "1.5", 1.5, true, false},
		{"nan", SurveyQuestion{Type: "float"}, "NaN", nil, false, true},
		{"infinity", SurveyQuestion{Type: "float"}, "Inf", nil, false, true},
		{"float bounds", SurveyQuestion{Type: "float", Max: bound(1)}, "2", nil, false, true},
		{"saved password", SurveyQuestion{Type: "password", Default: "$encrypted$", Required: true, Min: bound(30)}, "$encrypted$", "$encrypted$", true, false},
		{"fake saved password", SurveyQuestion{Type: "password"}, "$encrypted$", nil, false, true},
		{"password bounds", SurveyQuestion{Type: "password", Min: bound(10)}, "secret", nil, false, true},
		{"optional password", SurveyQuestion{Type: "password"}, "", nil, false, false},
		{"choice", SurveyQuestion{Type: "multiplechoice", Choices: []string{"dev", "prod"}}, "prod", "prod", true, false},
		{"bad choice", SurveyQuestion{Type: "multiplechoice", Choices: []string{"dev"}}, "secret", nil, false, true},
		{"multiselect", SurveyQuestion{Type: "multiselect", Choices: []string{"a", "b"}}, "a\nb", []string{"a", "b"}, true, false},
		{"duplicate selection", SurveyQuestion{Type: "multiselect", Choices: []string{"a"}}, "a\na", nil, false, true},
		{"unknown", SurveyQuestion{Type: "unknown"}, "secret", nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, p, err := surveyValue(tc.q, tc.answer)
			if (err != nil) != tc.bad || err == nil && (!reflect.DeepEqual(v, tc.want) || p != tc.present) {
				t.Fatal(v, p, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("value leaked")
			}
		})
	}
}

func TestLaunchVariablesCombinesAndRestrictsInputs(t *testing.T) {
	r := LaunchRequirements{AskVariables: true, Questions: []SurveyQuestion{{Variable: "name", Type: "text", Required: true}, {Variable: "optional", Type: "integer"}}, VariablesNeeded: []string{"name"}}
	got, err := LaunchVariables(r, []string{"deploy", ""}, "manual: true")
	if err != nil || !reflect.DeepEqual(got, map[string]any{"name": "deploy", "manual": true}) {
		t.Fatal(got, err)
	}
	for _, tc := range []struct {
		r       LaunchRequirements
		answers []string
		manual  string
		reason  string
	}{
		{r, []string{"deploy", ""}, "name: secret", "survey field"},
		{r, []string{"deploy", ""}, "optional: 3", "survey field"},
		{r, []string{"", ""}, "{}", "required"},
		{r, nil, "{}", "Survey changed"},
		{LaunchRequirements{AskVariables: false}, nil, "manual: secret", "Prompt on launch"},
		{LaunchRequirements{AskVariables: true, VariablesNeeded: []string{"secret"}}, nil, "{}", "required launch variable"},
		{r, []string{strings.Repeat("x", LaunchInputBytes), ""}, "{}", "64 KiB"},
	} {
		_, err := LaunchVariables(tc.r, tc.answers, tc.manual)
		if err == nil || !strings.Contains(err.Error(), tc.reason) || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
	r.AskVariables = false
	if _, err := LaunchVariables(r, []string{"deploy", ""}, "{}"); err != nil {
		t.Fatal("survey denied without unrestricted variables", err)
	}
}
