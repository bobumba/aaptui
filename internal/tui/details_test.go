package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func normalizedDetailText(text string) string {
	return strings.Join(strings.Fields(ansi.Strip(text)), " ")
}

func TestJobDetailSections(t *testing.T) {
	for _, kind := range []aap.JobType{aap.PlaybookJob, aap.WorkflowJob, aap.ProjectUpdate, aap.InventoryUpdate, aap.AdHocCommand, aap.SystemJob, aap.WorkflowApproval, "future_job"} {
		t.Run(string(kind), func(t *testing.T) {
			m := New(context.Background(), "")
			cleanupModel(t, m)
			m.screen = jobScreen
			no, zero := false, 0
			m.jobDetail = aap.JobDetails{
				JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 42, Type: kind}, Name: "nightly deploy", Description: "Deployment details", Status: "running"},
				Failed:     &no,
				Metadata:   aap.ResourceMetadata{CreatedBy: &aap.ResourceReference{ID: 8, Name: "alice"}},
				Template:   &aap.ResourceReference{ID: 7, Name: "deploy"},
				Options:    aap.ExecutionSettings{Forks: &zero, DiffMode: &no},
				Playbook:   &aap.PlaybookSettings{Playbook: "site.yml"},
				Capabilities: aap.JobCapabilities{
					Output:   aap.Capability{State: aap.Available},
					Children: aap.Capability{State: aap.Unavailable, Reason: "No children"},
					Cancel:   aap.Capability{State: aap.Unknown, Reason: "Permission unknown"},
				},
			}
			text := ansi.Strip(strings.Join(m.details(110), "\n"))
			capabilities, details, ok := strings.Cut(text, "Job details\n")
			capabilities, details = normalizedDetailText(capabilities), normalizedDetailText(details)
			if !ok || strings.Count(text, "Capabilities") != 1 || strings.Contains(text, "Configuration") {
				t.Fatalf("incorrect sections: %s", text)
			}
			for _, value := range []string{"Capabilities", "Output (o): available", "Children (w): unavailable · No children", "Cancel (c): unknown · Permission unknown"} {
				if !strings.Contains(capabilities, value) || strings.Contains(details, value) {
					t.Fatalf("capability outside its section: %q in %s", value, text)
				}
			}
			for _, value := range []string{"ID: 42", "Name: nightly deploy", "Deployment details", "Status: running", "Started: —", "Finished: —", "Created by: 8 · alice", "Template: 7 · deploy", "Playbook: site.yml", "Forks: 0", "Failed: false", "Diff mode: false"} {
				if !strings.Contains(details, value) || strings.Contains(capabilities, value) {
					t.Fatalf("API field outside Job details: %q in %s", value, text)
				}
			}
		})
	}
}

func TestUnavailableJobCapabilities(t *testing.T) {
	for _, state := range []aap.CapabilityState{aap.Unavailable, aap.Unknown} {
		m := New(context.Background(), "").WithWorkflows(fakeWorkflow{}).WithCancellation(&fakeCancel{}).WithOutput(func(context.Context, aap.JobRef) OutputSession {
			t.Fatal("unavailable output opened")
			return nil
		})
		cleanupModel(t, m)
		m.screen, m.width, m.height = jobScreen, 30, 24
		capability := aap.Capability{State: state, Reason: "Operation unavailable"}
		m.jobDetail.Capabilities = aap.JobCapabilities{Output: capability, Children: capability, Cancel: capability}
		assertFrameSize(t, m)
		for _, action := range []string{"o", "w", "c"} {
			if _, cmd := m.Update(key(action)); cmd != nil || m.screen != jobScreen || m.confirming {
				t.Fatalf("%s triggered %s operation", action, state)
			}
		}
	}
	m := New(context.Background(), "")
	cleanupModel(t, m)
	if text := strings.Join(m.renderDetailFields(m.capabilityFields(), 80), "\n"); strings.Count(text, "unknown") != 3 {
		t.Fatalf("missing capabilities not shown as unknown: %s", text)
	}
}
