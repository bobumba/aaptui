package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"aaptui/internal/aap"
)

type fakeRunningJobs struct {
	ctx    context.Context
	update aap.RunningJobsUpdate
	err    error
}

func (f *fakeRunningJobs) Next() (aap.RunningJobsUpdate, error) {
	if f.ctx.Err() != nil {
		return aap.RunningJobsUpdate{}, f.ctx.Err()
	}
	return f.update, f.err
}

func TestRunningJobsHeaderUpdatesAndErrors(t *testing.T) {
	f := &fakeRunningJobs{update: aap.RunningJobsUpdate{Count: 237}}
	m := New(context.Background(), "gateway").WithIdentity("https://aap.example.com", fakeUser{}).WithRunningJobs(func(ctx context.Context) RunningJobsPoller { f.ctx = ctx; return f })
	cleanupModel(t, m)
	m.Update(m.loadUser()())
	if !strings.Contains(m.View().Content, "Running jobs: Loading") {
		t.Fatal("missing count loading state")
	}
	cmd := m.nextRunningJobs()
	if m.nextRunningJobs() != nil {
		t.Fatal("duplicate poll scheduled")
	}
	m.move(templatesScreen)
	_, cmd = m.Update(cmd())
	if m.runningCount != 237 || !m.runningKnown || cmd == nil {
		t.Fatal("count/navigation refresh failed")
	}
	for s := mainScreen; s <= hostsScreen; s++ {
		m.screen = s
		if !strings.Contains(m.View().Content, "Running jobs: 237") {
			t.Fatal("count missing on screen", s)
		}
		assertFrameSize(t, m)
	}
	f.update = aap.RunningJobsUpdate{Err: &aap.APIError{Kind: aap.Connection, Operation: "count running jobs"}}
	_, cmd = m.Update(cmd())
	if cmd == nil || !strings.Contains(m.View().Content, "237 (stale)") {
		t.Fatal("failed refresh concealed")
	}
	f.update = aap.RunningJobsUpdate{Count: 0}
	_, cmd = m.Update(cmd())
	if !strings.Contains(m.View().Content, "Running jobs: 0") || m.runningErr != nil {
		t.Fatal("zero/recovery missing")
	}
	f.err = &aap.APIError{Kind: aap.Authentication, Operation: "count running jobs"}
	_, cmd = m.Update(cmd())
	if cmd != nil || !strings.Contains(m.View().Content, "0 (stale)") || m.err != nil {
		t.Fatal("permanent failure did not stop header polling")
	}
	m.runningKnown = false
	if !strings.Contains(m.View().Content, "Running jobs: Unavailable") {
		t.Fatal("unknown count shown as zero")
	}
}

func TestRunningJobsHeaderStopsOnQuitAndClose(t *testing.T) {
	for _, action := range []string{"q", "ctrl+c", "close"} {
		t.Run(action, func(t *testing.T) {
			f := &fakeRunningJobs{}
			m := New(context.Background(), "").WithRunningJobs(func(ctx context.Context) RunningJobsPoller { f.ctx = ctx; return f })
			cleanupModel(t, m)
			cmd := m.nextRunningJobs()
			if action == "close" {
				if err := m.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				m.Update(key(action))
			}
			msg := cmd().(runningJobsMsg)
			if !errors.Is(msg.err, context.Canceled) {
				t.Fatal("poll context survived shutdown", msg.err)
			}
			_, next := m.Update(msg)
			if next != nil || m.runningKnown || m.nextRunningJobs() != nil {
				t.Fatal("polling restarted after shutdown")
			}
		})
	}
}

type fakeUser struct{ err error }

func (f fakeUser) Me(context.Context) (aap.User, error) {
	return aap.User{ID: 1, Username: "alice"}, f.err
}

func TestIdentityHeaderIndependentOfNavigation(t *testing.T) {
	m := New(context.Background(), "gateway").WithIdentity("https://aap.example.com", fakeUser{})
	cleanupModel(t, m)
	if !strings.Contains(m.View().Content, "User: Loading") {
		t.Fatal("missing loading state")
	}
	cmd := m.loadUser()
	m.move(templatesScreen)
	m.Update(cmd())
	if m.userLoading || m.userErr != nil || m.loading {
		t.Fatal("identity result affected screen loading")
	}
	for s := mainScreen; s <= hostsScreen; s++ {
		m.screen = s
		view := m.View().Content
		if !strings.Contains(view, "https://aap.example.com") || !strings.Contains(view, "User: alice") {
			t.Fatal("missing identity on screen", s)
		}
		assertFrameSize(t, m)
	}
}

func TestIdentityHeaderFailureAndQuit(t *testing.T) {
	m := New(context.Background(), "").WithIdentity("https://aap.example.com", fakeUser{err: &aap.APIError{Kind: aap.Authentication, Operation: "read user"}})
	cleanupModel(t, m)
	m.Update(m.loadUser()())
	if !strings.Contains(m.View().Content, "Unavailable (read user: authentication)") || m.err != nil || m.quitting {
		t.Fatal("identity failure not shown safely", m.View().Content)
	}
	m.Update(key("q"))
	m.Update(userMsg{user: aap.User{Username: "late"}})
	if m.user.Username == "late" {
		t.Fatal("late result applied after quitting")
	}
}
