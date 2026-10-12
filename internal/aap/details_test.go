package aap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"aaptui/internal/config"
)

// All fixtures in this file are synthetic, based on the pinned serializer field
// review in docs/API_CONTRACT.md. They are not captured AAP responses.
func TestExpandedTemplateDetailsBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, kind := range []TemplateType{PlaybookTemplate, WorkflowTemplate, ProjectTemplate, InventoryTemplate, SystemTemplate, ApprovalTemplate, "future_template"} {
			t.Run(string(mode)+"/"+string(kind), func(t *testing.T) {
				payload := map[string]any{
					"id": 1, "type": kind, "name": "demo", "created": "2026-10-12T12:00:00Z", "modified": "2026-10-12T13:00:00Z",
					"organization": 2, "last_job_run": "2026-10-12T12:00:00Z", "last_job_failed": false, "next_job_run": nil,
					"summary_fields": map[string]any{"organization": map[string]any{"id": 2, "name": "secret\x1b[31mteam"}, "created_by": map[string]any{"id": 3, "username": "alice"}, "resolved_environment": map[string]any{"id": 6, "name": "default"}, "credentials": []any{map[string]any{"id": 8, "name": "machine", "inputs": map[string]string{"password": "never-display"}}}},
				}
				switch kind {
				case PlaybookTemplate:
					payload["playbook"], payload["inventory"], payload["project"], payload["execution_environment"] = "site.yml", 4, 5, 6
					payload["forks"], payload["verbosity"], payload["timeout"], payload["diff_mode"], payload["survey_enabled"] = 0, 2, 0, false, true
					payload["job_tags"], payload["skip_tags"], payload["start_at_task"], payload["job_slice_count"] = "deploy", "slow", "Install", 1
				case WorkflowTemplate:
					payload["allow_simultaneous"], payload["survey_enabled"], payload["inventory"], payload["limit"] = false, false, 4, "web*"
				case ProjectTemplate:
					payload["scm_type"], payload["scm_url"] = "git", "https://user:password@repo.example/team.git?access_token=other-secret#private"
					payload["default_environment"] = 9
					payload["scm_revision"], payload["scm_refspec"], payload["scm_clean"], payload["scm_update_cache_timeout"] = "abc123", "refs/heads/*", false, 0
				case InventoryTemplate:
					payload["source"], payload["source_project"], payload["overwrite"], payload["update_cache_timeout"] = "scm", 5, false, 0
				case SystemTemplate:
					payload["job_type"] = "cleanup_jobs"
				case ApprovalTemplate:
					payload["timeout"] = 0
				}
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != cPrefix(mode)+templateCollection(kind)+"/1/" {
						t.Error(r.URL)
					}
					if err := json.NewEncoder(w).Encode(payload); err != nil {
						t.Error(err)
					}
				})
				d, err := c.Template(context.Background(), TemplateRef{ID: 1, Type: kind})
				if err != nil {
					t.Fatal(err)
				}
				if d.Metadata.Created != "2026-10-12T12:00:00Z" || d.Metadata.CreatedBy == nil || d.Metadata.CreatedBy.Name != "alice" || d.Metadata.Organization == nil || d.Metadata.Organization.Name != "[redacted]team" || d.LastJobFailed == nil || *d.LastJobFailed {
					t.Fatalf("metadata: %+v", d)
				}
				if len(d.Options.Credentials) != 1 || d.Options.Credentials[0].ID != 8 || d.Options.Credentials[0].Name != "machine" {
					t.Fatal(d.Options.Credentials)
				}
				switch kind {
				case PlaybookTemplate:
					if d.Options.Forks == nil || *d.Options.Forks != 0 || d.Options.Timeout == nil || *d.Options.Timeout != 0 || d.Options.DiffMode == nil || *d.Options.DiffMode || d.Options.Inventory == nil || d.Options.Inventory.ID != 4 || d.Options.Inventory.Name != "" || d.Options.JobTags != "deploy" || d.Options.Environment.ID != 6 || d.Options.ResolvedEnvironment == nil || d.Options.ResolvedEnvironment.Name != "default" {
						t.Fatalf("options: %+v", d.Options)
					}
				case WorkflowTemplate:
					if d.Options.AllowSimultaneous == nil || *d.Options.AllowSimultaneous || d.Options.Limit != "web*" {
						t.Fatal(d.Options)
					}
				case ProjectTemplate:
					if d.Project.DefaultEnvironment == nil || d.Project.DefaultEnvironment.ID != 9 || d.Project.SCMURL != "https://repo.example/team.git" || d.Project.SCMRevision != "abc123" || d.Project.Clean == nil || *d.Project.Clean || d.Project.UpdateCacheTimeout == nil || *d.Project.UpdateCacheTimeout != 0 {
						t.Fatal(d.Project)
					}
				case InventoryTemplate:
					if d.Inventory.SourceProject.ID != 5 || d.Inventory.Overwrite == nil || *d.Inventory.Overwrite || *d.Inventory.UpdateCacheTimeout != 0 {
						t.Fatal(d.Inventory)
					}
				case ApprovalTemplate:
					if d.ApprovalTimeout == nil || *d.ApprovalTimeout != 0 {
						t.Fatal(d)
					}
				}
			})
		}
	}
}

