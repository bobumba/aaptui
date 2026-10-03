package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

type fakeProjects struct{ fakeTemplates }

func (f *fakeProjects) ListProjects(ctx context.Context, o aap.ListOptions) (aap.Page[aap.TemplateSummary], error) {
	page, err := f.ListTemplates(ctx, o)
	for i := range page.Items {
		page.Items[i].Ref.Type = aap.ProjectTemplate
	}
	return page, err
}

func TestProjectNavigation(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			type request struct{ path, search, page string }
			requests := make(chan request, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- request{r.URL.Path, r.URL.Query().Get("search"), r.URL.Query().Get("page")}
				var response any
				switch r.URL.Path {
				case prefix + "projects/":
					if r.URL.Query().Get("page") == "2" {
						response = map[string]any{"count": 3, "results": []any{map[string]any{"id": 9, "type": "project", "name": "last project"}}, "previous": prefix + "projects/?search=q"}
					} else {
						response = map[string]any{"count": 3, "results": []any{
							map[string]any{"id": 7, "type": "project", "name": "first project", "status": "successful"},
							map[string]any{"id": 8, "type": "project", "name": "second project", "status": "failed"},
						}, "next": prefix + "projects/?page=2&search=q"}
					}
				case prefix + "projects/8/":
					response = map[string]any{"id": 8, "type": "project", "name": "second project", "description": "repository", "status": "failed", "scm_type": "git", "scm_url": "https://example.com/repo.git", "scm_branch": "main"}
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
			m := New(context.Background(), string(mode)).WithProjects(client)
			cleanupModel(t, m)
			load := func(k, path, search, page string) {
				t.Helper()
				_, cmd := m.Update(key(k))
				if cmd == nil || !m.loading {
					t.Fatalf("%q did not start a request", k)
				}
				m.Update(cmd())
				if m.err != nil {
					t.Fatal(m.err)
				}
				if got, want := <-requests, (request{prefix + path, search, page}); got != want {
					t.Fatalf("request = %+v, want %+v", got, want)
				}
			}
			if !strings.Contains(m.View().Content, "Projects") {
				t.Fatal("Projects missing from main menu")
			}
			m.Update(key("j"))
			m.Update(key("j"))
			if m.selected != 2 {
				t.Fatal("Projects is not the third main menu entry")
			}
			// Opening Projects must discard any cached Job Templates list.
			m.templatePage.Items = []aap.TemplateSummary{{Name: "old template"}}
			load("enter", "projects/", "", "")
			if m.screen != projectsScreen || strings.Contains(m.View().Content, "old template") {
				t.Fatal("project list", m.View().Content)
			}
			m.Update(key("/"))
			m.Update(key("q"))
			if m.quitting || m.search != "q" {
				t.Fatal("search input")
			}
			load("enter", "projects/", "q", "")
			m.Update(key("j"))
			m.Update(key("j"))
			if m.selected != 1 || !strings.Contains(ansi.Strip(m.View().Content), "failed") {
				t.Fatal("project selection/status")
			}
			load("enter", "projects/8/", "", "")
			for _, text := range []string{"Project details", "repository", "git", "https://example.com/repo.git", "main"} {
				if !strings.Contains(ansi.Strip(m.View().Content), text) {
					t.Fatalf("project details missing %q", text)
				}
			}
			m.Update(key("esc"))
			if m.screen != projectsScreen || m.search != "q" || m.selected != 1 || len(m.templatePage.Items) != 2 {
				t.Fatal("project list state not restored")
			}
			load("n", "projects/", "q", "2")
			if m.selected != 0 || m.templatePage.Items[0].Ref.ID != 9 {
				t.Fatal("next page")
			}
			load("p", "projects/", "q", "")
			load("r", "projects/", "q", "")
			m.Update(key("esc"))
			if m.screen != mainScreen || m.selected != 2 {
				t.Fatal("back to main")
			}
		})
	}
}

func TestProjectLoadingErrorAndStaleResults(t *testing.T) {
	for _, stale := range []bool{false, true} {
		m := New(context.Background(), "gateway").WithProjects(&fakeProjects{})
		cleanupModel(t, m)
		m.Update(key("j"))
		m.Update(key("j"))
		m.templatePage.Items = []aap.TemplateSummary{{Name: "old template"}}
		_, cmd := m.Update(key("enter"))
		if !strings.Contains(m.View().Content, "Loading projects") || strings.Contains(m.View().Content, "old template") {
			t.Fatal("project loading state", m.View().Content)
		}
		if stale {
			m.Update(key("esc"))
			m.Update(cmd())
			if m.screen != mainScreen || m.loading || m.err != nil || m.templatePage.Items[0].Name != "old template" {
				t.Fatal("stale projects replaced restored state")
			}
		} else {
			m.Update(resultMsg{id: m.generation, err: &aap.APIError{Kind: aap.Permission, Operation: "read page"}})
			if m.loading || !strings.Contains(m.View().Content, "Unable to load projects.") || !strings.Contains(m.View().Content, "permission") {
				t.Fatal("project error state", m.View().Content)
			}
		}
	}
}
