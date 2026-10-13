package aap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"aaptui/internal/config"
)

// These fixtures model the pinned upstream contract, not captured AAP responses.
func launchRequirementsFixture(survey bool) map[string]any {
	return map[string]any{"ask_variables_on_launch": true, "survey_enabled": survey, "passwords_needed_to_start": []string{}, "inventory_needed_to_start": false, "credential_needed_to_start": false, "variables_needed_to_start": []string{}}
}
func launchTemplateFixture(prefix string) map[string]any {
	return map[string]any{"id": 1, "type": "job_template", "name": "secret\x1b[31m deploy", "related": map[string]string{"launch": prefix + "job_templates/1/launch/", "survey_spec": prefix + "job_templates/1/survey_spec/"}, "summary_fields": map[string]any{"user_capabilities": map[string]any{"start": true}}}
}
func surveyFixture() map[string]any {
	return map[string]any{"spec": []any{
		map[string]any{"question_name": "Title", "variable": "title", "type": "text", "required": true, "default": "部署", "min": 1, "max": 3},
		map[string]any{"question_name": "Notes", "variable": "notes", "type": "textarea", "required": false, "default": "first\nsecond", "min": 0, "max": 50},
		map[string]any{"question_name": "Password", "variable": "password", "type": "password", "required": true, "default": "$encrypted$", "min": 8},
		map[string]any{"question_name": "Count", "variable": "count", "type": "integer", "required": true, "default": 2, "min": 1, "max": 3},
		map[string]any{"question_name": "Rate", "variable": "rate", "type": "float", "required": false, "default": 1.5, "min": 0, "max": 2},
		map[string]any{"question_name": "Environment", "variable": "env", "type": "multiplechoice", "required": true, "default": "dev", "choices": "dev\nprod"},
		map[string]any{"question_name": "Targets", "variable": "targets", "type": "multiselect", "required": false, "default": []string{"a"}, "choices": []string{"a", "b"}},
	}}
}
func writeLaunchJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}

