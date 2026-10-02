package aap

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"aaptui/internal/config"
)

func TestTemplateTypes(t *testing.T) {
	for _, kind := range []TemplateType{PlaybookTemplate, WorkflowTemplate, ProjectTemplate, InventoryTemplate, SystemTemplate, ApprovalTemplate, "future_template"} {
		t.Run(string(kind), func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/api/v2/"+templateCollection(kind)+"/") {
					t.Error(r.URL)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": kind, "name": "secret\x1b[31mname", "playbook": "site.yml", "scm_type": "git", "source": "ec2"}); err != nil {
					t.Error(err)
				}
			})
			d, err := c.Template(context.Background(), TemplateRef{1, kind})
			if err != nil || d.Name != "[redacted]name" {
				t.Fatal(d, err)
			}
			if kind == PlaybookTemplate && (d.Playbook == nil || d.Playbook.Playbook != "site.yml") {
				t.Fatal(d)
			}
			if kind == ProjectTemplate && d.Project == nil {
				t.Fatal(d)
			}
			if kind == InventoryTemplate && d.Inventory == nil {
				t.Fatal(d)
			}
		})
	}
}
func TestTemplatePagination(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			calls := 0
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != prefix+"job_templates/" {
					t.Errorf("list endpoint = %q, want %q", r.URL.Path, prefix+"job_templates/")
					http.NotFound(w, r)
					return
				}
				if r.URL.Query().Get("page") == "2" {
					if err := json.NewEncoder(w).Encode(map[string]any{"count": 1, "results": []any{}}); err != nil {
						t.Error(err)
					}
					return
				}
				if r.URL.Query().Get("search") != "abc" || r.URL.Query().Get("page_size") != "5" {
					t.Error(r.URL)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"count": 1, "results": []any{map[string]any{"id": 1, "type": "job_template", "name": "a"}}, "next": prefix + "job_templates/?page=2"}); err != nil {
					t.Error(err)
				}
			})
			p, err := c.ListTemplates(context.Background(), ListOptions{Search: "abc", PageSize: 5})
			if err != nil || len(p.Items) != 1 || !p.Next.Present() {
				t.Fatal(p, err)
			}
			if p.Items[0].Ref.Type != PlaybookTemplate || p.Count != 1 {
				t.Fatal(p)
			}
			p, err = c.ListTemplates(context.Background(), ListOptions{Cursor: p.Next})
			if err != nil || len(p.Items) != 0 || calls != 2 {
				t.Fatal(p, err)
			}
		})
	}
}
