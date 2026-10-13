package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// Synthetic offline scenario: verify navigation through the real API client.
func TestRelaunchIntegrationBothPrefixes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Direct, config.Gateway} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/v2/"
			if mode == config.Gateway {
				prefix = "/api/controller/v2/"
			}
			var posts atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var response any
				switch r.URL.Path {
				case prefix + "unified_jobs/":
					response = map[string]any{"count": 1, "results": []any{map[string]any{"id": 1, "type": "workflow_job", "name": "source workflow", "status": "successful"}}}
				case prefix + "workflow_jobs/1/":
					response = map[string]any{"id": 1, "type": "workflow_job", "name": "source workflow", "related": map[string]string{"relaunch": prefix + "workflow_jobs/1/relaunch/"}, "summary_fields": map[string]any{"user_capabilities": map[string]bool{"start": true}}}
				case prefix + "workflow_jobs/1/relaunch/":
					response = map[string]any{}
					if r.Method == http.MethodPost {
						posts.Add(1)
						w.WriteHeader(http.StatusCreated)
						response = map[string]any{"id": 2, "type": "workflow_job"}
					}
				case prefix + "workflow_jobs/2/":
					response = map[string]any{"id": 2, "type": "workflow_job", "name": "new workflow", "status": "pending", "related": map[string]string{"workflow_nodes": prefix + "workflow_jobs/2/workflow_nodes/"}}
				case prefix + "workflow_jobs/2/workflow_nodes/":
					response = map[string]any{"count": 0, "results": []any{}}
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
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
			m := New(context.Background(), string(mode)).WithJobs(client).WithRelaunch(client).WithWorkflows(client)
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
			m.selected = 1
			step("enter")
			step("enter")
			step("R")
			if posts.Load() != 0 || !m.confirmRelaunch || !strings.Contains(m.View().Content, "source workflow") {
				t.Fatal("missing source confirmation")
			}
			step("n")
			if posts.Load() != 0 {
				t.Fatal("dismissal mutated server")
			}
			step("R")
			step("y")
			if posts.Load() != 1 || m.jobDetail.Ref.ID != 2 || m.jobDetail.Name != "new workflow" || !strings.Contains(m.View().Content, "Relaunched as job 2") {
				t.Fatal("new job not opened", m.jobDetail)
			}
			step("w")
			if m.screen != workflowScreen {
				t.Fatal("relaunched workflow children unavailable")
			}
			step("esc")
			step("esc")
			if m.jobDetail.Ref.ID != 1 || m.jobDetail.Name != "source workflow" {
				t.Fatal("source context lost")
			}
			step("esc")
			if m.screen != jobsScreen || posts.Load() != 1 {
				t.Fatal("list context lost or duplicate mutation")
			}
		})
	}
}
