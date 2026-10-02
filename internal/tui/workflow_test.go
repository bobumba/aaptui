package tui

import (
	"context"
	"testing"

	"aaptui/internal/aap"
)

type fakeWorkflow struct{}

func (fakeWorkflow) WorkflowNodes(ctx context.Context, ref aap.JobRef, o aap.ListOptions) (aap.Page[aap.WorkflowNode], error) {
	return aap.Page[aap.WorkflowNode]{Items: []aap.WorkflowNode{{ID: 1, Identifier: "pending"}, {ID: 2, Job: &aap.JobRef{ID: 12, Type: aap.WorkflowJob}}}}, nil
}
func TestNestedWorkflowBack(t *testing.T) {
	jobs := &fakeJobs{}
	m := New(context.Background(), "").WithJobs(jobs).WithWorkflows(fakeWorkflow{})
	defer m.Close()
	m.screen = jobScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 2, Type: aap.WorkflowJob}}, Capabilities: aap.JobCapabilities{Children: aap.Capability{State: aap.Available}}}
	_, cmd := m.Update(key("w"))
	m.Update(cmd())
	if m.screen != workflowScreen {
		t.Fatal("children")
	}
	_, cmd = m.Update(key("enter"))
	if cmd != nil {
		t.Fatal("pending node opened")
	}
	m.selected = 1
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if m.jobDetail.Ref.ID != 12 {
		t.Fatal("nested")
	}
	_, cmd = m.Update(key("esc"))
	if cmd != nil {
		m.Update(cmd())
	}
	if m.screen != workflowScreen || m.jobDetail.Ref.ID != 2 || len(m.nodePage.Items) != 2 {
		t.Fatal("workflow parent not restored")
	}
	_, cmd = m.Update(key("esc"))
	if cmd != nil {
		m.Update(cmd())
	}
	if m.screen != jobScreen || m.jobDetail.Ref.ID != 2 {
		t.Fatal("parent details not restored")
	}
}
