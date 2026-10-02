package aap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"aaptui/internal/config"
)

func TestProjectPagination(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != prefix+"projects/" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("unexpected project request: %s", r.URL.Path)
				}
				q := r.URL.Query()
				if q.Get("search") != "demo project" || q.Get("page_size") != "5" {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				response := map[string]any{
					"count":   1,
					"results": []any{map[string]any{"id": 7, "type": "project", "name": "secret\x1b[31mdemo", "status": "successful"}},
					"next":    prefix + "projects/?page=2&search=demo+project&page_size=5",
				}
				if q.Get("page") == "2" {
					response["results"] = []any{}
					response["next"] = nil
					response["previous"] = prefix + "projects/?search=demo+project&page_size=5"
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			})
			page, err := c.ListProjects(context.Background(), ListOptions{Search: "demo project", PageSize: 5})
			if err != nil || page.Count != 1 || len(page.Items) != 1 || !page.Next.Present() {
				t.Fatal(page, err)
			}
			if got := page.Items[0]; got.Ref != (TemplateRef{ID: 7, Type: ProjectTemplate}) || got.Name != "[redacted]demo" || got.Status != "successful" {
				t.Fatal(got)
			}
			page, err = c.ListProjects(context.Background(), ListOptions{Cursor: page.Next})
			if err != nil || len(page.Items) != 0 || page.Next.Present() || !page.Previous.Present() {
				t.Fatal(page, err)
			}
			page, err = c.ListProjects(context.Background(), ListOptions{Cursor: page.Previous})
			if err != nil || len(page.Items) != 1 {
				t.Fatal(page, err)
			}
		})
	}
}

func TestProjectsRejectMalformedResourcesAndLinks(t *testing.T) {
	for _, body := range []string{
		`{"count":1,"results":[{"id":0,"type":"project"}]}`,
		`{"count":1,"results":[{"id":1,"type":"job_template"}]}`,
		`{"count":1,"results":[{"id":1}]}`,
		`{"count":0,"results":[],"next":"/api/v2/job_templates/?page=2"}`,
	} {
		t.Run(body, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			})
			_, err := c.ListProjects(context.Background(), ListOptions{})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Kind != Malformed {
				t.Fatalf("expected malformed response, got %v", err)
			}
		})
	}
}
