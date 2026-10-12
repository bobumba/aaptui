package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

// jobTimestamp uses a fixed timezone so list and detail views remain comparable.
func jobTimestamp(value string) string {
	if value == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "Unavailable"
	}
	return t.UTC().Format("2006-01-02 15:04:05 MST")
}

const jobTimestampWidth = 23

func (m *Model) jobListRow(job aap.JobSummary, selected bool, width int) []string {
	if width >= 120 {
		c := m.columns(width - 2*(jobTimestampWidth+1))
		id, kind, status := strconv.Itoa(job.Ref.ID), string(job.Ref.Type), string(job.Status)
		if !selected {
			id = m.styles.muted.Render(id)
			kind = m.styles.muted.Render(kind)
			status = m.styles.status(status)
		}
		row := c.render(id, singleLine(job.Name), kind, status) +
			" " + cell(jobTimestamp(job.Started), jobTimestampWidth) + " " + cell(jobTimestamp(job.Finished), jobTimestampWidth)
		return []string{m.selectRow(row, selected, width)}
	}
	lines := []string{m.resourceRow(strconv.Itoa(job.Ref.ID), job.Name, string(job.Ref.Type), string(job.Status), selected, width)}
	timestamps := "Started: " + jobTimestamp(job.Started) + " · Finished: " + jobTimestamp(job.Finished)
	for _, line := range strings.Split(ansi.Hardwrap(timestamps, max(1, width-2), true), "\n") {
		lines = append(lines, "  "+line)
	}
	return lines
}

func (m *Model) jobsBody(width, height int) []string {
	if len(m.jobPage.Items) == 0 {
		return []string{m.emptyState("jobs")}
	}
	if height == 0 {
		return nil
	}
	selected := min(m.selected, len(m.jobPage.Items)-1)
	start := selected
	used := len(m.jobListRow(m.jobPage.Items[selected], true, width))
	for start > 0 {
		previous := len(m.jobListRow(m.jobPage.Items[start-1], false, width))
		if used+previous > height {
			break
		}
		used += previous
		start--
	}
	var lines []string
	for i := start; i < len(m.jobPage.Items) && len(lines) < height; i++ {
		lines = append(lines, m.jobListRow(m.jobPage.Items[i], i == selected, width)...)
	}
	return lines
}
