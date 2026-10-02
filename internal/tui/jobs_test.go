package tui

import (
	"context"
	"strings"
	"testing"

	"aaptui/internal/aap"
)

type fakeJobs struct {
	search   string
	requests []aap.JobRef
}

func (f *fakeJobs) ListJobs(ctx context.Context, o aap.ListOptions) (aap.Page[aap.JobSummary], error) {
	f.search = o.Search
	return aap.Page[aap.JobSummary]{Items: []aap.JobSummary{{Ref: aap.JobRef{ID: 7, Type: aap.PlaybookJob}, Name: "demo", Status: "running"}}}, nil
}
func (f *fakeJobs) Job(ctx context.Context, r aap.JobRef) (aap.JobDetails, error) {
	f.requests = append(f.requests, r)
	return aap.JobDetails{JobSummary: aap.JobSummary{Ref: r, Name: "demo", Status: "running"}, Capabilities: aap.JobCapabilities{Output: aap.Capability{State: aap.Unavailable, Reason: "No stdout"}}}, nil
}
func TestJobNavigation(t *testing.T) {
	f := &fakeJobs{}
	m := New(context.Background(), "").WithJobs(f)
	defer m.Close()
	m.selected = 1
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	if len(m.jobPage.Items) != 1 {
		t.Fatal("list")
	}
	m.Update(key("/"))
	m.Update(key("q"))
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if f.search != "q" {
		t.Fatal("search")
	}
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if m.screen != jobScreen || !strings.Contains(m.View().Content, "No stdout") {
		t.Fatal("details")
	}
	_, cmd = m.Update(key("esc"))
	if cmd != nil {
		m.Update(cmd())
	}
	if m.screen != jobsScreen {
		t.Fatal("back")
	}
}

type inaccessibleJobs struct{ fakeJobs }

func (f *inaccessibleJobs) Job(context.Context, aap.JobRef) (aap.JobDetails, error) {
	return aap.JobDetails{}, &aap.APIError{Kind: aap.Permission, Operation: "read child"}
}
func TestInaccessibleChildRetainsIdentity(t *testing.T) {
	f := &inaccessibleJobs{}
	m := New(context.Background(), "").WithJobs(f)
	defer m.Close()
	m.screen = workflowScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 2, Type: aap.WorkflowJob}, Name: "parent"}}
	m.nodePage = aap.Page[aap.WorkflowNode]{Items: []aap.WorkflowNode{{Job: &aap.JobRef{ID: 7, Type: aap.PlaybookJob}, Name: "inaccessible child", Status: "running"}}}
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	if m.err == nil || m.jobDetail.Ref.ID != 7 || m.jobDetail.Name != "inaccessible child" || m.jobDetail.Capabilities.Children.State != aap.Unknown {
		t.Fatal("parent details leaked into child", m.jobDetail, m.err)
	}
}
