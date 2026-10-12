package tui

import (
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
		labels := []string{"Job Templates", "Jobs", "Projects", "Inventories"}
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
	case inventoriesScreen:
		for i := start; i < min(len(m.inventoryPage.Items), start+rows); i++ {
			v := m.inventoryPage.Items[i]
			kind := v.Kind
			if kind == "" {
				kind = "inventory"
			}
			lines = append(lines, m.resourceRow(strconv.Itoa(v.ID), v.Name, kind, "", i == m.selected, width))
		}
		if len(m.inventoryPage.Items) == 0 {
			lines = []string{m.emptyState("inventories")}
		}
	case inventoryScreen, groupScreen:
		return m.inventoryDetailsBody(width, height)
	case groupsScreen:
		for i := start; i < min(len(m.groupPage.Items), start+rows); i++ {
			v := m.groupPage.Items[i]
			lines = append(lines, m.resourceRow(strconv.Itoa(v.ID), v.Name, "group", "", i == m.selected, width))
		}
		if len(m.groupPage.Items) == 0 {
			lines = []string{m.emptyState("groups")}
		}
	case hostsScreen:
		for i := start; i < min(len(m.hostPage.Items), start+rows); i++ {
			v := m.hostPage.Items[i]
			status := "unknown"
			if v.Enabled != nil {
				status = "disabled"
				if *v.Enabled {
					status = "enabled"
				}
			}
			lines = append(lines, m.resourceRow(strconv.Itoa(v.ID), v.Name, "host", status, i == m.selected, width))
		}
		if len(m.hostPage.Items) == 0 {
			lines = []string{m.emptyState("hosts")}
		}
	case jobsScreen:
		return m.jobsBody(width, height)
	case workflowScreen:
		for i := start; i < min(len(m.nodePage.Items), start+rows); i++ {
			v := m.nodePage.Items[i]
			lines = append(lines, m.resourceRow(v.Identifier, v.Name, v.Reason, string(v.Status), i == m.selected, width))
		}
		if len(m.nodePage.Items) == 0 {
			lines = []string{m.emptyState("workflow nodes")}
		}
	case templateScreen, jobScreen, hostScreen, nodeScreen:
		if m.hasTemplateJobs() {
			return m.templateJobsBody(width, height)
		}
		lines = m.details(width)
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
	if m.screen == jobsScreen || m.screen == workflowScreen || m.screen == projectsScreen || m.screen == hostsScreen {
		if width >= 20 {
			c.status = 10
			c.name -= c.status + 1
		}
	}
	return c
}

func (m *Model) columnHeader(width int) string {
	if m.screen == jobsScreen && width >= 120 {
		c := m.columns(width - 2*(jobTimestampWidth+1))
		return m.styles.muted.Render("  " + c.render("ID", "NAME", "TYPE", "STATUS") + " " + cell("STARTED (UTC)", jobTimestampWidth) + " " + cell("FINISHED (UTC)", jobTimestampWidth))
	}
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
