package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

func TestObjectDetailsIntegrationBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("unexpected method or authentication")
				}
				route := strings.TrimPrefix(r.URL.Path, prefix)
				payload := map[string]any{"created": "2026-10-12T12:00:00Z", "modified": "2026-10-12T13:00:00Z", "description": "useful description", "summary_fields": map[string]any{"created_by": map[string]any{"id": 8, "username": "alice"}}}
				switch route {
				case "inventories/1/":
					payload["id"], payload["type"], payload["name"], payload["total_hosts"], payload["total_groups"], payload["has_active_failures"], payload["pending_deletion"] = 1, "inventory", "production", 0, 1, false, false
				case "inventories/1/groups/":
					payload = map[string]any{"count": 1, "results": []any{map[string]any{"id": 2, "type": "group", "name": "web", "inventory": 1}}}
				case "groups/2/":
					payload["id"], payload["type"], payload["name"], payload["inventory"] = 2, "group", "web", 1
				case "groups/2/all_hosts/":
					payload = map[string]any{"count": 1, "results": []any{map[string]any{"id": 3, "type": "host", "name": "web-1", "inventory": 4, "enabled": false}}}
				case "hosts/3/":
					payload["id"], payload["type"], payload["name"], payload["inventory"], payload["enabled"], payload["instance_id"], payload["last_job"] = 3, "host", "web-1", 4, false, "i-123", 9
					payload["ansible_facts_modified"] = "2026-10-12T14:00:00Z"
				case "job_templates/7/":
					payload["id"], payload["type"], payload["name"], payload["playbook"], payload["forks"], payload["diff_mode"], payload["inventory"], payload["project"] = 7, "job_template", "deploy", "site.yml", 0, false, 1, 10
					payload["extra_vars"], payload["host_config_key"] = "private-variable", "private-launch-key"
				case "projects/10/":
					payload["id"], payload["type"], payload["name"], payload["scm_type"], payload["scm_revision"], payload["scm_clean"] = 10, "project", "source", "git", "abc123", false
					payload["default_environment"] = 12
					payload["scm_url"] = "https://user:private-password@repo.example/team.git?token=private-token"
				case "jobs/11/":
					payload["id"], payload["type"], payload["name"], payload["status"], payload["launch_type"], payload["failed"], payload["verbosity"], payload["force_handlers"], payload["playbook"] = 11, "job", "latest", "failed", "manual", true, 0, false, "site.yml"
					payload["job_explanation"], payload["extra_vars"], payload["job_env"] = "secret\x1b[31m explanation", "private-variable", map[string]string{"PASSWORD": "private-password"}
				default:
					t.Errorf("unexpected endpoint %s", r.URL)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(payload); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}, "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			m := New(context.Background(), "").WithObjectDetails(client).WithInventoryContents(client).WithTemplates(client).WithProjects(client).WithJobs(client)
			cleanupModel(t, m)
			m.width, m.height = 100, 60
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
			contains := func(values ...string) {
				t.Helper()
				view := normalizedDetailText(m.View().Content)
				for _, value := range values {
					if !strings.Contains(view, value) {
						t.Fatalf("missing %q: %s", value, view)
					}
				}
				for _, value := range []string{"private-variable", "private-password", "private-token", "private-launch-key", "secret"} {
					if strings.Contains(view, value) {
						t.Fatalf("exposed %q", value)
					}
				}
				assertFrameSize(t, m)
			}
			m.screen = inventoriesScreen
			m.inventoryPage.Items = []aap.InventorySummary{{ID: 1, Name: "production"}}
			step("enter")
			contains("Total hosts: 0", "Pending deletion: false", "Created by: 8 · alice", "Groups", "Hosts")
			step("tab")
			step("j")
			if m.detailTop != 1 || m.selected != 0 {
				t.Fatal("detail focus changed menu selection")
			}
			step("tab")
			step("enter")
			step("enter")
			contains("web", "Inventory: 1", "useful description")
			step("j")
			step("enter")
			m.search = "web"
			step("enter")
			contains("Enabled: false", "Instance ID: i-123", "Inventory: 4", "Last job ID: 9", "Facts modified: 2026-10-12 14:00:00 UTC")
			step("r")
			step("esc")
			if m.screen != hostsScreen || m.search != "web" || m.group.ID != 2 || m.inventory.ID != 1 {
				t.Fatal("host back context lost")
			}
			step("esc")
			step("esc")
			step("esc")
			if m.screen != inventoryScreen || m.detailTop != 1 || m.objectDetailsFocus {
				t.Fatal("inventory detail/menu context lost")
			}
			step("esc")
			if m.screen != inventoriesScreen {
				t.Fatal("inventory list not restored")
			}
			for _, entry := range []struct {
				screen screen
				ref    aap.TemplateRef
				want   []string
			}{
				{templatesScreen, aap.TemplateRef{ID: 7, Type: aap.PlaybookTemplate}, []string{"Playbook: site.yml", "Forks: 0", "Diff mode: false", "Project: 10"}},
				{projectsScreen, aap.TemplateRef{ID: 10, Type: aap.ProjectTemplate}, []string{"SCM revision: abc123", "SCM clean: false", "URL: https://repo.example/team.git", "Default execution environment: 12"}},
			} {
				m.screen = entry.screen
				m.templatePage.Items = []aap.TemplateSummary{{Ref: entry.ref}}
				step("enter")
				contains(entry.want...)
				step("esc")
			}
			m.screen = jobsScreen
			m.jobPage.Items = []aap.JobSummary{{Ref: aap.JobRef{ID: 11, Type: aap.PlaybookJob}}}
			step("enter")
			contains("Failed: true", "Launch type: manual", "Failure / status explanation: [redacted] explanation", "Verbosity: 0", "Force handlers: false")
		})
	}
}

