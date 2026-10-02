package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

type fakeOutput struct {
	stopped, closed bool
	reads           []int
}

func (f *fakeOutput) Read(ctx context.Context, start, end int) (aap.OutputUpdate, error) {
	f.reads = append(f.reads, start)
	return aap.OutputUpdate{Chunk: aap.OutputChunk{Start: start, End: start + 1, AbsoluteEnd: 30, Text: "line\n"}}, nil
}
func (f *fakeOutput) Next(lines int) (aap.OutputUpdate, error) {
	u, err := f.Read(context.Background(), 0, lines)
	u.Cursor = aap.OutputCursor{Line: u.Chunk.End}
	return u, err
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
