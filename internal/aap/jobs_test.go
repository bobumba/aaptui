package aap

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"aaptui/internal/config"
)

func TestJobTypesAndCapabilities(t *testing.T) {
	data, err := os.ReadFile("testdata/jobs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Results []json.RawMessage }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/unified_jobs/" {
			if _, err := w.Write(data); err != nil {
				t.Error(err)
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, "cancel/") {
			if err := json.NewEncoder(w).Encode(map[string]bool{"can_cancel": true}); err != nil {
				t.Error(err)
			}
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		id, e := strconv.Atoi(parts[4])
		if e != nil || id < 1 || id > len(fixture.Results) {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(fixture.Results[id-1]); err != nil {
			t.Error(err)
		}
	})
	p, err := c.ListJobs(context.Background(), ListOptions{})
	if err != nil || len(p.Items) != 8 {
		t.Fatal(p, err)
	}
	for _, summary := range p.Items {
		d, err := c.Job(context.Background(), summary.Ref)
		if err != nil || d.Ref != summary.Ref {
			t.Fatal(d, err)
		}
		switch d.Ref.Type {
		case PlaybookJob:
			if d.Playbook == nil || d.Capabilities.Output.State != Available || d.Capabilities.Cancel.State != Available {
				t.Fatal(d)
			}
		case WorkflowJob:
			if d.Capabilities.Children.State != Available || d.Capabilities.Output.State != Unavailable {
				t.Fatal(d)
			}
		case WorkflowApproval:
			if d.Capabilities.Cancel.State != Unavailable || d.ApprovalTimeout == nil {
				t.Fatal(d)
			}
		case "future_job":
			if d.Capabilities.Cancel.State != Unknown {
				t.Fatal(d)
			}
		}
	}
}
func TestPermissionAndMissingJob(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "cancel/") {
			w.WriteHeader(403)
			return
		}
		if strings.Contains(r.URL.Path, "99/") {
			w.WriteHeader(404)
			return
		}
		if _, err := w.Write([]byte(`{"id":1,"type":"job","status":"running","related":{"cancel":"/api/v2/jobs/1/cancel/"}}`)); err != nil {
			t.Error(err)
		}
	})
	d, err := c.Job(context.Background(), JobRef{1, PlaybookJob})
	if err != nil || d.Capabilities.Cancel.State != Unavailable {
		t.Fatal(d, err)
	}
	if _, err = c.Job(context.Background(), JobRef{99, PlaybookJob}); err == nil {
		t.Fatal("missing accepted")
	}
}