type fakeObjectDetails struct {
	ctx context.Context
	err error
}

func (f *fakeObjectDetails) Inventory(ctx context.Context, id int) (aap.InventoryDetails, error) {
	f.ctx = ctx
	return aap.InventoryDetails{}, f.err
}
func (f *fakeObjectDetails) Group(ctx context.Context, id int) (aap.GroupDetails, error) {
	f.ctx = ctx
	return aap.GroupDetails{}, f.err
}
func (f *fakeObjectDetails) Host(ctx context.Context, id int) (aap.HostDetails, error) {
	f.ctx = ctx
	return aap.HostDetails{}, f.err
}

func TestObjectDetailFailuresAndStaleResults(t *testing.T) {
	for _, screen := range []screen{inventoriesScreen, groupsScreen, hostsScreen} {
		for _, stale := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/stale=%t", screen, stale), func(t *testing.T) {
				f := &fakeObjectDetails{err: &aap.APIError{Kind: aap.Permission, Operation: "read object"}}
				m := New(context.Background(), "").WithObjectDetails(f)
				cleanupModel(t, m)
				m.screen = screen
				m.inventoryPage.Items = []aap.InventorySummary{{ID: 1, Name: "inventory"}}
				m.groupPage.Items = []aap.GroupSummary{{ID: 2, InventoryID: 1, Name: "group"}}
				m.hostPage.Items = []aap.HostSummary{{ID: 3, InventoryID: 1, Name: "host"}}
				_, cmd := m.Update(key("enter"))
				if cmd == nil || !m.loading {
					t.Fatal("detail read did not run asynchronously")
				}
				if stale {
					m.Update(key("esc"))
				}
				m.Update(cmd())
				if stale {
					if m.screen != screen || m.err != nil || m.loading || f.ctx.Err() != context.Canceled {
						t.Fatal("stale detail response applied or request not canceled")
					}
				} else {
					if m.err == nil || m.loading || !strings.Contains(m.View().Content, "permission") {
						t.Fatal("error not displayed")
					}
					var name string
					switch screen {
					case inventoriesScreen:
						name = m.inventoryDetail.Name
					case groupsScreen:
						name = m.groupDetail.Name
					case hostsScreen:
						name = m.hostDetail.Name
					}
					if name == "" {
						t.Fatal("summary identity lost after failed read")
					}
				}
			})
		}
	}
}

func TestWorkflowNodeDetailNavigation(t *testing.T) {
	m := New(context.Background(), "").WithJobs(&fakeJobs{})
	cleanupModel(t, m)
	m.screen = workflowScreen
	m.width, m.height = 100, 40
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.WorkflowJob}}}
	no := false
	m.nodePage.Items = []aap.WorkflowNode{{ID: 2, Identifier: "pending", Status: "pending", Reason: "Node has no launched child job", DoNotRun: &no, AllParentsMustConverge: &no, SuccessNodes: []int{3, 4}, FailureNodes: []int{}, AlwaysNodes: []int{5}}}
	m.Update(key("d"))
	view := normalizedDetailText(m.View().Content)
	for _, value := range []string{"pending", "All parents must converge: false", "Do not run: false", "Success nodes: 3, 4", "Failure nodes: None", "Always nodes: 5", "Node has no launched child job"} {
		if !strings.Contains(view, value) {
			t.Fatalf("missing %q: %s", value, view)
		}
	}
	if _, cmd := m.Update(key("enter")); cmd != nil || m.screen != nodeScreen {
		t.Fatal("pending node launched a child")
	}
	m.Update(key("esc"))
	if m.screen != workflowScreen || m.selected != 0 {
		t.Fatal("workflow context not restored")
	}
	m.nodePage.Items[0].Job = &aap.JobRef{ID: 7, Type: aap.PlaybookJob}
	m.Update(key("d"))
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	if m.screen != jobScreen || m.jobDetail.Ref.ID != 7 {
		t.Fatal("child not opened")
	}
	m.Update(key("esc"))
	if m.screen != nodeScreen || m.nodeDetail.ID != 2 || m.jobDetail.Ref.ID != 1 {
		t.Fatal("node/parent context not restored")
	}
}