func cPrefix(mode config.ConnectionMode) string {
	if mode == config.Direct {
		return "/api/v2/"
	}
	return "/api/controller/v2/"
}

func TestExpandedJobDetailsBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, kind := range []JobType{PlaybookJob, WorkflowJob, ProjectUpdate, InventoryUpdate, AdHocCommand, SystemJob, WorkflowApproval, "future_job"} {
			t.Run(string(mode)+"/"+string(kind), func(t *testing.T) {
				payload := map[string]any{"id": 1, "type": kind, "name": "run", "status": "failed", "failed": true,
					"created": "2026-10-12T12:00:00Z", "launch_type": "manual", "job_explanation": "secret\x1b[31m reason", "unified_job_template": 7,
					"summary_fields": map[string]any{"unified_job_template": map[string]any{"id": 7, "name": "deploy"}, "source_workflow_job": map[string]any{"id": 2, "name": "parent"}, "ancestor_job": map[string]any{"id": 3, "name": "ancestor"}},
				}
				switch kind {
				case PlaybookJob:
					payload["playbook"], payload["scm_revision"], payload["job_slice_number"], payload["job_slice_count"] = "site.yml", "abc123", 0, 1
				case ProjectUpdate:
					payload["project"], payload["scm_type"], payload["scm_revision"] = 5, "git", "def456"
				case InventoryUpdate:
					payload["inventory_source"], payload["license_error"], payload["org_host_limit_error"] = 9, false, true
				case AdHocCommand:
					payload["module_name"], payload["inventory"], payload["forks"], payload["become_enabled"] = "ping", 4, 0, false
				case WorkflowApproval:
					payload["approval_expiration"], payload["timed_out"] = "2026-10-12T13:00:00Z", true
				}
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != cPrefix(mode)+jobCollection(kind)+"/1/" {
						t.Error(r.URL)
					}
					if err := json.NewEncoder(w).Encode(payload); err != nil {
						t.Error(err)
					}
				})
				d, err := c.Job(context.Background(), JobRef{ID: 1, Type: kind})
				if err != nil || d.Failed == nil || !*d.Failed || d.LaunchType != "manual" || d.Explanation != "[redacted] reason" || d.Template == nil || d.Template.Name != "deploy" || d.SourceWorkflowJob == nil || d.SourceWorkflowJob.Name != "parent" || d.AncestorJob == nil || d.AncestorJob.ID != 3 {
					t.Fatalf("details: %+v, %v", d, err)
				}
				switch kind {
				case PlaybookJob:
					if d.SCMRevision != "abc123" || d.Project != nil || d.JobSliceNumber == nil || *d.JobSliceNumber != 0 {
						t.Fatal(d)
					}
				case ProjectUpdate:
					if d.Project.SCMRevision != "def456" || d.Options.Project.ID != 5 {
						t.Fatal(d)
					}
				case InventoryUpdate:
					if d.Inventory.InventorySource.ID != 9 || d.LicenseError == nil || *d.LicenseError || !*d.OrgHostLimitError {
						t.Fatal(d)
					}
				case AdHocCommand:
					if d.ModuleName != "ping" || d.Options.Forks == nil || *d.Options.Forks != 0 || d.Options.BecomeEnabled == nil || *d.Options.BecomeEnabled {
						t.Fatal(d)
					}
				case WorkflowApproval:
					if d.ApprovalExpiration != "2026-10-12T13:00:00Z" || d.TimedOut == nil || !*d.TimedOut {
						t.Fatal(d)
					}
				}
			})
		}
	}
}

