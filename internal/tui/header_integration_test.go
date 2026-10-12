package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// Synthetic integration fixtures; real AAP 2.6 acceptance remains deferred.
func TestHeaderInitializationBothModes(t *testing.T) {
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
				body := ""
				switch r.URL.Path {
				case prefix + "me/":
					body = `{"count":1,"results":[{"id":1,"username":"alice\u001b[31msecret\u001b[0m"}]}`
				case prefix + "unified_jobs/":
					if r.URL.Query().Get("status") != "running" || r.URL.Query().Get("page_size") != "1" {
						t.Error("count query missing")
					}
					body = `{"count":237,"results":[{"id":1,"type":"workflow_job","status":"running"}]}`
				default:
					http.NotFound(w, r)
					return
				}
				if _, err := fmt.Fprint(w, body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}, "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			m := New(context.Background(), string(mode)).WithIdentity(client.CleanText(server.URL), client).WithRunningJobs(func(ctx context.Context) RunningJobsPoller { return client.NewRunningJobsPoller(ctx) })
			cleanupModel(t, m)
			// Init batches independent commands. Execute only the initial snapshots,
			// leaving the next poll pending until shutdown cancels its context.
			batch, ok := m.Init()().(tea.BatchMsg)
			if !ok {
				t.Fatal("startup did not batch asynchronous requests")
			}
			for _, cmd := range batch {
				m.Update(cmd())
			}
			view := m.View().Content
			if !strings.Contains(view, server.URL) || !strings.Contains(view, "User: alice[redacted]") || !strings.Contains(view, "Running jobs: 237") || strings.Contains(view, "secret") {
				t.Fatal("header not initialized safely", view)
			}
			m.Update(key("q"))
			if m.nextRunningJobs() != nil {
				t.Fatal("header polling survived quit")
			}
		})
	}
}

func TestFullHeaderOutputLayout(t *testing.T) {
	f := &fakeRunningJobs{}
	m := New(context.Background(), "gateway").WithIdentity("https://aap.example.com", fakeUser{}).WithRunningJobs(func(ctx context.Context) RunningJobsPoller { f.ctx = ctx; return f })
	cleanupModel(t, m)
	m.Update(m.loadUser()())
	m.Update(m.nextRunningJobs()())
	m.screen = outputScreen
	m.following = true
	m.outputUpdate = aap.OutputUpdate{Chunk: aap.OutputChunk{Start: 0, End: 20, AbsoluteEnd: 20, Text: strings.Repeat("line\n", 20)}, CompletionKnown: true}
	for _, size := range [][2]int{{80, 24}, {30, 10}, {1, 1}, {237, 62}} {
		m.width, m.height = size[0], size[1]
		assertFrameSize(t, m)
		l := m.layout()
		if m.width > 30 {
			want := fmt.Sprintf("lines %d–20", max(0, 20-l.bodyHeight))
			if !strings.Contains(m.View().Content, want) {
				t.Fatal("output range no longer matches rows", want)
			}
		}
	}
}
