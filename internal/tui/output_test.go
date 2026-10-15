package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type fakeOutput struct {
	stopped, closed bool
	reads           []int
	latest          *aap.OutputUpdate
}

func (f *fakeOutput) Read(ctx context.Context, start, end int) (aap.OutputUpdate, error) {
	f.reads = append(f.reads, start)
	return aap.OutputUpdate{Chunk: aap.OutputChunk{Start: start, End: start + 1, AbsoluteEnd: 30, Text: "line\n"}}, nil
}
func (f *fakeOutput) Next(lines int) (aap.OutputUpdate, error) {
	if f.latest != nil {
		return *f.latest, nil
	}
	u, err := f.Read(context.Background(), 0, lines)
	u.Cursor = aap.OutputCursor{Line: u.Chunk.End}
	return u, err
}

func TestOutputOpensAtBottomAndPagesUp(t *testing.T) {
	f := &fakeOutput{latest: &aap.OutputUpdate{Chunk: aap.OutputChunk{Start: 999990, End: 1000000, AbsoluteEnd: 1000000, Text: strings.Repeat("latest\n", 10)}, Cursor: aap.OutputCursor{Line: 1000000}, Complete: true}}
	m := New(context.Background(), "").WithOutput(func(context.Context, aap.JobRef) OutputSession { return f })
	defer m.Close()
	m.screen = jobScreen
	m.jobDetail.Capabilities.Output.State = aap.Available
	_, cmd := m.Update(key("o"))
	if !m.loading {
		t.Fatal("opening did not show loading")
	}
	m.Update(cmd())
	if m.loading || !m.following || m.outputTop != 999990 || len(f.reads) != 0 {
		t.Fatal("opening replayed history or missed tail", m.outputTop, f.reads)
	}
	start := m.visibleOutputStart() - m.outputLines()
	_, cmd = m.Update(key("pgup"))
	m.Update(cmd())
	if m.following || len(f.reads) != 1 || f.reads[0] != start {
		t.Fatal("paging did not load earlier range", f.reads)
	}
}

func TestFollowDiscardsPendingHistory(t *testing.T) {
	f := &fakeOutput{latest: &aap.OutputUpdate{Chunk: aap.OutputChunk{Start: 90, End: 100, AbsoluteEnd: 100, Text: "latest\n"}, Cursor: aap.OutputCursor{Line: 100}, Complete: true}}
	m := New(context.Background(), "")
	defer m.Close()
	m.screen, m.output = outputScreen, f
	_, history := m.Update(key("pgup"))
	_, follow := m.Update(key("f"))
	if follow == nil || !m.following || m.loading {
		t.Fatal("follow did not resume")
	}
	m.Update(follow())
	m.Update(history())
	if m.outputUpdate.Chunk.Text != "latest\n" || m.outputTop != 90 {
		t.Fatal("stale history replaced latest output", m.outputUpdate)
	}
}

func TestLiveUpdatePreservesPendingHistoryAndCursor(t *testing.T) {
	f := &fakeOutput{}
	m := New(context.Background(), "")
	defer m.Close()
	m.screen, m.output = outputScreen, f
	m.outputTop = 10
	_, history := m.Update(key("pgup"))
	msg := history()
	m.Update(liveMsg{m.outputID, aap.OutputUpdate{Chunk: aap.OutputChunk{Start: 90, End: 100, AbsoluteEnd: 100, Text: "latest\n"}, Cursor: aap.OutputCursor{Line: 100}}, nil})
	if !m.loading || m.outputTop != 0 {
		t.Fatal("poll moved history or cleared its loading state")
	}
	m.Update(msg)
	if m.loading || m.outputUpdate.Chunk.Text != "line\n" || m.outputUpdate.Cursor.Line != 100 || m.outputUpdate.Chunk.AbsoluteEnd != 100 {
		t.Fatal("history lost latest cursor or end", m.outputUpdate)
	}
}
func (f *fakeOutput) Stop()        { f.stopped = true }
func (f *fakeOutput) Close() error { f.closed = true; return nil }
func TestOutputScrollAndLeave(t *testing.T) {
	f := &fakeOutput{}
	m := New(context.Background(), "").WithOutput(func(context.Context, aap.JobRef) OutputSession { return f })
	defer m.Close()
	m.screen = jobScreen
	m.jobDetail = aap.JobDetails{JobSummary: aap.JobSummary{Ref: aap.JobRef{ID: 1, Type: aap.PlaybookJob}}, Capabilities: aap.JobCapabilities{Output: aap.Capability{State: aap.Available}}}
	_, cmd := m.Update(key("o"))
	m.Update(cmd())
	if m.screen != outputScreen || len(f.reads) != 1 {
		t.Fatal("output")
	}
	_, cmd = m.Update(key("j"))
	m.Update(cmd())
	if f.reads[1] != 1 {
		t.Fatal("scroll")
	}
	_, cmd = m.Update(key("esc"))
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("leaving output did not schedule both cleanup and repaint")
	}
	for _, command := range batch {
		m.Update(command())
	}
	if m.screen != jobScreen || !f.stopped || !f.closed {
		t.Fatal("session not closed")
	}
}
func TestCollectWhilePausedAndIgnoreOldSession(t *testing.T) {
	f := &fakeOutput{}
	m := New(context.Background(), "").WithOutput(func(context.Context, aap.JobRef) OutputSession { return f })
	defer m.Close()
	m.screen = outputScreen
	m.output = f
	m.sessions = append(m.sessions, f)
	m.outputID = 10
	m.following = false
	m.outputUpdate = aap.OutputUpdate{Chunk: aap.OutputChunk{Text: "older\n"}}
	_, cmd := m.Update(liveMsg{10, aap.OutputUpdate{Chunk: aap.OutputChunk{Text: "new\n", AbsoluteEnd: 100}, Cursor: aap.OutputCursor{Line: 100}}, nil})
	if cmd == nil || m.outputUpdate.Chunk.Text != "older\n" || m.outputUpdate.Cursor.Line != 100 {
		t.Fatal("collection paused with scrolling")
	}
	m.Update(liveMsg{9, aap.OutputUpdate{Chunk: aap.OutputChunk{Text: "stale"}}, nil})
	if m.outputUpdate.Chunk.Text != "older\n" {
		t.Fatal("stale applied")
	}
	m.Update(key("q"))
	if !m.quitting || m.ctx.Err() == nil {
		t.Fatal("quit did not cancel")
	}
}

