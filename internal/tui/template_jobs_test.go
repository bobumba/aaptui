package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"aaptui/internal/aap"
)

type fakeTemplateJobs struct {
	ref  aap.TemplateRef
	page aap.Page[aap.JobSummary]
	err  error
}

func (f *fakeTemplateJobs) TemplateJobs(ctx context.Context, ref aap.TemplateRef, options aap.ListOptions) (aap.Page[aap.JobSummary], error) {
	f.ref = ref
	if ctx.Err() != nil {
		return aap.Page[aap.JobSummary]{}, ctx.Err()
	}
	return f.page, f.err
}

func TestTemplateJobsNavigationAndFocus(t *testing.T) {
	f := &fakeTemplateJobs{page: aap.Page[aap.JobSummary]{Count: 2, Items: []aap.JobSummary{
		{Ref: aap.JobRef{ID: 9, Type: aap.PlaybookJob}, Name: "latest", Status: "running", Started: "2026-10-12T10:00:00Z"},
		{Ref: aap.JobRef{ID: 8, Type: aap.PlaybookJob}, Name: "older", Status: "successful"},
	}}}
	m := New(context.Background(), "").WithTemplates(&fakeTemplates{}).WithTemplateJobs(f).WithJobs(&fakeJobs{})
	cleanupModel(t, m)
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	_, cmd = m.Update(key("enter"))
	if _, refresh := m.Update(key("r")); refresh != nil {
		t.Fatal("refresh interrupted the initial template details request")
	}
	_, jobsCmd := m.Update(cmd())
	if jobsCmd == nil || !m.loading {
		t.Fatal("table fetch not scheduled")
	}
	if !strings.Contains(m.View().Content, "Loading jobs") {
		t.Fatal("missing loading state")
	}
	m.Update(jobsCmd())
	view := m.View().Content
	for _, text := range []string{"site.yml", "latest", "older", "STARTED", "running"} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing %s: %s", text, view)
		}
	}
	if f.ref.ID != 7 || !m.templateJobsFocus {
		t.Fatal("wrong template/focus")
	}
	m.Update(key("j"))
	m.Update(key("tab"))
	m.Update(key("j"))
	if m.selected != 1 || m.detailTop != 1 {
		t.Fatal("focus did not separate scrolling and selection")
	}
	m.Update(key("tab"))
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if m.screen != jobScreen || m.jobDetail.Ref.ID != 8 {
		t.Fatal("wrong job opened")
	}
	m.Update(key("esc"))
	if m.screen != templateScreen || m.selected != 1 || m.detailTop != 1 || !m.templateJobsFocus || m.templateDetail.Ref.ID != 7 {
		t.Fatal("template context lost")
	}
	_, cmd = m.Update(key("r"))
	m.Update(cmd())
	if m.selected != 0 {
		t.Fatal("refresh did not reset selection")
	}
}

func TestTemplateJobsEmptyFailureAndStaleResults(t *testing.T) {
	f := &fakeTemplateJobs{}
	m := New(context.Background(), "").WithTemplateJobs(f)
	cleanupModel(t, m)
	m.screen = templateScreen
	m.templateDetail = aap.TemplateDetails{TemplateSummary: aap.TemplateSummary{Ref: aap.TemplateRef{ID: 7, Type: aap.PlaybookTemplate}, Name: "template"}}
	m.Update(m.loadTemplateJobs(aap.PageCursor{})())
	if !strings.Contains(m.View().Content, "No jobs.") {
		t.Fatal("missing empty state")
	}
	f.err = &aap.APIError{Kind: aap.Permission, Operation: "read page"}
	m.Update(m.loadTemplateJobs(aap.PageCursor{})())
	if !strings.Contains(m.View().Content, "Unable to load jobs") || !strings.Contains(m.View().Content, "template") {
		t.Fatal("error lost template details")
	}
	cmd := m.loadTemplateJobs(aap.PageCursor{})
	m.move(templatesScreen)
	m.templateDetail.Ref.ID = 8
	m.Update(cmd())
	if m.err != nil || m.loading || len(m.templateJobPage.Items) != 0 {
		t.Fatal("stale result applied")
	}
}

func TestTemplateJobTableSelectionFits(t *testing.T) {
	m := New(context.Background(), "").WithTemplateJobs(&fakeTemplateJobs{})
	cleanupModel(t, m)
	m.screen = templateScreen
	m.templateJobsFocus = true
	m.templateDetail.Ref.Type = aap.PlaybookTemplate
	for i := 0; i < 50; i++ {
		m.templateJobPage.Items = append(m.templateJobPage.Items, aap.JobSummary{Name: fmt.Sprintf("job%02d", i), Status: "running"})
	}
	m.selected = 49
	for _, size := range [][2]int{{80, 24}, {30, 10}, {1, 1}, {237, 62}} {
		m.width, m.height = size[0], size[1]
		assertFrameSize(t, m)
		if size[0] > 1 && !strings.Contains(m.View().Content, "> job49") && size[0] < 36 {
			t.Fatal("selected job hidden")
		}
	}
}
