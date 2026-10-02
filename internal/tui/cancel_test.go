package tui

import (
	"context"
	"testing"

	"aaptui/internal/aap"
)

type fakeCancel struct{ calls int }

func (f *fakeCancel) Cancel(ctx context.Context, ref aap.JobRef) (aap.CancellationResult, error) {
	f.calls++
	return aap.CancellationResult{Requested: true, Job: aap.JobDetails{JobSummary: aap.JobSummary{Ref: ref, Status: "canceled"}}}, nil
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