func TestLaunchBothPrefixesAndCreatedTypes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Direct, config.Gateway} {
		for _, kind := range []JobType{PlaybookJob, WorkflowJob} {
			t.Run(string(mode)+"/"+string(kind), func(t *testing.T) {
				prefix := "/api/v2/"
				if mode == config.Gateway {
					prefix = "/api/controller/v2/"
				}
				var posts, reads atomic.Int32
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("authentication missing")
					}
					switch r.URL.Path {
					case prefix + "job_templates/1/":
						writeLaunchJSON(t, w, launchTemplateFixture(prefix))
					case prefix + "job_templates/1/survey_spec/":
						writeLaunchJSON(t, w, surveyFixture())
					case prefix + "job_templates/1/launch/":
						if r.Method == http.MethodGet {
							reads.Add(1)
							writeLaunchJSON(t, w, launchRequirementsFixture(true))
							return
						}
						posts.Add(1)
						var body struct {
							Extra map[string]any `json:"extra_vars"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						want := map[string]any{"title": "部署", "notes": "first\nsecond", "password": "$encrypted$", "count": float64(2), "rate": 1.5, "env": "dev", "targets": []any{"a"}, "manual": true}
						if !reflect.DeepEqual(body.Extra, want) || r.Header.Get("Content-Type") != "application/json" {
							t.Errorf("payload mismatch")
						}
						response := map[string]any{"id": 9, "type": kind, "ignored_fields": map[string]any{}}
						if kind == PlaybookJob {
							response["job"] = 9
						} else {
							response["workflow_job"] = 9
						}
						w.Header().Set("Location", "https://evil.example/api/v2/jobs/10/")
						w.WriteHeader(201)
						writeLaunchJSON(t, w, response)
					default:
						t.Errorf("unexpected route %s", r.URL.Path)
						w.WriteHeader(404)
					}
				})
				req, err := c.PrepareLaunch(context.Background(), TemplateRef{1, PlaybookTemplate})
				if err != nil || len(req.Questions) != 7 || req.Template.Name != "[redacted] deploy" {
					t.Fatal(req, err)
				}
				answers := make([]string, len(req.Questions))
				for i, q := range req.Questions {
					answers[i] = q.Default
				}
				result, err := c.Launch(context.Background(), req, answers, "manual: true")
				if err != nil || !result.Requested || result.Ambiguous || result.Job.Ref != (JobRef{9, kind}) || posts.Load() != 1 || reads.Load() != 2 {
					t.Fatal(result, err, posts.Load(), reads.Load())
				}
			})
		}
	}
}

func TestPrepareLaunchRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		value any
		kind  ErrorKind
	}{
		{"password", "passwords_needed_to_start", []string{"secret"}, Unsupported},
		{"inventory", "inventory_needed_to_start", true, Unsupported},
		{"credentials", "credential_needed_to_start", true, Unsupported},
		{"missing prompt", "ask_variables_on_launch", nil, Malformed},
		{"malformed survey flag", "survey_enabled", "secret", Malformed},
		{"missing passwords", "passwords_needed_to_start", nil, Malformed},
		{"missing inventory", "inventory_needed_to_start", nil, Malformed},
		{"missing variables", "variables_needed_to_start", nil, Malformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("unexpected mutation")
				}
				if strings.HasSuffix(r.URL.Path, "launch/") {
					v := launchRequirementsFixture(false)
					v[tc.field] = tc.value
					writeLaunchJSON(t, w, v)
				} else {
					writeLaunchJSON(t, w, launchTemplateFixture("/api/v2/"))
				}
			})
			_, err := c.PrepareLaunch(context.Background(), TemplateRef{1, PlaybookTemplate})
			var api *APIError
			if !errors.As(err, &api) || api.Kind != tc.kind || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
}

func TestLaunchCapabilitiesAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		permission any
		link       string
		state      CapabilityState
	}{
		{"available", true, "/api/v2/job_templates/1/launch/", Available},
		{"denied", false, "/api/v2/job_templates/1/launch/", Unavailable},
		{"unknown", nil, "/api/v2/job_templates/1/launch/", Unknown},
		{"missing link", true, "", Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				v := launchTemplateFixture("/api/v2/")
				v["related"] = map[string]string{"launch": tc.link}
				v["summary_fields"] = map[string]any{"user_capabilities": map[string]any{"start": tc.permission}}
				writeLaunchJSON(t, w, v)
			})
			d, err := c.Template(context.Background(), TemplateRef{1, PlaybookTemplate})
			if err != nil || d.Launch.State != tc.state {
				t.Fatal(d.Launch, err)
			}
			if tc.state != Available {
				if _, err := c.PrepareLaunch(context.Background(), d.Ref); err == nil {
					t.Fatal("unavailable launch allowed")
				}
			}
		})
	}
	for _, link := range []string{"https://evil.example/api/v2/job_templates/1/launch/", "/api/v2/job_templates/2/launch/", "/api/controller/v2/job_templates/1/launch/", "/api/v2/job_templates/1/launch/?", "/api/v2/job_templates/1/launch/?x=1", "/api/v2/job_templates/1/launch/#bad"} {
		t.Run(link, func(t *testing.T) {
			var calls atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				v := launchTemplateFixture("/api/v2/")
				v["related"] = map[string]string{"launch": link}
				writeLaunchJSON(t, w, v)
			})
			if _, err := c.PrepareLaunch(context.Background(), TemplateRef{1, PlaybookTemplate}); err == nil || calls.Load() != 1 {
				t.Fatal("invalid link followed", err, calls.Load())
			}
		})
	}
	for _, kind := range []TemplateType{WorkflowTemplate, ProjectTemplate, InventoryTemplate, SystemTemplate, ApprovalTemplate, "future_template"} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported type requested") })
		if _, err := c.PrepareLaunch(context.Background(), TemplateRef{1, kind}); err == nil {
			t.Fatal("unsupported launch allowed")
		}
	}
}

func TestSurveyMalformedSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec any
	}{
		{"missing", nil}, {"empty", []any{}}, {"unknown", []any{map[string]any{"variable": "a", "type": "future", "required": true}}},
		{"missing required", []any{map[string]any{"variable": "a", "type": "text"}}},
		{"duplicate", []any{map[string]any{"variable": "a", "type": "text", "required": false}, map[string]any{"variable": "a", "type": "text", "required": true}}},
		{"bounds", []any{map[string]any{"variable": "a", "type": "integer", "required": true, "min": 3, "max": 2}}},
		{"bad bounds", []any{map[string]any{"variable": "a", "type": "text", "required": true, "min": "secret"}}},
		{"bad length", []any{map[string]any{"variable": "a", "type": "text", "required": true, "min": 0.5}}},
		{"no choices", []any{map[string]any{"variable": "a", "type": "multiplechoice", "required": true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "survey_spec/") {
					writeLaunchJSON(t, w, map[string]any{"spec": tc.spec})
				} else if strings.HasSuffix(r.URL.Path, "launch/") {
					writeLaunchJSON(t, w, launchRequirementsFixture(true))
				} else {
					writeLaunchJSON(t, w, launchTemplateFixture("/api/v2/"))
				}
			})
			if _, err := c.PrepareLaunch(context.Background(), TemplateRef{1, PlaybookTemplate}); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
}

func TestLaunchPostOutcomesAndNoRetry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		body      string
		ambiguous bool
		ignored   bool
	}{
		{"success", 201, `{"id":9,"type":"job","job":9,"ignored_fields":{}}`, false, false},
		{"ignored", 201, `{"id":9,"type":"job","ignored_fields":{"extra_vars":{"secret":"secret"}}}`, false, true},
		{"denied", 403, `secret`, false, false}, {"rejected", 400, `secret`, false, false}, {"authentication", 401, `secret`, false, false},
		{"missing", 404, `secret`, false, false}, {"temporary", 503, `secret`, true, false}, {"rate limit", 429, `secret`, true, false},
		{"malformed", 201, `secret`, true, false}, {"no identity", 201, `{}`, true, false},
		{"wrong type", 201, `{"id":9,"type":"project_update"}`, true, false}, {"mismatch", 201, `{"id":9,"type":"job","job":8}`, true, false},
		{"wrong legacy kind", 201, `{"id":9,"type":"workflow_job","job":9}`, true, false}, {"negative", 201, `{"id":-1,"type":"job"}`, true, false},
		{"lost transport", 0, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					if tc.code == 0 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						if err := conn.Close(); err != nil {
							t.Error(err)
						}
						return
					}
					w.WriteHeader(tc.code)
					if _, err := w.Write([]byte(tc.body)); err != nil {
						t.Error(err)
					}
					return
				}
				if strings.HasSuffix(r.URL.Path, "launch/") {
					writeLaunchJSON(t, w, launchRequirementsFixture(false))
				} else {
					writeLaunchJSON(t, w, launchTemplateFixture("/api/v2/"))
				}
			})
			req, err := c.PrepareLaunch(context.Background(), TemplateRef{1, PlaybookTemplate})
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.Launch(context.Background(), req, nil, "{}")
			success := tc.name == "success" || tc.name == "ignored"
			if (err == nil) != success || result.Ambiguous != tc.ambiguous || result.IgnoredFields != tc.ignored || posts.Load() != 1 {
				t.Fatal(result, err, posts.Load())
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("secret exposed")
			}
		})
	}
}

func TestLaunchRechecksSchemaPermissionsAndPrompt(t *testing.T) {
	for _, change := range []string{"schema", "permission", "prompt", "password"} {
		t.Run(change, func(t *testing.T) {
			var reads, posts atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					w.WriteHeader(500)
					return
				}
				if strings.HasSuffix(r.URL.Path, "survey_spec/") {
					v := surveyFixture()
					if change == "schema" && reads.Load() > 1 {
						v["spec"].([]any)[0].(map[string]any)["variable"] = "changed"
					}
					writeLaunchJSON(t, w, v)
				} else if strings.HasSuffix(r.URL.Path, "launch/") {
					v := launchRequirementsFixture(true)
					if reads.Load() > 1 {
						if change == "prompt" {
							v["ask_variables_on_launch"] = false
						}
						if change == "password" {
							v["passwords_needed_to_start"] = []string{"secret"}
						}
					}
					writeLaunchJSON(t, w, v)
				} else {
					reads.Add(1)
					v := launchTemplateFixture("/api/v2/")
					if change == "permission" && reads.Load() > 1 {
						v["summary_fields"] = map[string]any{"user_capabilities": map[string]any{"start": false}}
					}
					writeLaunchJSON(t, w, v)
				}
			})
			req, err := c.PrepareLaunch(context.Background(), TemplateRef{1, PlaybookTemplate})
			if err != nil {
				t.Fatal(err)
			}
			answers := make([]string, len(req.Questions))
			for i, q := range req.Questions {
				answers[i] = q.Default
			}
			if _, err := c.Launch(context.Background(), req, answers, "manual: true"); err == nil || posts.Load() != 0 {
				t.Fatal("changed requirements ignored", err, posts.Load())
			}
		})
	}
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled request reached server") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.PrepareLaunch(ctx, TemplateRef{1, PlaybookTemplate}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
