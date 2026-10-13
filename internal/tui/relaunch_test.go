package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type fakeRelaunch struct {
	calls  int
	ref    aap.JobRef
	err    error
	source aap.JobDetails
}

func (f *fakeRelaunch) Relaunch(ctx context.Context, ref aap.JobRef) (aap.RelaunchResult, error) {
	f.calls++
	f.ref = ref
	if err := ctx.Err(); err != nil {
		return aap.RelaunchResult{}, err
	}
	return aap.RelaunchResult{Requested: f.err == nil, Source: f.source, Job: aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 2, Type: ref.Type}, Name: "new execution"}}}, f.err
}

func relaunchModel(t *testing.T, f *fakeRelaunch, reader JobReader) *Model {
	t.Helper()
	m := New(context.Background(), "").WithJobs(reader).WithRelaunch(f).WithCancellation(&fakeCancel{})
	cleanupModel(t, m)
	m.screen = jobScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "nightly deploy", Status: "successful"}, Capabilities: aap.JobCapabilities{Relaunch: aap.Capability{State: aap.Available}, Cancel: aap.Capability{State: aap.Available}}}
	f.source = m.jobDetail
	m.detailTop = 3
	m.stack = []frame{{screen: jobsScreen, selected: 4, search: "deploy"}}
	return m
}

func TestRelaunchConfirmationNavigationAndProgress(t *testing.T) {
	f := &fakeRelaunch{}
	reader := &fakeJobs{}
	m := relaunchModel(t, f, reader)
	m.Update(key("R"))
	if !m.confirming || !m.confirmRelaunch || f.calls != 0 {
		t.Fatal("relaunch submitted without confirmation")
	}
	for _, size := range [][2]int{{80, 24}, {30, 10}, {20, 8}, {1, 1}} {
		m.width, m.height = size[0], size[1]
		assertFrameSize(t, m)
		if size[0] >= 30 {
			view := ansi.Strip(m.View().Content)
			for _, text := range []string{"Relaunch job 1 (job)?", "nightly deploy", "Creates a new execution", "y/Enter confirm", "n/Esc dismiss"} {
				if !strings.Contains(view, text) {
					t.Fatalf("missing %q: %q", text, view)
				}
			}
		}
	}
	m.width, m.height = 80, 24
	for _, k := range []string{"r", "R", "c", "o", "w", "j", "tab", "/"} {
		if _, cmd := m.Update(key(k)); cmd != nil || !m.confirmRelaunch || m.detailTop != 3 || m.screen != jobScreen {
			t.Fatalf("background action %q while confirming", k)
		}
	}
	_, cmd := m.Update(key("enter"))
	if cmd == nil || !strings.Contains(m.View().Content, "Relaunching job") {
		t.Fatal("missing progress")
	}
	for _, k := range []string{"enter", "y", "R", "r", "c"} {
		if _, duplicate := m.Update(key(k)); duplicate != nil {
			t.Fatalf("duplicate command for %q", k)
		}
	}
	_, load := m.Update(cmd())
	if f.calls != 1 || f.ref != f.source.Ref || load == nil || m.jobDetail.Ref.ID != 2 || len(m.stack) != 2 || m.stack[1].job.Ref.ID != 1 {
		t.Fatal("new job or source context lost")
	}
	m.Update(load())
	if m.jobDetail.Ref.ID != 2 || !strings.Contains(m.View().Content, "Relaunched as job 2") || len(reader.requests) != 1 || reader.requests[0].ID != 2 {
		t.Fatal("new job not loaded")
	}
	m.Update(key("esc"))
	if m.jobDetail.Ref.ID != 1 || m.detailTop != 3 || m.screen != jobScreen {
		t.Fatal("source context not restored")
	}
	m.Update(key("esc"))
	if m.screen != jobsScreen || m.selected != 4 || m.search != "deploy" {
		t.Fatal("original list context not restored")
	}
}