func TestOptionalDetailFieldsAndReferenceIdentity(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"id":1,"type":"job_template","forks":null,"diff_mode":null,"inventory":4,"summary_fields":{"inventory":{"id":99,"name":"wrong inventory"}}}`)); err != nil {
			t.Error(err)
		}
	})
	d, err := c.Template(context.Background(), TemplateRef{ID: 1, Type: PlaybookTemplate})
	if err != nil || d.Options.Forks != nil || d.Options.DiffMode != nil || d.Metadata.CreatedBy != nil || d.Metadata.Created != "" || d.Options.Inventory.ID != 4 || d.Options.Inventory.Name != "" {
		t.Fatal(d, err)
	}
}

func TestInventoryObjectDetailsBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				payload := map[string]any{"id": 1, "name": "secret\x1b[31mobject", "created": "2026-10-12T12:00:00Z", "inventory": 4,
					"summary_fields": map[string]any{"inventory": map[string]any{"id": 4, "name": "production"}},
				}
				switch r.URL.Path {
				case cPrefix(mode) + "inventories/1/":
					payload["type"], payload["total_hosts"], payload["total_groups"], payload["pending_deletion"], payload["has_active_failures"] = "inventory", 0, 2, false, true
				case cPrefix(mode) + "groups/1/":
					payload["type"] = "group"
				case cPrefix(mode) + "hosts/1/":
					payload["type"], payload["enabled"], payload["instance_id"], payload["last_job"], payload["has_active_failures"] = "host", false, "i-123", 9, false
					payload["ansible_facts_modified"] = "2026-10-12T14:00:00Z"
				default:
					t.Error(r.URL)
				}
				if err := json.NewEncoder(w).Encode(payload); err != nil {
					t.Error(err)
				}
			})
			inventory, err := c.Inventory(context.Background(), 1)
			if err != nil || inventory.TotalHosts == nil || *inventory.TotalHosts != 0 || inventory.PendingDeletion == nil || *inventory.PendingDeletion || inventory.Metadata.Created != "2026-10-12T12:00:00Z" || inventory.Name != "[redacted]object" {
				t.Fatal(inventory, err)
			}
			group, err := c.Group(context.Background(), 1)
			if err != nil || group.InventoryID != 4 || group.Inventory.Name != "production" || group.Name != "[redacted]object" {
				t.Fatal(group, err)
			}
			host, err := c.Host(context.Background(), 1)
			if err != nil || host.Enabled == nil || *host.Enabled || host.InstanceID != "i-123" || host.LastJob == nil || *host.LastJob != 9 || host.FactsModified != "2026-10-12T14:00:00Z" {
				t.Fatal(host, err)
			}
		})
	}
}

func TestInventoryDetailErrors(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{
		{403, "private"}, {404, "private"}, {200, `{"id":2,"type":"inventory"}`}, {200, `{"id":1,"type":"wrong"}`}, {200, "invalid"},
	} {
		t.Run(fmt.Sprint(response), func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(response.status)
				if _, err := w.Write([]byte(response.body)); err != nil {
					t.Error(err)
				}
			})
			for _, read := range []func() error{
				func() error { _, err := c.Inventory(context.Background(), 1); return err },
				func() error { _, err := c.Group(context.Background(), 1); return err },
				func() error { _, err := c.Host(context.Background(), 1); return err },
			} {
				if err := read(); err == nil || strings.Contains(err.Error(), "private") {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestWorkflowNodeDetailsBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != cPrefix(mode)+"workflow_jobs/1/workflow_nodes/" {
					t.Error(r.URL)
				}
				if _, err := w.Write([]byte(`{"count":1,"results":[{"id":2,"identifier":"secret\u001b[31mstep","created":"2026-10-12T12:00:00Z","workflow_job":1,"unified_job_template":7,"job":null,"do_not_run":false,"all_parents_must_converge":false,"success_nodes":[3,4],"failure_nodes":[],"always_nodes":[5],"summary_fields":{"unified_job_template":{"id":7,"name":"deploy"},"created_by":{"id":8,"username":"alice"}}}]}`)); err != nil {
					t.Error(err)
				}
			})
			page, err := c.WorkflowNodes(context.Background(), JobRef{ID: 1, Type: WorkflowJob}, ListOptions{})
			if err != nil || len(page.Items) != 1 {
				t.Fatal(page, err)
			}
			node := page.Items[0]
			if node.Identifier != "[redacted]step" || node.Metadata.CreatedBy == nil || node.Metadata.CreatedBy.Name != "alice" || node.Template.Name != "deploy" || node.WorkflowJob.ID != 1 || len(node.SuccessNodes) != 2 || node.FailureNodes == nil || len(node.FailureNodes) != 0 || node.AllParentsMustConverge == nil || *node.AllParentsMustConverge || node.DoNotRun == nil || *node.DoNotRun || node.Skipped || node.Job != nil {
				t.Fatalf("node: %+v", node)
			}
		})
	}
}
