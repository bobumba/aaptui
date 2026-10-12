package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// This fake API models the pinned source; it is not a captured AAP fixture.
func TestTemplateJobsIntegrationBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing authentication")
				}
				var response any
				switch r.URL.Path {
				case prefix + "job_templates/":
					response = map[string]any{"count": 2, "results": []any{map[string]any{"id": 7, "type": "job_template", "name": "first template"}, map[string]any{"id": 8, "type": "job_template", "name": "empty template"}}}
				case prefix + "job_templates/7/":
					response = map[string]any{"id": 7, "type": "job_template", "name": "first template", "playbook": "site.yml"}
				case prefix + "job_templates/8/":
					response = map[string]any{"id": 8, "type": "job_template", "name": "empty template"}
				case prefix + "job_templates/7/jobs/":
					if r.URL.Query().Get("page") == "2" {
						response = map[string]any{"count": 2, "results": []any{map[string]any{"id": 4, "type": "job", "job_template": 7, "name": "old run", "status": "successful"}}, "previous": prefix + "job_templates/7/jobs/?page=1&order_by=-id"}
					} else {
						response = map[string]any{"count": 2, "results": []any{map[string]any{"id": 5, "type": "job", "job_template": 7, "name": "latest run", "status": "running", "started": "2026-10-12T10:00:00Z"}}, "next": prefix + "job_templates/7/jobs/?page=2&order_by=-id"}
					}
				case prefix + "job_templates/8/jobs/":
					response = map[string]any{"count": 0, "results": []any{}}
				case prefix + "jobs/4/":
					response = map[string]any{"id": 4, "type": "job", "name": "old run", "status": "successful"}
				default:
					http.NotFound(w, r)
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
			m := New(context.Background(), string(mode)).WithTemplates(client).WithTemplateJobs(client).WithJobs(client)
			cleanupModel(t, m)
			step := func(k string) {
				t.Helper()
				_, cmd := m.Update(key(k))
				for cmd != nil {
					_, cmd = m.Update(cmd())
				}
				if m.err != nil {
					t.Fatal(m.err)
				}
			}
			step("enter")
			step("enter")
			if !strings.Contains(m.View().Content, "site.yml") || !strings.Contains(m.View().Content, "latest run") {
				t.Fatal("details/table missing")
			}
			step("n")
			if m.templateJobPage.Items[0].Ref.ID != 4 {
				t.Fatal("next page failed")
			}
			step("enter")
			step("esc")
			if m.screen != templateScreen || m.templateDetail.Ref.ID != 7 || m.templateJobPage.Items[0].Ref.ID != 4 {
				t.Fatal("back lost page")
			}
			step("p")
			if m.templateJobPage.Items[0].Ref.ID != 5 {
				t.Fatal("previous page failed")
			}
			step("r")
			step("esc")
			step("j")
			step("enter")
			if m.templateDetail.Ref.ID != 8 || len(m.templateJobPage.Items) != 0 || strings.Contains(m.View().Content, "latest run") {
				t.Fatal("template switch leaked old jobs")
			}
		})
	}
}