func TestExpandedDetailsWrapAndScroll(t *testing.T) {
	for _, screen := range []screen{templateScreen, jobScreen, inventoryScreen, groupScreen, hostScreen, nodeScreen} {
		for _, width := range []int{1, 30, 80} {
			t.Run(fmt.Sprintf("%d/%d", screen, width), func(t *testing.T) {
				m := New(context.Background(), "")
				cleanupModel(t, m)
				m.screen, m.width, m.height = screen, width, 24
				long := strings.Repeat("部署 café ", 300) + "tail"
				m.templateDetail.Description = long
				m.jobDetail.Description = long
				m.inventoryDetail.Description = long
				m.groupDetail.Description = long
				m.hostDetail.Description = long
				m.nodeDetail.Reason = long
				if screen == inventoryScreen || screen == groupScreen {
					m.Update(key("tab"))
				}
				assertFrameSize(t, m)
				before := m.View().Content
				for i := 0; i < 20; i++ {
					m.Update(key("j"))
				}
				assertFrameSize(t, m)
				if m.detailTop != 20 || m.selected != 0 || m.View().Content == before {
					t.Fatal("wrapped details did not scroll")
				}
			})
		}
	}
}

func TestTypeSpecificDetailFields(t *testing.T) {
	no, yes, zero := false, true, 0
	for _, tc := range []struct {
		name     string
		template *aap.TemplateDetails
		job      *aap.JobDetails
		want     []string
	}{
		{name: "workflow template", template: &aap.TemplateDetails{Options: aap.ExecutionSettings{AllowSimultaneous: &no, SurveyEnabled: &no, Limit: "web*"}}, want: []string{"Allow simultaneous: false", "Survey enabled: false", "Limit: web*"}},
		{name: "inventory source", template: &aap.TemplateDetails{Inventory: &aap.InventorySettings{Source: "scm", SourceProject: &aap.ResourceReference{ID: 7}, Overwrite: &no, UpdateCacheTimeout: &zero}}, want: []string{"Source: scm", "Source project: 7", "Overwrite hosts/groups: false", "Update cache timeout (seconds): 0"}},
		{name: "inventory update", job: &aap.JobDetails{Inventory: &aap.InventorySettings{Source: "ec2", InventorySource: &aap.ResourceReference{ID: 8}}, LicenseError: &no, OrgHostLimitError: &yes}, want: []string{"Source: ec2", "Inventory source: 8", "License error: false", "Organization host limit error: true"}},
		{name: "ad hoc command", job: &aap.JobDetails{ModuleName: "shell", ModuleArgs: "password=never-display", Options: aap.ExecutionSettings{Inventory: &aap.ResourceReference{ID: 9}, Credential: &aap.ResourceReference{ID: 10, Name: "machine"}, Forks: &zero, BecomeEnabled: &no}}, want: []string{"Module: shell", "Inventory: 9", "Credential: 10 · machine", "Forks: 0", "Privilege escalation: false"}},
		{name: "approval", job: &aap.JobDetails{ApprovalTimeout: &zero, TimedOut: &yes, ApprovalExpiration: "2026-10-12T13:00:00Z"}, want: []string{"Approval timeout (seconds): 0", "Approval timed out: true", "Approval expiration: 2026-10-12 13:00:00 UTC"}},
		{name: "system job", job: &aap.JobDetails{SystemJobType: "cleanup_jobs"}, want: []string{"System job: cleanup_jobs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(context.Background(), "")
			cleanupModel(t, m)
			m.screen = jobScreen
			if tc.job != nil {
				m.jobDetail = *tc.job
			}
			if tc.template != nil {
				m.screen = templateScreen
				m.templateDetail = *tc.template
			}
			text := normalizedDetailText(strings.Join(m.details(110), "\n"))
			for _, value := range tc.want {
				if !strings.Contains(text, value) {
					t.Fatalf("missing %q: %s", value, text)
				}
			}
			if strings.Contains(text, "never-display") {
				t.Fatal("module arguments exposed")
			}
		})
	}
}
