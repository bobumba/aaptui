package tui

import (
	"context"
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

type TemplateJobsReader interface {
	TemplateJobs(context.Context, aap.TemplateRef, aap.ListOptions) (aap.Page[aap.JobSummary], error)
}

type templateJobsMsg struct{ page aap.Page[aap.JobSummary] }

func (m *Model) WithTemplateJobs(reader TemplateJobsReader) *Model { m.templateJobs = reader; return m }

func (m *Model) hasTemplateJobs() bool {
	return m.screen == templateScreen && m.templateJobs != nil && m.templateDetail.Ref.Type == aap.PlaybookTemplate
}

func (m *Model) loadTemplateJobs(cursor aap.PageCursor) tea.Cmd {
	ref, reader := m.templateDetail.Ref, m.templateJobs
	return m.begin(func(ctx context.Context) (any, error) {
		page, err := reader.TemplateJobs(ctx, ref, aap.ListOptions{Cursor: cursor})
		return templateJobsMsg{page}, err
	})
}

func templateJobColumns(width int) columns {
	c := columns{name: max(0, width-2)}
	if width >= 20 {
		c.status = 10
		c.name -= 11
	}
	if width >= 36 {
		c.id = 6
		c.name -= 7
	}
	if width >= 72 {
		c.kind = 20
		c.name -= 21
	}
	return c
}

func (m *Model) templateJobsBody(width, height int) []string {
	if height == 0 {
		return nil
	}
	details := m.details(width)
	// Reserve room for table headings and at least one selected job row.
	detailHeight := min(len(details), height/2, max(0, height-3))
	top := min(m.detailTop, max(0, len(details)-detailHeight))
	lines := append([]string{}, details[top:top+detailHeight]...)
	if height-len(lines) >= 3 {
		focus := "details"
		if m.templateJobsFocus {
			focus = "jobs"
		}
		paging := fmt.Sprintf("Jobs · %d results · Tab focus: %s", m.templateJobPage.Count, focus)
		if m.templateJobPage.Previous.Present() {
			paging += " · p previous"
		}
		if m.templateJobPage.Next.Present() {
			paging += " · n next"
		}
		lines = append(lines, m.styles.key.Render(paging))
		lines = append(lines, m.styles.muted.Render("  "+templateJobColumns(width).render("ID", "NAME", "STARTED", "STATUS")))
	}
	rows := height - len(lines)
	start := max(0, m.selected-rows+1)
	for i := start; i < min(len(m.templateJobPage.Items), start+rows); i++ {
		job := m.templateJobPage.Items[i]
		c := templateJobColumns(width)
		row := c.render(strconv.Itoa(job.Ref.ID), singleLine(job.Name), singleLine(job.Started), string(job.Status))
		if i == m.selected && m.templateJobsFocus {
			lines = append(lines, m.selectRow(row, true, width))
		} else {
			lines = append(lines, "  "+c.render(m.styles.muted.Render(strconv.Itoa(job.Ref.ID)), singleLine(job.Name), singleLine(job.Started), m.styles.status(string(job.Status))))
		}
	}
	if len(m.templateJobPage.Items) == 0 && rows > 0 {
		lines = append(lines, m.emptyState("jobs"))
	}
	return lines
}