func TestOutputPageAliases(t *testing.T) {
	for _, binding := range []struct{ page, alias string }{{"pgup", "ctrl+u"}, {"pgdown", "ctrl+d"}} {
		for _, state := range []struct {
			name                        string
			top                         int
			following, loading, editing bool
		}{
			{name: "middle", top: 15},
			{name: "start"},
			{name: "end", top: 29},
			{name: "following", following: true},
			{name: "loading", top: 15, loading: true},
			{name: "editing", top: 15, editing: true},
		} {
			t.Run(binding.alias+"/"+state.name, func(t *testing.T) {
				var tops []int
				for _, input := range []string{binding.page, binding.alias} {
					f := &fakeOutput{}
					m := New(context.Background(), "")
					defer m.Close()
					m.screen, m.output = outputScreen, f
					m.outputTop, m.following = state.top, state.following
					m.loading, m.editing = state.loading, state.editing
					m.search = "keep"
					m.outputUpdate = aap.OutputUpdate{Chunk: aap.OutputChunk{Start: 10, AbsoluteEnd: 30, Text: "line\n"}}
					_, cmd := m.Update(key(input))
					if state.loading || state.editing {
						if cmd != nil || m.outputTop != state.top || m.following != state.following || m.search != "keep" {
							t.Fatal("paging changed loading/input state")
						}
					} else {
						if cmd == nil || m.following {
							t.Fatal("paging did not pause and read history")
						}
						cmd()
						if len(f.reads) != 1 || f.reads[0] != m.outputTop {
							t.Fatal("wrong history range")
						}
						start := state.top
						if state.following {
							start = 10
						}
						delta := m.outputLines()
						if binding.page == "pgup" {
							delta = -delta
						}
						if m.outputTop != max(0, min(29, start+delta)) {
							t.Fatal("wrong page distance or boundary")
						}
					}
					tops = append(tops, m.outputTop)
				}
				if tops[0] != tops[1] {
					t.Fatal("alias differs from page key")
				}
			})
		}
	}
}

func TestOutputPageAliasHelp(t *testing.T) {
	m := New(context.Background(), "")
	defer m.Close()
	m.screen = outputScreen
	for _, width := range []int{40, 80, 120} {
		help := ansi.Strip(strings.Join(m.screenHelp(width), "\n"))
		if !strings.Contains(help, "Ctrl-U") || !strings.Contains(help, "Ctrl-D") {
			t.Fatalf("missing aliases at width %d: %s", width, help)
		}
	}
}

func TestPageAliasesPreserveOtherScreenFocus(t *testing.T) {
	for s := mainScreen; s <= nodeScreen; s++ {
		if s == outputScreen {
			continue
		}
		m := New(context.Background(), "")
		defer m.Close()
		m.screen, m.detailTop, m.selected = s, 10, 1
		m.objectDetailsFocus, m.templateJobsFocus = true, true
		for _, input := range []string{"ctrl+u", "ctrl+d"} {
			_, cmd := m.Update(key(input))
			if cmd != nil || m.detailTop != 10 || m.selected != 1 || !m.objectDetailsFocus || !m.templateJobsFocus {
				t.Fatalf("alias changed focus or navigation on screen %d", s)
			}
		}
	}
}
