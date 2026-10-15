package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type fakeTaskOutput struct {
	latest          aap.TaskOutputUpdate
	page            aap.TaskOutputPage
	err             error
	reads           [][2]int
	stopped, closed bool
}

func (f *fakeTaskOutput) Next() (aap.TaskOutputUpdate, error) { return f.latest, f.err }
func (f *fakeTaskOutput) Read(ctx context.Context, before, after int) (aap.TaskOutputPage, error) {
	f.reads = append(f.reads, [2]int{before, after})
	return f.page, f.err
}
func (f *fakeTaskOutput) Stop()        { f.stopped = true }
func (f *fakeTaskOutput) Close() error { f.closed = true; return nil }

// Deliver one command/batch, leaving any scheduled future live poll unexecuted.
func deliverOutputCommand(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, command := range batch {
			deliverOutputCommand(t, m, command)
		}
		return
	}
	m.Update(msg)
}

func TestTaskFilterSwitchingAndReopening(t *testing.T) {
	var all []*fakeOutput
	var tasks []*fakeTaskOutput
	var filters []aap.OutputFilter
	m := New(context.Background(), "").WithOutput(func(context.Context, aap.JobRef) OutputSession {
		f := &fakeOutput{latest: &aap.OutputUpdate{Chunk: aap.OutputChunk{Text: "all stdout\n"}, Complete: true}}
		all = append(all, f)
		return f
	}).WithTaskOutput(func(ctx context.Context, ref aap.JobRef, filter aap.OutputFilter) TaskOutputSession {
		f := &fakeTaskOutput{latest: aap.TaskOutputUpdate{Page: aap.TaskOutputPage{Text: string(filter) + " result\n", First: 100, Last: 108, Older: true}, Complete: true, CompletionKnown: true}}
		tasks = append(tasks, f)
		filters = append(filters, filter)
		return f
	})
	cleanupModel(t, m)
	m.screen = jobScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 7, Type: aap.PlaybookJob}}, Capabilities: aap.JobCapabilities{Output: aap.Capability{State: aap.Available}}}
	press := func(k string) { t.Helper(); _, cmd := m.Update(key(k)); deliverOutputCommand(t, m, cmd) }
	press("o")
	if m.activeOutputFilter() != aap.OutputAll || m.outputUpdate.Chunk.Text != "all stdout\n" {
		t.Fatal("wrong default")
	}
	oldID := m.outputID
	press("C")
	if !all[0].stopped || !all[0].closed || m.output != nil || m.taskPage.Text != "Changed result\n" || m.loading || !m.following {
		t.Fatal("Changed switch failed")
	}
	m.Update(liveMsg{oldID, aap.OutputUpdate{Chunk: aap.OutputChunk{Text: "stale all\n"}}, nil})
	if m.taskPage.Text != "Changed result\n" {
		t.Fatal("stale All update applied")
	}
	_, history := m.Update(key("pgup"))
	staleHistory := history()
	oldID = m.outputID
	press("F")
	m.Update(staleHistory)
	m.Update(taskLiveMsg{id: oldID, update: aap.TaskOutputUpdate{Page: aap.TaskOutputPage{Text: "stale Changed\n"}}})
	if !tasks[0].stopped || !tasks[0].closed || m.taskPage.Text != "Failed result\n" || m.outputFilter != aap.OutputFailed {
		t.Fatal("Failed switch or stale rejection failed")
	}
	if len(filters) != 2 || filters[0] != aap.OutputChanged || filters[1] != aap.OutputFailed {
		t.Fatal(filters)
	}
	for _, width := range []int{40, 80, 180} {
		m.width, m.height = width, 24
		assertFrameSize(t, m)
		help := ansi.Strip(strings.Join(m.screenHelp(width), "\n"))
		for _, choice := range []string{"A All", "C Changed", "F Failed"} {
			if !strings.Contains(help, choice) {
				t.Fatal("missing filter help", help)
			}
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "Filter: Failed") {
			t.Fatal("missing active filter")
		}
	}
	press("A")
	if !tasks[1].closed || m.taskOutput != nil || m.outputUpdate.Chunk.Text != "all stdout\n" {
		t.Fatal("All restore failed")
	}
	press("C")
	press("esc")
	press("o")
	if m.outputFilter != aap.OutputAll || m.taskOutput != nil || !tasks[2].stopped || !tasks[2].closed {
		t.Fatal("reopening retained filter or leaked session")
	}
}

