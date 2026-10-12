package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

type UserReader interface {
	Me(context.Context) (aap.User, error)
}

type userMsg struct {
	user aap.User
	err  error
}

type RunningJobsPoller interface {
	Next() (aap.RunningJobsUpdate, error)
}

type runningJobsMsg struct {
	update aap.RunningJobsUpdate
	err    error
}

func (m *Model) WithRunningJobs(open func(context.Context) RunningJobsPoller) *Model {
	m.runningJobs = open(m.ctx)
	return m
}

func (m *Model) nextRunningJobs() tea.Cmd {
	if m.runningJobs == nil || m.runningPending || m.quitting || m.ctx.Err() != nil {
		return nil
	}
	m.runningPending = true
	poller := m.runningJobs
	return func() tea.Msg { update, err := poller.Next(); return runningJobsMsg{update, err} }
}

func (m *Model) WithIdentity(url string, reader UserReader) *Model {
	m.serverURL = url
	m.userReader = reader
	m.userLoading = reader != nil
	return m
}

func (m *Model) loadUser() tea.Cmd {
	if m.userReader == nil {
		return nil
	}
	ctx, reader := m.ctx, m.userReader
	return func() tea.Msg {
		user, err := reader.Me(ctx)
		return userMsg{user, err}
	}
}

func (m *Model) connectionHeader() []string {
	lines := []string{m.styles.muted.Render(m.connection)}
	if m.serverURL != "" {
		lines = append(lines, m.styles.muted.Render("AAP URL: "+singleLine(m.serverURL)))
	}
	if m.userReader != nil {
		user := m.user.Username
		switch {
		case m.userLoading:
			user = "Loading…"
		case m.userErr != nil:
			user = fmt.Sprintf("Unavailable (%s)", m.userErr)
		}
		lines = append(lines, m.styles.muted.Render("User: "+singleLine(user)))
	}
	if m.runningJobs != nil {
		count := "Loading…"
		if m.runningKnown {
			count = fmt.Sprint(m.runningCount)
		}
		if m.runningErr != nil {
			if m.runningKnown {
				count += " (stale)"
			} else {
				count = "Unavailable"
			}
			count += fmt.Sprintf(" · %s", m.runningErr)
		}
		lines = append(lines, m.styles.muted.Render("Running jobs: "+singleLine(count)))
	}
	return lines
}
