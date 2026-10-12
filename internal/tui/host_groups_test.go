package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type fakeHostGroups struct {
	ctx  context.Context
	id   int
	page aap.Page[aap.GroupSummary]
	err  error
}

func (f *fakeHostGroups) HostGroups(ctx context.Context, id int, o aap.ListOptions) (aap.Page[aap.GroupSummary], error) {
	f.ctx, f.id = ctx, id
	page := f.page
	page.Items = append([]aap.GroupSummary(nil), page.Items...)
	return page, f.err
}

// Execute batches and their resulting commands as the event loop would.
func runHostCommand(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, command := range batch {
			runHostCommand(m, command)
		}
		return
	}
	_, next := m.Update(msg)
	runHostCommand(m, next)
}

func hostGroupText(m *Model) string {
	return normalizedDetailText(strings.Join(m.details(110), "\n"))
}

func TestHostGroupStatesAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"empty", nil, "No visible groups."},
		{"permission", &aap.APIError{Kind: aap.Permission}, "Permission denied"},
		{"missing", &aap.APIError{Kind: aap.Missing}, "Unavailable"},
		{"unsupported", &aap.APIError{Kind: aap.Unsupported}, "Unavailable"},
		{"authentication", &aap.APIError{Kind: aap.Authentication}, "Authentication failed"},
		{"temporary", &aap.APIError{Kind: aap.Temporary}, "Unable to load groups"},
		{"unsafe error", errors.New("secret internal URL"), "Unable to load groups"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeHostGroups{err: tc.err}
			m := New(context.Background(), "").WithHostGroups(f)
			cleanupModel(t, m)
			m.screen = hostsScreen
			m.hostPage.Items = []aap.HostSummary{{ID: 3, Name: "web-1", InventoryID: 1}}
			_, cmd := m.Update(key("enter"))
			if cmd == nil || !m.hostGroupsLoading || !strings.Contains(hostGroupText(m), "Loading…") {
				t.Fatal("missing loading state")
			}
			runHostCommand(m, cmd)
			text := hostGroupText(m)
			if !strings.Contains(text, tc.want) || !strings.Contains(text, "web-1") || strings.Contains(text, "secret") || m.hostGroupsLoading || f.id != 3 {
				t.Fatalf("wrong membership state: %s", text)
			}
			if tc.err != nil && strings.Contains(text, "No visible groups.") {
				t.Fatal("failure presented as empty membership")
			}
			f.err = nil
			f.page = aap.Page[aap.GroupSummary]{Count: 1, Items: []aap.GroupSummary{{ID: 7, Name: "parent"}}}
			m.detailTop = 2
			_, cmd = m.Update(key("r"))
			runHostCommand(m, cmd)
			if m.hostGroupsErr != nil || !m.hostGroupsKnown || m.detailTop != 2 || !strings.Contains(hostGroupText(m), "7 · parent") {
				t.Fatal("refresh did not recover or lost scroll position")
			}
		})
	}
	m := New(context.Background(), "")
	cleanupModel(t, m)
	m.screen = hostScreen
	if !strings.Contains(hostGroupText(m), "Membership: Unavailable") {
		t.Fatal("missing reader shown as empty")
	}
}

func TestHostGroupsIndependentOfDetails(t *testing.T) {
	for _, groupsFirst := range []bool{false, true} {
		m := New(context.Background(), "").WithObjectDetails(&fakeObjectDetails{err: &aap.APIError{Kind: aap.Permission}}).
			WithHostGroups(&fakeHostGroups{page: aap.Page[aap.GroupSummary]{Count: 1, Items: []aap.GroupSummary{{ID: 7, Name: "parent"}}}})
		cleanupModel(t, m)
		m.screen = hostsScreen
		m.hostPage.Items = []aap.HostSummary{{ID: 3, Name: "web-1"}}
		_, cmd := m.Update(key("enter"))
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) != 2 {
			t.Fatal("detail and membership reads are not independent")
		}
		if groupsFirst {
			batch[0], batch[1] = batch[1], batch[0]
		}
		runHostCommand(m, batch[0])
		if groupsFirst {
			if !m.loading || m.hostGroupsLoading || !m.hostGroupsKnown {
				t.Fatal("groups result changed detail loading")
			}
		} else if m.loading || !m.hostGroupsLoading {
			t.Fatal("detail result changed group loading")
		}
		runHostCommand(m, batch[1])
		if m.err == nil || m.hostGroupsErr != nil || !strings.Contains(hostGroupText(m), "7 · parent") || m.hostDetail.Name != "web-1" {
			t.Fatal("one result replaced the other")
		}
	}
}

