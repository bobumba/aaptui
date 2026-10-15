package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

// TaskOutputSession keeps event paging separate from absolute stdout ranges.
type TaskOutputSession interface {
	Read(context.Context, int, int) (aap.TaskOutputPage, error)
	Next() (aap.TaskOutputUpdate, error)
	Stop()
	Close() error
}

func (m *Model) WithTaskOutput(open func(context.Context, aap.JobRef, aap.OutputFilter) TaskOutputSession) *Model {
	m.openTaskOutput = open
	return m
}

type taskLiveMsg struct {
	id     uint64
	update aap.TaskOutputUpdate
	err    error
}

type taskReadMsg struct {
	page  aap.TaskOutputPage
	older bool
}

func (m *Model) cancelOutputHistory() {
	if m.requestCancel != nil {
		m.requestCancel()
	}
	m.generation++
	m.loading = false
}

func (m *Model) selectOutputFilter(filter aap.OutputFilter) tea.Cmd {
	if filter == m.outputFilter {
		m.err = nil
		return nil
	}
	if filter != aap.OutputAll && (m.openTaskOutput == nil || (m.jobDetail.Ref.Type != aap.PlaybookJob && m.jobDetail.Ref.Type != aap.AdHocCommand)) {
		m.err = errors.New("task filters unavailable for this job; press A for All")
		return nil
	}
	if filter == aap.OutputAll && m.openOutput == nil {
		return nil
	}
	m.cancelOutputHistory()
	cleanup := m.closeOutput()
	m.outputFilter = filter
	m.outputUpdate = aap.OutputUpdate{}
	m.taskPage = aap.TaskOutputPage{}
	m.outputTop, m.outputLeft, m.taskTop = 0, 0, 0
	m.following, m.loading = true, true
	m.err = nil
	if filter == aap.OutputAll {
		m.output = m.openOutput(m.ctx, m.jobDetail.Ref)
		m.sessions = append(m.sessions, m.output)
		return tea.Batch(cleanup, m.nextOutput())
	}
	m.taskOutput = m.openTaskOutput(m.ctx, m.jobDetail.Ref, filter)
	m.sessions = append(m.sessions, m.taskOutput)
	return tea.Batch(cleanup, m.nextTaskOutput())
}

func (m *Model) nextTaskOutput() tea.Cmd {
	if m.taskOutput == nil || m.outputPending {
		return nil
	}
	session, id := m.taskOutput, m.outputID
	m.outputPending = true
	return func() tea.Msg {
		update, err := session.Next()
		return taskLiveMsg{id: id, update: update, err: err}
	}
}

func (m *Model) acceptTaskLive(msg taskLiveMsg) tea.Cmd {
	if msg.id != m.outputID || m.screen != outputScreen || m.taskOutput == nil || m.quitting {
		return nil
	}
	m.outputPending = false
	if m.following {
		m.loading = false
	}
	if msg.err != nil {
		m.err = msg.err
		return nil
	}
	m.err = msg.update.Err
	m.outputUpdate.Retrying = msg.update.Retrying
	if !msg.update.Retrying {
		m.outputUpdate.Status = msg.update.Status
		m.outputUpdate.Complete = msg.update.Complete
		m.outputUpdate.CompletionKnown = msg.update.CompletionKnown
		if m.following {
			m.taskPage = msg.update.Page
			m.taskTop = m.taskMaxTop()
		} else if msg.update.Page.Last > m.taskPage.Last {
			m.taskPage.Newer = true
		}
	}
	if !msg.update.Complete {
		return m.nextTaskOutput()
	}
	return nil
}

func (m *Model) taskMaxTop() int {
	return max(0, len(m.taskLines())-m.outputLines())
}

func (m *Model) taskLines() []string {
	if m.taskPage.Text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(m.taskPage.Text, "\n"), "\n")
}

func (m *Model) acceptTaskPage(msg taskReadMsg) {
	if msg.page.Text == "" {
		if msg.older {
			m.taskPage.Older = false
		} else {
			m.taskPage.Newer = false
		}
		return
	}
	m.taskPage, m.taskTop = msg.page, 0
	if msg.older {
		m.taskTop = m.taskMaxTop()
	}
}

func (m *Model) taskOutputKey(key string) (tea.Cmd, bool) {
	switch key {
	case "f", "r":
		m.cancelOutputHistory()
		if key == "f" {
			m.following = true
		}
		return m.nextTaskOutput(), true
	case "up", "k", "down", "j", "pgup", "pgdown", "ctrl+u", "ctrl+d":
		if m.loading {
			return nil, true
		}
		if m.following {
			m.taskTop = m.taskMaxTop()
		}
		m.following = false
		delta := 1
		if key == "pgup" || key == "pgdown" || key == "ctrl+u" || key == "ctrl+d" {
			delta = m.outputLines()
		}
		older := key == "up" || key == "k" || key == "pgup" || key == "ctrl+u"
		if older {
			delta = -delta
		}
		target := m.taskTop + delta
		before, after := 0, 0
		if target < 0 && m.taskPage.Older {
			before = m.taskPage.First
		}
		if target > m.taskMaxTop() && m.taskPage.Newer {
			after = m.taskPage.Last
		}
		if before == 0 && after == 0 {
			m.taskTop = max(0, min(m.taskMaxTop(), target))
			return nil, true
		}
		session := m.taskOutput
		return m.begin(func(ctx context.Context) (any, error) {
			page, err := session.Read(ctx, before, after)
			return taskReadMsg{page: page, older: older}, err
		}), true
	}
	return nil, false
}

func (m *Model) activeOutputFilter() aap.OutputFilter {
	if m.outputFilter == "" {
		return aap.OutputAll
	}
	return m.outputFilter
}
