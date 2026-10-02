package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func (m *Model) View() tea.View {
	l := m.layout()
	body := m.body(l.width, l.bodyHeight)
	if len(body) > l.bodyHeight {
		body = body[:l.bodyHeight]
	}
	// Cover the full frame, including rows vacated by searches and smaller pages.
	for len(body) < l.bodyHeight {
		body = append(body, "")
	}
	lines := append(l.header, body...)
	lines = append(lines, l.footer...)
	for i, line := range lines {
		line = ansi.Truncate(line, l.width, "")
		lines[i] = strings.Repeat(" ", l.padding) + line + strings.Repeat(" ", l.padding)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (m *Model) body(width, height int) []string {
	var lines []string
	rows := min(aap.OutputLines, height)
	start := max(0, m.selected-rows+1)
	switch m.screen {
	case mainScreen:
		labels := []string{"Job Templates", "Jobs", "Projects"}
		for i := start; i < min(len(labels), start+rows); i++ {
			lines = append(lines, m.selectRow(labels[i], i == m.selected, width))
		}
	case templatesScreen, projectsScreen:
		for i := start; i < min(len(m.templatePage.Items), start+rows); i++ {
			v := m.templatePage.Items[i]
			lines = append(lines, m.resourceRow(strconv.Itoa(v.Ref.ID), v.Name, string(v.Ref.Type), v.Status, i == m.selected, width))
		}
		if len(m.templatePage.Items) == 0 {
			resource := "templates"
			if m.screen == projectsScreen {
				resource = "projects"
			}
			lines = []string{m.emptyState(resource)}
		}
	case jobsScreen:
		for i := start; i < min(len(m.jobPage.Items), start+rows); i++ {
			v := m.jobPage.Items[i]
			lines = append(lines, m.resourceRow(strconv.Itoa(v.Ref.ID), v.Name, string(v.Ref.Type), string(v.Status), i == m.selected, width))
		}
		if len(m.jobPage.Items) == 0 {
			lines = []string{m.emptyState("jobs")}
		}
	case workflowScreen:
		for i := start; i < min(len(m.nodePage.Items), start+rows); i++ {
			v := m.nodePage.Items[i]
			lines = append(lines, m.resourceRow(v.Identifier, v.Name, v.Reason, string(v.Status), i == m.selected, width))
		}
		if len(m.nodePage.Items) == 0 {
			lines = []string{m.emptyState("workflow nodes")}
		}
	case templateScreen, jobScreen:
		lines = strings.Split(ansi.Hardwrap(strings.Join(m.details(), "\n"), width, true), "\n")
		top := min(m.detailTop, max(0, len(lines)-height))
		lines = lines[top:]
	case outputScreen:
		text := strings.TrimSuffix(m.outputUpdate.Chunk.Text, "\n")
		if text == "" {
			return nil
		}
		lines = strings.Split(text, "\n")
		// An in-flight update may have been sized before a resize or an error.
		// Following must continue to show the newest lines of that update.
		if m.following && len(lines) > height {
			lines = lines[len(lines)-height:]
		}
		for i, line := range lines {
			lines[i] = ansi.Cut(line, m.outputLeft, m.outputLeft+width)
		}
	}
	return lines
}

func (m *Model) emptyState(resource string) string {
	switch {
	case m.loading:
		return m.styles.muted.Render("Loading " + resource + "…")
	case m.err != nil:
		return m.styles.danger.Render("Unable to load " + resource + ".")
	default:
		return m.styles.muted.Render("No " + resource + ".")
	}
}

func (m *Model) selectRow(text string, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = "> "
		return m.styles.selected.Render(cell(marker+text, width))
	}
	return marker + text
}

type columns struct {
	id, name, kind, status int
}

func (m *Model) columns(width int) columns {
	c := columns{name: max(0, width-2)} // Leave room for the selection marker.
	if width >= 36 {
		c.id = 6
		c.name -= c.id + 1
	}
	if width >= 64 {
		c.kind = 22
		c.name -= c.kind + 1
	}
	if m.screen == jobsScreen || m.screen == workflowScreen || m.screen == projectsScreen {
		if width >= 20 {
			c.status = 10
			c.name -= c.status + 1
		}
	}
	return c
}

func (m *Model) columnHeader(width int) string {
	kind := "TYPE"
	if m.screen == workflowScreen {
		kind = "REASON"
	}
	return m.styles.muted.Render("  " + m.columns(width).render("ID", "NAME", kind, "STATUS"))
}

func (c columns) render(id, name, kind, status string) string {
	var cells []string
	for _, item := range []struct {
		text  string
		width int
	}{{id, c.id}, {name, c.name}, {kind, c.kind}, {status, c.status}} {
		if item.width > 0 {
			cells = append(cells, cell(item.text, item.width))
		}
	}
	return strings.Join(cells, " ")
}

func (m *Model) resourceRow(id, name, kind, status string, selected bool, width int) string {
	c := m.columns(width)
	if selected {
		return m.selectRow(c.render(id, singleLine(name), singleLine(kind), status), true, width)
	}
	return "  " + c.render(m.styles.muted.Render(id), singleLine(name), m.styles.muted.Render(singleLine(kind)), m.styles.status(status))
}

func cell(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

func singleLine(text string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(text)
}

func (m *Model) details() []string {
	var lines []string
	field := func(label, value string) {
		lines = append(lines, m.styles.muted.Render(label+": ")+value)
	}
	if m.screen == templateScreen {
		d := m.templateDetail
		lines = append(lines, m.styles.key.Render(fmt.Sprintf("%d · %s · %s", d.Ref.ID, d.Name, d.Ref.Type)), d.Description, "")
		field("Status", m.styles.status(d.Status))
		if d.Playbook != nil {
			field("Playbook", d.Playbook.Playbook)
			field("Job type", d.Playbook.JobType)
			field("Limit", d.Playbook.Limit)
		}
		if d.Project != nil {
			field("SCM", d.Project.SCMType)
			field("URL", d.Project.SCMURL)
			field("Branch", d.Project.SCMBranch)
		}
		if d.Inventory != nil {
			field("Source", d.Inventory.Source)
			field("Path", d.Inventory.SourcePath)
		}
		if d.SystemJobType != "" {
			field("System job", d.SystemJobType)
		}
		if d.ApprovalTimeout != nil {
			field("Approval timeout", fmt.Sprint(*d.ApprovalTimeout))
		}
		return lines
	}
	d := m.jobDetail
	lines = append(lines, m.styles.key.Render(fmt.Sprintf("%d · %s · %s", d.Ref.ID, d.Name, d.Ref.Type)), d.Description, "")
	field("Status", m.styles.status(string(d.Status))+fmt.Sprintf(" · elapsed %.1fs", d.Elapsed))
	field("Started", d.Started)
	field("Finished", d.Finished)
	if d.Playbook != nil {
		field("Playbook", d.Playbook.Playbook)
	}
	if d.Project != nil {
		field("SCM", d.Project.SCMType+" · "+d.Project.SCMBranch)
	}
	if d.Inventory != nil {
		field("Source", d.Inventory.Source)
	}
	if d.ModuleName != "" {
		field("Module", d.ModuleName+" "+d.ModuleArgs)
	}
	if d.SystemJobType != "" {
		field("System job", d.SystemJobType)
	}
	if d.ApprovalTimeout != nil {
		field("Approval timeout", fmt.Sprintf("%ds", *d.ApprovalTimeout))
	}
	lines = append(lines, "", m.styles.key.Render("Capabilities"))
	for _, item := range []struct {
		label string
		value aap.Capability
	}{{"Output", d.Capabilities.Output}, {"Cancel", d.Capabilities.Cancel}, {"Children", d.Capabilities.Children}} {
		style := m.styles.muted
		if item.value.State == aap.Available {
			style = m.styles.success
		}
		field(item.label, style.Render(string(item.value.State))+" "+item.value.Reason)
	}
	return lines
}
