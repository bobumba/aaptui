package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// Synthetic API exercises the real event session, both API prefixes, and the view.
func TestTaskOutputIntegrationBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Direct, config.Gateway} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/v2/"
			if mode == config.Gateway {
				prefix = "/api/controller/v2/"
			}
			var denied atomic.Bool
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer secret" || r.Method != http.MethodGet {
					t.Error("wrong authentication or method")
				}
				var response any
				switch r.URL.Path {
				case prefix + "jobs/7/":
					response = map[string]any{"id": 7, "type": "job", "status": "successful", "event_processing_finished": true}
				case prefix + "jobs/7/job_events/":
					if denied.Load() {
						w.WriteHeader(403)
						return
					}
					changed := r.URL.Query().Get("changed") == "true"
					failed := r.URL.Query().Get("failed") == "true"
					if changed == failed || r.URL.Query().Get("limit") != "9" || r.URL.Query().Get("no_truncate") != "1" {
						t.Error("missing task filters", r.URL)
					}
					text := "changed secret\x1b[31m host\ndetail line\n"
					if failed {
						text = "failed secret\x1b[31m host\nerror detail\n"
					}
					response = map[string]any{"results": []any{map[string]any{"counter": 1234567, "event": "runner_on_ok", "changed": changed, "failed": failed, "task": "Deploy", "host_name": "web-1", "stdout": text}}}
				default:
					t.Error("unexpected request", r.URL)
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
			m := New(context.Background(), "").WithOutput(func(context.Context, aap.JobRef) OutputSession {
				return &fakeOutput{latest: &aap.OutputUpdate{Chunk: aap.OutputChunk{Text: "all output\n"}, Complete: true}}
			}).WithTaskOutput(func(ctx context.Context, ref aap.JobRef, filter aap.OutputFilter) TaskOutputSession {
				return client.NewTaskOutputSession(ctx, ref, filter)
			})
			cleanupModel(t, m)
			m.screen = jobScreen
			m.width, m.height = 100, 24
			m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 7, Type: aap.PlaybookJob}}, Capabilities: aap.JobCapabilities{Output: aap.Capability{State: aap.Available}}}
			press := func(k string) {
				t.Helper()
				_, cmd := m.Update(key(k))
				deliverOutputCommand(t, m, cmd)
				assertFrameSize(t, m)
			}
			press("o")
			press("C")
			text := ansi.Strip(m.View().Content)
			if m.err != nil || !strings.Contains(text, "changed [redacted] host") || !strings.Contains(text, "detail line") || !strings.Contains(text, "TASK [Deploy]") || strings.Contains(text, "secret") {
				t.Fatal("Changed output incorrect", text, m.err)
			}
			press("F")
			text = ansi.Strip(m.View().Content)
			if m.err != nil || !strings.Contains(text, "failed [redacted] host") || strings.Contains(text, "changed [redacted]") {
				t.Fatal("Failed output incorrect", text, m.err)
			}
			if requests.Load() != 4 || len(m.sessions) != 1 {
				t.Fatal("unexpected requests or leaked sessions", requests.Load(), len(m.sessions))
			}
			press("A")
			denied.Store(true)
			press("F")
			if m.err == nil || strings.Contains(ansi.Strip(m.View().Content), "No matching tasks") {
				t.Fatal("denied metadata shown as empty")
			}
			press("A")
			if m.err != nil || !strings.Contains(ansi.Strip(m.View().Content), "all output") {
				t.Fatal("All inaccessible after metadata denial")
			}
			press("esc")
			if len(m.sessions) != 0 || m.screen != jobScreen {
				t.Fatal("output lifecycle leaked sessions")
			}
		})
	}
}
