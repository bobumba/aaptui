package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// This is a synthetic offline integration scenario, not a captured AAP contract.
func TestOfflineIntegrationBothPrefixes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			var mutations atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("authentication missing")
				}
				var response any
				switch r.URL.Path {
				case prefix + "job_templates/":
					response = map[string]any{"count": 1, "results": []any{map[string]any{"id": 3, "type": "job_template", "name": "template"}}}
				case prefix + "job_templates/3/":
					response = map[string]any{"id": 3, "type": "job_template", "name": "template", "playbook": "site.yml"}
				case prefix + "unified_jobs/":
					response = map[string]any{"count": 1, "results": []any{map[string]any{"id": 2, "type": "workflow_job", "name": "parent", "status": "running"}}}
				case prefix + "workflow_jobs/2/":
					response = map[string]any{"id": 2, "type": "workflow_job", "name": "parent", "status": "running", "related": map[string]string{"workflow_nodes": prefix + "workflow_jobs/2/workflow_nodes/"}}
				case prefix + "workflow_jobs/2/workflow_nodes/":
					response = map[string]any{"count": 1, "results": []any{map[string]any{"id": 5, "identifier": "child", "job": 1, "summary_fields": map[string]any{"job": map[string]any{"id": 1, "type": "job", "status": "running"}}}}}
				case prefix + "jobs/1/":
					status := "running"
					if mutations.Load() > 0 {
						status = "canceled"
					}
					response = map[string]any{"id": 1, "type": "job", "name": "child", "status": status, "event_processing_finished": false, "related": map[string]string{"stdout": prefix + "jobs/1/stdout/", "cancel": prefix + "jobs/1/cancel/"}}
				case prefix + "jobs/1/stdout/":
					response = map[string]any{"range": map[string]int{"start": 0, "end": 2, "absolute_end": 2}, "content": "secret\x1b[31mline\x1b[0m\nliteral <html>\n"}
				case prefix + "jobs/1/cancel/":
					if r.Method == http.MethodPost {
						mutations.Add(1)
						w.WriteHeader(202)
						return
					}
					response = map[string]bool{"can_cancel": true}
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
			m := New(context.Background(), string(mode)).WithTemplates(client).WithJobs(client).WithWorkflows(client).WithCancellation(client).WithOutput(func(ctx context.Context, ref aap.JobRef) OutputSession { return client.NewOutputSession(ctx, ref) })
			defer func() {
				if err := m.Close(); err != nil {
					t.Error(err)
				}
			}()
			step := func(k string) {
				t.Helper()
				_, cmd := m.Update(key(k))
				if cmd != nil {
					m.Update(cmd())
				}
				if m.err != nil {
					t.Fatal(m.err)
				}
			}
			step("enter")
			step("enter")
			if m.templateDetail.Playbook == nil || m.templateDetail.Playbook.Playbook != "site.yml" {
				t.Fatal("template details")
			}
			step("esc")
			step("esc")
			m.selected = 1
			step("enter")
			step("enter")
			step("w")
			step("enter")
			step("o")
			if !strings.Contains(m.View().Content, "[redacted]line") || strings.Contains(m.View().Content, "secret") {
				t.Fatal("output presentation", m.View().Content)
			}
			step("esc")
			step("c")
			if mutations.Load() != 0 || !m.confirming {
				t.Fatal("mutation without confirmation")
			}
			step("y")
			if mutations.Load() != 1 || m.jobDetail.Status != "canceled" {
				t.Fatal("reconciliation")
			}
			step("esc")
			if m.screen != workflowScreen || m.jobDetail.Ref.ID != 2 {
				t.Fatal("back stack")
			}
			step("q")
			if mutations.Load() != 1 {
				t.Fatal("quit mutated server")
			}
		})
	}
}

// Exercise real HTTP requests and renderer writes together, rather than only
// supplying an already-positioned tail through a fake OutputSession.
func TestRenderedOutputLoadsOnlyTailUntilPageUp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const total = 1_000_000
	type outputRange struct{ start, end int }
	requests := make(chan outputRange, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch r.URL.Path {
		case "/api/v2/jobs/1/":
			response = map[string]any{"id": 1, "type": "job", "status": "successful", "event_processing_finished": true, "related": map[string]string{"stdout": "/api/v2/jobs/1/stdout/"}}
		case "/api/v2/jobs/1/stdout/":
			start, err := strconv.Atoi(r.URL.Query().Get("start_line"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			end := total
			if start < 0 {
				start = max(0, total+start)
				if r.URL.Query().Has("end_line") {
					t.Error("tail request has an end_line")
				}
			} else {
				end, err = strconv.Atoi(r.URL.Query().Get("end_line"))
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
			if start < total-2*aap.OutputLines || end > total || end < start || end-start > aap.OutputLines {
				t.Error("request walked the log or exceeded the window budget", start, end)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case requests <- outputRange{start, end}:
			default:
				t.Error("unexpected repeated output requests")
			}
			var text strings.Builder
			label := "latest-"
			if r.URL.Query().Has("end_line") {
				label = "history-"
			}
			for i := start; i < end; i++ {
				text.WriteString(label + strconv.Itoa(i) + "\n")
			}
			response = map[string]any{"range": map[string]int{"start": start, "end": end, "absolute_end": total}, "content": text.String()}
		default:
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: config.Direct, TLSVerify: true}, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), "direct").WithOutput(func(ctx context.Context, ref aap.JobRef) OutputSession {
		return client.NewOutputSession(ctx, ref)
	})
	m.screen = jobScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}}, Capabilities: aap.JobCapabilities{Output: aap.Capability{State: aap.Available}}}
	p, waitFor := runTerminal(t, m)
	waitFor(func(frame string) bool { return strings.Contains(frame, "Job details") })
	p.Send(key("o"))
	waitFor(func(frame string) bool { return strings.Contains(frame, "latest-999999") })
	initial := <-requests
	if initial.end != total || initial.start <= 0 || len(requests) != 0 {
		t.Fatal("opening did not fetch just one latest window", initial)
	}
	p.Send(key("pgup"))
	waitFor(func(frame string) bool { return strings.Contains(frame, "history-"+strconv.Itoa(initial.start-1)) })
	history := <-requests
	if history.end != initial.start || history.start >= initial.start || len(requests) != 0 {
		t.Fatal("paging did not load just the preceding window", history)
	}
}
