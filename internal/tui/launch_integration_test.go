package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// Synthetic fixtures exercise the real client and TUI for both connection modes.
func TestTemplateLaunchIntegrationBothPrefixes(t *testing.T) {
	for _, manual := range []struct {
		name, input string
		release     any
	}{
		{"json", `{"release":"v2"}`, "v2"},
		{"yaml", "release: v2", "v2"},
		{"blank", "", nil},
		{"whitespace", " \n  ", nil},
	} {
		for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
			t.Run(string(mode)+"/"+manual.name, func(t *testing.T) {
				prefix := "/api/controller/v2/"
				if mode == config.Direct {
					prefix = "/api/v2/"
				}
				var posts, checks atomic.Int32
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("missing authentication")
					}
					var response any
					switch r.URL.Path {
					case prefix + "job_templates/1/":
						response = map[string]any{"id": 1, "type": "job_template", "name": "deploy", "related": map[string]any{"launch": prefix + "job_templates/1/launch/", "survey_spec": prefix + "job_templates/1/survey_spec/"}, "summary_fields": map[string]any{"user_capabilities": map[string]any{"start": true}}}
					case prefix + "job_templates/1/survey_spec/":
						response = map[string]any{"spec": []any{map[string]any{"question_name": "Environment", "variable": "environment", "type": "multiplechoice", "required": true, "choices": "dev\nprod", "default": "dev"}, map[string]any{"question_name": "Password", "variable": "password", "type": "password", "required": true, "default": "$encrypted$"}}}
					case prefix + "job_templates/1/launch/":
						if r.Method == http.MethodGet {
							checks.Add(1)
							response = map[string]any{"ask_variables_on_launch": true, "survey_enabled": true, "passwords_needed_to_start": []string{}, "inventory_needed_to_start": false, "credential_needed_to_start": false, "variables_needed_to_start": []string{"environment", "password"}}
						} else {
							posts.Add(1)
							var payload struct {
								Vars map[string]any `json:"extra_vars"`
							}
							if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							if payload.Vars["environment"] != "prod" || payload.Vars["password"] != "$encrypted$" || payload.Vars["release"] != manual.release {
								t.Error("survey/manual payload mismatch")
							}
							response = map[string]any{"id": 9, "type": "job", "job": 9, "ignored_fields": map[string]any{}}
							w.WriteHeader(201)
						}
					case prefix + "jobs/9/":
						response = map[string]any{"id": 9, "type": "job", "name": "deploy", "status": "pending"}
					default:
						t.Errorf("unexpected request %s", r.URL.Path)
						w.WriteHeader(404)
						return
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}, "secret", server.Client())
				if err != nil {
					t.Fatal(err)
				}
				d, err := client.Template(context.Background(), aap.TemplateRef{ID: 1, Type: aap.PlaybookTemplate})
				if err != nil {
					t.Fatal(err)
				}
				m := New(context.Background(), string(mode)).WithTemplates(client).WithJobs(client).WithLaunch(client)
				cleanupModel(t, m)
				m.screen = templateScreen
				m.templateDetail = d
				m.detailTop = 2
				openLaunch(t, m)
				m.Update(launchKey("down"))
				m.Update(launchKey("space"))
				m.Update(launchKey("tab"))
				m.Update(launchKey("tab"))
				m.Update(tea.PasteMsg{Content: manual.input})
				m.Update(launchKey("tab"))
				_, submit := m.Update(key("enter"))
				if submit == nil {
					t.Fatal(m.err)
				}
				_, load := m.Update(submit())
				if load == nil {
					t.Fatal(m.err)
				}
				m.Update(load())
				if posts.Load() != 1 || checks.Load() != 2 || m.screen != jobScreen || m.jobDetail.Ref.ID != 9 || !strings.Contains(m.notice, "Launched job 9") {
					t.Fatal("launch failed", m.err, posts.Load(), checks.Load())
				}
				m.Update(key("esc"))
				if m.screen != templateScreen || m.detailTop != 2 || m.templateDetail.Ref != d.Ref {
					t.Fatal("template context lost")
				}
			})
		}
	}
}