func TestRelaunchDismissAndQuit(t *testing.T) {
	for _, k := range []string{"n", "esc", "q", "ctrl+c"} {
		f := &fakeRelaunch{}
		m := relaunchModel(t, f, &fakeJobs{})
		m.Update(key("R"))
		m.Update(key(k))
		if f.calls != 0 || m.confirming || m.confirmRelaunch || (k == "n" || k == "esc") && m.jobDetail.Ref.ID != 1 {
			t.Fatalf("dismissal %s mutated state", k)
		}
	}
}

func TestRelaunchUnavailableAndFocus(t *testing.T) {
	for _, state := range []aap.CapabilityState{aap.Unavailable, aap.Unknown, ""} {
		f := &fakeRelaunch{}
		m := relaunchModel(t, f, &fakeJobs{})
		m.jobDetail.Capabilities.Relaunch = aap.Capability{State: state, Reason: "Requires credential passwords"}
		_, cmd := m.Update(key("R"))
		if cmd != nil || m.confirming || f.calls != 0 || m.err == nil || !strings.Contains(m.View().Content, "Requires credential passwords") {
			t.Fatal("unavailable action was not explained")
		}
	}
	for _, screen := range []screen{jobsScreen, templateScreen, workflowScreen, outputScreen} {
		f := &fakeRelaunch{}
		m := relaunchModel(t, f, &fakeJobs{})
		m.screen = screen
		_, cmd := m.Update(key("R"))
		if cmd != nil || m.confirming || f.calls != 0 {
			t.Fatal("relaunch outside job details")
		}
	}
	f := &fakeRelaunch{}
	m := relaunchModel(t, f, &fakeJobs{})
	m.loading = true
	m.Update(key("R"))
	if m.confirming {
		t.Fatal("relaunch while loading")
	}
	m.loading = false
	m.editing = true
	m.Update(key("R"))
	if m.confirming || m.search != "R" {
		t.Fatal("relaunch consumed search text")
	}
	for _, width := range []int{30, 80} {
		m.width = width
		m.editing = false
		if !strings.Contains(ansi.Strip(strings.Join(m.screenHelp(width), "\n")), "R relaunch") {
			t.Fatal("missing relaunch help")
		}
	}
}

func TestRelaunchFailureAndStaleResults(t *testing.T) {
	for _, navigate := range []bool{false, true} {
		f := &fakeRelaunch{err: &aap.APIError{Kind: aap.Connection, Operation: "Relaunch outcome unknown; refresh Jobs before retrying"}}
		m := relaunchModel(t, f, &fakeJobs{})
		m.Update(key("R"))
		_, cmd := m.Update(key("y"))
		result := cmd()
		if navigate {
			m.Update(key("esc"))
		}
		m.Update(result)
		if navigate {
			if m.screen != jobsScreen || m.err != nil || m.notice != "" {
				t.Fatal("stale result applied")
			}
		} else if m.screen != jobScreen || m.jobDetail.Ref.ID != 1 || !errors.Is(m.err, f.err) || !strings.Contains(m.View().Content, "refresh Jobs") {
			t.Fatal("missing uncertain outcome")
		}
	}
	// A successful mutation result arriving after back navigation must also be ignored.
	f := &fakeRelaunch{}
	m := relaunchModel(t, f, &fakeJobs{})
	m.Update(key("R"))
	_, cmd := m.Update(key("y"))
	result := cmd()
	m.Update(key("esc"))
	m.Update(result)
	if m.screen != jobsScreen || len(m.stack) != 0 || m.notice != "" {
		t.Fatal("stale success navigated into new job")
	}
}

func TestRelaunchNewDetailsFailureRetainsIdentity(t *testing.T) {
	f := &fakeRelaunch{}
	m := relaunchModel(t, f, &inaccessibleJobs{})
	m.Update(key("R"))
	_, cmd := m.Update(key("y"))
	_, load := m.Update(cmd())
	m.Update(load())
	if m.jobDetail.Ref.ID != 2 || m.err == nil || m.screen != jobScreen || !strings.Contains(m.View().Content, "Relaunched as job 2") {
		t.Fatal("created job identity lost on detail failure")
	}
}