func TestTaskOutputScrollingPollingAndFollow(t *testing.T) {
	f := &fakeTaskOutput{page: aap.TaskOutputPage{Text: strings.Repeat("older diagnostic\n", 40), First: 10, Last: 18, Older: true, Newer: true}, latest: aap.TaskOutputUpdate{Page: aap.TaskOutputPage{Text: "latest\n", First: 200, Last: 208}, Complete: true, CompletionKnown: true}}
	m := New(context.Background(), "")
	cleanupModel(t, m)
	m.screen, m.taskOutput, m.outputFilter = outputScreen, f, aap.OutputChanged
	m.width, m.height = 80, 24
	m.taskPage = aap.TaskOutputPage{Text: strings.Repeat("diagnostic\n", 40), First: 100, Last: 108, Older: true}
	m.following = true
	_, cmd := m.Update(key("up"))
	if cmd != nil || m.following || m.taskTop != m.taskMaxTop()-1 {
		t.Fatal("line scrolling failed")
	}
	m.taskTop = 0
	_, cmd = m.Update(key("ctrl+u"))
	deliverOutputCommand(t, m, cmd)
	if len(f.reads) != 1 || f.reads[0] != ([2]int{100, 0}) || m.taskPage.First != 10 || m.taskTop != m.taskMaxTop() {
		t.Fatal("older page failed", f.reads, m.taskTop)
	}
	text, top := m.taskPage.Text, m.taskTop
	m.Update(taskLiveMsg{id: m.outputID, update: f.latest})
	if m.taskPage.Text != text || m.taskTop != top || !m.taskPage.Newer {
		t.Fatal("poll moved history")
	}
	f.page = aap.TaskOutputPage{Text: "newer\n", First: 100, Last: 108}
	_, cmd = m.Update(key("ctrl+d"))
	deliverOutputCommand(t, m, cmd)
	if f.reads[1] != ([2]int{0, 18}) || m.taskTop != 0 || m.taskPage.First != 100 {
		t.Fatal("newer page failed")
	}
	m.taskPage.Older = true
	_, history := m.Update(key("pgup"))
	stale := history()
	_, cmd = m.Update(key("f"))
	deliverOutputCommand(t, m, cmd)
	m.Update(stale)
	if !m.following || m.taskPage.Text != "latest\n" || m.outputFilter != aap.OutputChanged {
		t.Fatal("follow failed or stale history applied")
	}
	m.Update(key("q"))
	if m.ctx.Err() == nil {
		t.Fatal("quit did not cancel context")
	}
}

func TestTaskOutputEmptyErrorsAndUnsupported(t *testing.T) {
	f := &fakeTaskOutput{latest: aap.TaskOutputUpdate{Complete: true, CompletionKnown: true}}
	m := New(context.Background(), "").WithTaskOutput(func(context.Context, aap.JobRef, aap.OutputFilter) TaskOutputSession { return f })
	cleanupModel(t, m)
	m.width, m.height = 80, 24
	m.screen, m.taskOutput, m.outputFilter = outputScreen, f, aap.OutputFailed
	_, cmd := m.Update(key("r"))
	deliverOutputCommand(t, m, cmd)
	if !strings.Contains(ansi.Strip(m.View().Content), "No matching tasks") {
		t.Fatal("missing empty state")
	}
	f.err = &aap.APIError{Kind: aap.Permission, Operation: "Load task filter (press A for All)"}
	_, cmd = m.Update(key("r"))
	deliverOutputCommand(t, m, cmd)
	text := ansi.Strip(m.View().Content)
	if m.err == nil || strings.Contains(text, "No matching tasks") || !strings.Contains(text, "Unable to load matching tasks") {
		t.Fatal("failure shown as no matches", text)
	}
	f.err = errors.New("retry failed")
	m.jobDetail.Ref.Type = aap.SystemJob
	m.outputFilter = aap.OutputAll
	_, cmd = m.Update(key("C"))
	if cmd != nil || m.outputFilter != aap.OutputAll || m.err == nil {
		t.Fatal("unsupported filter accepted")
	}
}

func TestTaskHistoryEmptyPageKeepsPosition(t *testing.T) {
	m := New(context.Background(), "")
	cleanupModel(t, m)
	m.taskPage = aap.TaskOutputPage{Text: "current\n", First: 10, Last: 11, Older: true, Newer: true}
	m.acceptTaskPage(taskReadMsg{older: true})
	if m.taskPage.Text != "current\n" || m.taskPage.Older || !m.taskPage.Newer {
		t.Fatal("empty older page lost position")
	}
	m.acceptTaskPage(taskReadMsg{})
	if m.taskPage.Text != "current\n" || m.taskPage.Newer {
		t.Fatal("empty newer page lost position")
	}
}

func TestActiveOutputFilterVisibleOnNarrowScreen(t *testing.T) {
	m := New(context.Background(), "gateway")
	cleanupModel(t, m)
	m.screen, m.width, m.height, m.following = outputScreen, 40, 24, true
	for _, filter := range []aap.OutputFilter{aap.OutputAll, aap.OutputChanged, aap.OutputFailed} {
		m.outputFilter = filter
		if !strings.Contains(ansi.Strip(m.View().Content), "Filter: "+string(filter)) {
			t.Fatal("active filter hidden by unknown-completion state", filter)
		}
		assertFrameSize(t, m)
	}
}