func TestHostGroupsCancelAndIgnoreStaleResults(t *testing.T) {
	for _, action := range []string{"esc", "switch host", "refresh", "q", "close"} {
		t.Run(action, func(t *testing.T) {
			f := &fakeHostGroups{page: aap.Page[aap.GroupSummary]{Count: 1, Items: []aap.GroupSummary{{ID: 7, Name: "old group"}}}}
			m := New(context.Background(), "").WithHostGroups(f)
			cleanupModel(t, m)
			m.screen, m.search, m.selected = hostsScreen, "web", 1
			m.hostPage.Items = []aap.HostSummary{{ID: 2, Name: "host-2"}, {ID: 3, Name: "host-3"}}
			_, cmd := m.Update(key("enter"))
			// Start the request, then deliver its result after navigation or refresh.
			stale := cmd().(hostGroupsMsg)
			oldCtx := f.ctx
			// loadHostGroups closes its context when work finishes; also check
			// cancellation of an outstanding request before executing it below.
			_, pending := m.Update(key("r"))
			switch action {
			case "esc":
				m.Update(key("esc"))
			case "switch host":
				m.Update(key("esc"))
				m.Update(key("k"))
				_, next := m.Update(key("enter"))
				f.page.Items[0].Name = "new group"
				runHostCommand(m, next)
			case "refresh":
				_, next := m.Update(key("r"))
				f.page.Items[0].Name = "new group"
				runHostCommand(m, next)
			case "q":
				m.Update(key("q"))
			case "close":
				if err := m.Close(); err != nil {
					t.Fatal(err)
				}
			}
			m.Update(stale)
			m.Update(pending())
			if f.ctx.Err() != context.Canceled || oldCtx.Err() != context.Canceled {
				t.Fatal("request was not canceled")
			}
			if strings.Contains(hostGroupText(m), "old group") || m.hostGroupsErr != nil {
				t.Fatal("stale response applied")
			}
			if action == "esc" && (m.screen != hostsScreen || m.search != "web" || m.selected != 1 || m.hostGroupsLoading) {
				t.Fatal("back context lost")
			}
			if action == "switch host" && (m.hostDetail.ID != 2 || !strings.Contains(hostGroupText(m), "new group")) {
				t.Fatal("host switch did not show new membership")
			}
		})
	}
}

func TestHostGroupsResponsiveScrolling(t *testing.T) {
	for _, width := range []int{1, 30, 80, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := New(context.Background(), "")
			cleanupModel(t, m)
			m.screen, m.width, m.height = hostScreen, width, 24
			m.hostGroupsKnown = true
			for id := 1; id <= 60; id++ {
				m.hostGroupPage.Items = append(m.hostGroupPage.Items, aap.GroupSummary{ID: id, Name: fmt.Sprintf("部署 café group-%d", id)})
			}
			m.hostGroupPage.Count = len(m.hostGroupPage.Items)
			for _, line := range m.details(max(1, width-2)) {
				if width > 1 && ansi.StringWidth(line) > max(1, width-2) {
					t.Fatal("group rows exceed width")
				}
			}
			text := strings.Join(m.details(110), "\n")
			if !strings.Contains(text, "部署 café group-60") {
				t.Fatal("last membership missing")
			}
			before := m.View().Content
			for i := 0; i < 200; i++ {
				m.Update(key("j"))
			}
			assertFrameSize(t, m)
			if m.View().Content == before || m.selected != 0 {
				t.Fatal("groups inaccessible through scrolling")
			}
			if width >= 30 && !strings.Contains(ansi.Strip(m.View().Content), "group-60") {
				t.Fatal("last membership inaccessible")
			}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			if m.detailTop != 200 {
				t.Fatal("resize lost scroll position")
			}
		})
	}
}

type blockingHostGroups struct{ started chan context.Context }

func (f *blockingHostGroups) HostGroups(ctx context.Context, id int, o aap.ListOptions) (aap.Page[aap.GroupSummary], error) {
	f.started <- ctx
	<-ctx.Done()
	return aap.Page[aap.GroupSummary]{}, ctx.Err()
}

func TestHostGroupsCancelInFlight(t *testing.T) {
	for _, action := range []string{"esc", "r", "q", "close"} {
		t.Run(action, func(t *testing.T) {
			f := &blockingHostGroups{started: make(chan context.Context, 1)}
			m := New(context.Background(), "").WithHostGroups(f)
			cleanupModel(t, m)
			m.screen = hostsScreen
			m.hostPage.Items = []aap.HostSummary{{ID: 3}}
			_, cmd := m.Update(key("enter"))
			done := make(chan tea.Msg, 1)
			go func() { done <- cmd() }()
			ctx := <-f.started
			if action == "close" {
				if err := m.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				m.Update(key(action))
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("membership request not canceled")
			}
			select {
			case msg := <-done:
				m.Update(msg)
				if m.hostGroupsErr != nil || m.hostGroupsKnown {
					t.Fatal("canceled result applied")
				}
			case <-time.After(time.Second):
				t.Fatal("membership command did not stop")
			}
		})
	}
}
