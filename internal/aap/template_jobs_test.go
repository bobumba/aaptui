package aap

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"aaptui/internal/config"
)

// Fixtures are synthetic, modeled on the pinned JobTemplateJobsList contract.
func TestTemplateJobsBothPrefixes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			route := prefix + "job_templates/7/jobs/"
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != route || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("page_size") != "50" || r.URL.Query().Get("order_by") != "-id" {
					t.Error("incorrect request", r.URL)
				}
				if r.URL.Query().Get("page") == "2" {
					if _, err := fmt.Fprintf(w, `{"count":2,"results":[{"id":8,"type":"job","job_template":7,"status":"successful"}],"previous":%q}`, route+"?page=1&order_by=-id"); err != nil {
						t.Error(err)
					}
				} else {
					if _, err := fmt.Fprintf(w, `{"count":2,"results":[{"id":9,"type":"job","job_template":7,"name":"secret\u001b[31mdeploy","status":"running","started":"2026-10-12T10:00:00Z"}],"next":%q}`, route+"?page=2&order_by=-id"); err != nil {
						t.Error(err)
					}
				}
			})
			ref := TemplateRef{7, PlaybookTemplate}
			page, err := c.TemplateJobs(context.Background(), ref, ListOptions{})
			if err != nil || page.Count != 2 || len(page.Items) != 1 || page.Items[0].Ref.ID != 9 || page.Items[0].Name != "[redacted]deploy" || page.Items[0].Started == "" {
				t.Fatal(page, err)
			}
			page, err = c.TemplateJobs(context.Background(), ref, ListOptions{Cursor: page.Next})
			if err != nil || page.Items[0].Ref.ID != 8 || !page.Previous.Present() {
				t.Fatal(page, err)
			}
		})
	}
}

func TestTemplateJobsRejectWrongResourcesAndLinks(t *testing.T) {
	for _, body := range []string{
		`{"count":1,"results":[{"id":1,"type":"job","job_template":8}]}`,
		`{"count":1,"results":[{"id":1,"type":"workflow_job"}]}`,
		`{"count":0,"results":[],"next":"/api/v2/job_templates/8/jobs/?page=2"}`,
		`{"count":0,"results":[],"next":"https://evil.example/api/v2/job_templates/7/jobs/"}`,
	} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			if _, err := fmt.Fprint(w, body); err != nil {
				t.Error(err)
			}
		})
		if _, err := c.TemplateJobs(context.Background(), TemplateRef{7, PlaybookTemplate}, ListOptions{}); err == nil {
			t.Fatal("accepted invalid response", body)
		}
	}
	c := &Client{}
	for _, ref := range []TemplateRef{{0, PlaybookTemplate}, {1, ProjectTemplate}} {
		if _, err := c.TemplateJobs(context.Background(), ref, ListOptions{}); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
	c, _ = testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("wrong-template cursor made a request") })
	if _, err := c.TemplateJobs(context.Background(), TemplateRef{7, PlaybookTemplate}, ListOptions{Cursor: PageCursor{link: "/api/v2/job_templates/8/jobs/?page=2"}}); err == nil {
		t.Fatal("wrong cursor accepted")
	}
	for _, code := range []int{401, 403, 404, 503} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			if _, err := fmt.Fprint(w, "secret"); err != nil {
				t.Error(err)
			}
		})
		if _, err := c.TemplateJobs(context.Background(), TemplateRef{7, PlaybookTemplate}, ListOptions{}); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe/missing error", err)
		}
	}
}
