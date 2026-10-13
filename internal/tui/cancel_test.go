package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type fakeCancel struct {
	calls int
	err   error
}

func (f *fakeCancel) Cancel(ctx context.Context, ref aap.JobRef) (aap.CancellationResult, error) {
	f.calls++
	if f.err != nil {
		return aap.CancellationResult{}, f.err
	}
	return aap.CancellationResult{Requested: true, Job: aap.JobDetails{JobSummary: aap.JobSummary{Ref: ref, Status: "canceled"}}}, nil
}

func TestCancellationPromptFocusAndVisibility(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 10}, {20, 8}, {1, 1}} {
		f := &fakeCancel{}
		m := cancelModel(f)
		cleanupModel(t, m)
		m.width, m.height = size[0], size[1]
		m.Update(key("c"))
		assertFrameSize(t, m)
		view := ansi.Strip(m.View().Content)
		if size[0] >= 20 {
			for _, want := range []string{"Cancel job 1 (job)?", "demo", "y/Enter confirm", "n/Esc dismiss"} {
				if !strings.Contains(view, want) {
					t.Fatalf("missing %q: %q", want, view)
				}
			}
		}
		for _, k := range []string{"j", "o", "w", "r", "/", "tab", "c"} {
			_, cmd := m.Update(key(k))
			if cmd != nil || !m.confirming || m.detailTop != 0 || m.screen != jobScreen || m.editing {
				t.Fatalf("background action %q while confirming", k)
			}
		}
		m.Update(key("n"))
		if m.confirming || f.calls != 0 {
			t.Fatal("dismissal submitted cancellation")
		}
	}
}

func TestCancellationProgressDuplicateAndFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("cancel job: permission")} {
		f := &fakeCancel{err: failure}
		m := cancelModel(f)
		cleanupModel(t, m)
		m.Update(key("c"))
		_, cmd := m.Update(key("enter"))
		if !strings.Contains(ansi.Strip(m.View().Content), "Cancelling job") {
			t.Fatal("missing progress")
		}
		for _, k := range []string{"enter", "y", "c", "r"} {
			_, duplicate := m.Update(key(k))
			if duplicate != nil {
				t.Fatalf("duplicate command for %q", k)
			}
		}
		m.Update(cmd())
		if f.calls != 1 || m.operation != "" || m.loading || !errors.Is(m.err, failure) {
			t.Fatalf("unexpected result: %+v", m)
		}
		view := ansi.Strip(m.View().Content)
		if failure == nil && !strings.Contains(view, "Cancellation requested") {
			t.Fatal("missing success notice")
		}
		if failure != nil && !strings.Contains(view, failure.Error()) {
			t.Fatal("missing error")
		}
	}
}

func TestCancellationStaleResultAfterNavigation(t *testing.T) {
	f := &fakeCancel{}
	m := cancelModel(f)
	cleanupModel(t, m)
	m.stack = []frame{{screen: jobsScreen, job: m.jobDetail}}
	m.Update(key("c"))
	_, cmd := m.Update(key("y"))
	m.Update(key("esc"))
	m.Update(cmd())
	if m.screen != jobsScreen || m.jobDetail.Status != "running" || m.notice != "" {
		t.Fatal("stale cancellation result applied")
	}
}
func cancelModel(f *fakeCancel) *Model {
	m := New(context.Background(), "").WithCancellation(f)
	m.screen = jobScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}, Name: "demo", Status: "running"}, Capabilities: aap.JobCapabilities{Cancel: aap.Capability{State: aap.Available}}}
	return m
}
func TestCancellationConfirmation(t *testing.T) {
	f := &fakeCancel{}
	m := cancelModel(f)
	defer m.Close()
	m.Update(key("c"))
	if !m.confirming || f.calls != 0 {
		t.Fatal("cancel before confirmation")
	}
	m.Update(key("esc"))
	if m.confirming || f.calls != 0 || m.screen != jobScreen {
		t.Fatal("dismiss")
	}
	m.Update(key("c"))
	_, cmd := m.Update(key("y"))
	m.Update(cmd())
	if f.calls != 1 || m.jobDetail.Status != "canceled" {
		t.Fatal("confirmation")
	}
}
func TestNoMutationOnQuit(t *testing.T) {
	for _, quit := range []string{"q", "ctrl+c"} {
		f := &fakeCancel{}
		m := cancelModel(f)
		m.Update(key("c"))
		m.Update(key(quit))
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
		if f.calls != 0 || !m.quitting {
			t.Fatal("quit canceled server job")
		}
	}
}
