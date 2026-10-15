package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type layout struct {
	width, bodyHeight, padding int
	header, footer             []string
}

// layout measures the same chrome for rendering, scrolling, and output requests.
// Loading occupies the title row rather than changing the output window size.
func (m *Model) layout() layout {
	l := layout{}
	if m.width >= 40 {
		l.padding = 1
	}
	l.width = max(1, m.width-2*l.padding)
	l.footer = m.footer(l.width)
	if len(l.footer) > m.height {
		l.footer = l.footer[len(l.footer)-m.height:]
	}
	l.header = m.header(l.width)
	available := max(0, m.height-len(l.footer))
	// On small terminals, give the body a row before spending space on chrome.
	headerHeight := min(len(l.header), max(0, available-1))
	l.header = l.header[:headerHeight]
	l.bodyHeight = available - headerHeight
	outputRow := len(m.connectionHeader()) + 2
	if m.screen == outputScreen && m.taskOutput == nil && len(l.header) > outputRow {
		start := m.outputUpdate.Chunk.Start + m.outputOffset(l.bodyHeight)
		end := min(m.outputUpdate.Chunk.End, start+l.bodyHeight)
		l.header[outputRow] = ansi.Truncate(m.styles.muted.Render(fmt.Sprintf("Job %d · lines %d–%d of %d", m.jobDetail.Ref.ID, start, end, m.outputUpdate.Chunk.AbsoluteEnd)), l.width, "")
	}
	return l
}

func (m *Model) outputOffset(height int) int {
	text := strings.TrimSuffix(m.outputUpdate.Chunk.Text, "\n")
	if !m.following || text == "" {
		return 0
	}
	return max(0, len(strings.Split(text, "\n"))-height)
}

func (m *Model) visibleOutputStart() int {
	return m.outputUpdate.Chunk.Start + m.outputOffset(m.layout().bodyHeight)
}

func (m *Model) outputLines() int {
	// The API requires a positive, bounded window even when chrome fills the TUI.
	return min(aap.OutputLines, max(1, m.layout().bodyHeight))
}

func (m *Model) header(width int) []string {
	title := []string{"Main", "Job Templates", "Jobs", "Template details", "Job details", "Workflow children", "Output", "Projects", "Inventories", "Inventory", "Groups", "Group", "Hosts", "Host details", "Workflow node details"}[m.screen]
	if m.screen == templateScreen && m.templateDetail.Ref.Type == aap.ProjectTemplate {
		title = "Project details"
	}
	if m.launch != nil {
		title = fmt.Sprintf("Launch template %d · %s", m.templateDetail.Ref.ID, m.launchText(singleLine(m.templateDetail.Name)))
	}
	title = m.styles.title.Render("AAP TUI · " + title)
	if m.loading {
		message := "Loading…"
		if m.operation != "" {
			message = m.operation
		}
		title += m.styles.warning.Render(" · " + message)
	}
	lines := append([]string{title}, m.connectionHeader()...)
	lines = append(lines, m.styles.muted.Render(strings.Repeat("─", width)))
	if m.screen == inventoryScreen || m.screen == groupsScreen || m.screen == groupScreen || m.screen == hostsScreen {
		location := fmt.Sprintf("Inventory %d · %s", m.inventory.ID, singleLine(m.inventory.Name))
		if m.group.ID > 0 {
			location += fmt.Sprintf(" / Group %d · %s", m.group.ID, singleLine(m.group.Name))
		}
		lines = append(lines, m.styles.key.Render(location))
		if m.screen == hostsScreen && m.group.ID > 0 {
			lines = append(lines, m.styles.muted.Render("Hosts including descendant groups"))
		}
	}
	if m.inventoryList() {
		count := m.inventoryPage.Count
		if m.screen == groupsScreen {
			count = m.groupPage.Count
		}
		if m.screen == hostsScreen {
			count = m.hostPage.Count
		}
		paging := fmt.Sprintf("%d results", count)
		if m.inventoryCursor(false).Present() {
			paging += " · p previous"
		}
		if m.inventoryCursor(true).Present() {
			paging += " · n next"
		}
		lines = append(lines, m.styles.muted.Render(paging))
	}
	switch m.screen {
	case templatesScreen, projectsScreen, inventoriesScreen, groupsScreen, hostsScreen, jobsScreen, workflowScreen:
		lines = append(lines, m.columnHeader(width))
	case outputScreen:
		if m.taskOutput != nil {
			lines = append(lines, m.styles.muted.Render(fmt.Sprintf("Job %d · events %d–%d", m.jobDetail.Ref.ID, m.taskPage.First, m.taskPage.Last)))
		} else {
			lines = append(lines, m.styles.muted.Render(fmt.Sprintf("Job %d · lines %d–%d of %d", m.jobDetail.Ref.ID, m.outputUpdate.Chunk.Start, m.outputUpdate.Chunk.End, m.outputUpdate.Chunk.AbsoluteEnd)))
		}
		state := m.styles.warning.Render("paused scrolling")
		if m.following {
			state = m.styles.accent.Render("following")
		}
		if m.outputUpdate.Complete {
			state += " · " + m.styles.success.Render("complete")
		} else if !m.outputUpdate.CompletionKnown {
			state += " · " + m.styles.warning.Render("final-output evidence unknown")
		}
		if m.outputUpdate.Retrying {
			state += " · " + m.styles.warning.Render("reconnecting")
		}
		lines = append(lines, m.styles.key.Render("Filter: "+string(m.activeOutputFilter()))+" · "+state+" · "+m.styles.status(string(m.outputUpdate.Status)))
	}
	return clipLines(lines, width)
}

func (m *Model) footer(width int) []string {
	if m.confirming {
		return m.confirmationFooter(width)
	}
	var help []string
	if m.editing {
		prompt := "Search: "
		if width < 10 {
			prompt = "/ "
			if width < 3 {
				prompt = ""
			}
		}
		value := tailCells(m.search, max(0, width-ansi.StringWidth(prompt)-1))
		input := m.styles.accent.Bold(true).Render(prompt) + value + m.styles.cursor.Render("_")
		if width < 60 || ansi.StringWidth("Search: "+m.search+"_ · Enter apply · Esc exit input") > width {
			help = []string{input, m.helpLine("Enter apply · Esc exit input")}
		} else {
			help = []string{input + " · " + m.helpLine("Enter apply · Esc exit input")}
		}
	} else {
		help = m.screenHelp(width)
	}
	// Keep the final back/quit or input-help row visible at very small sizes.
	budget := max(1, m.height/3)
	if len(help) > budget {
		help = help[len(help)-budget:]
	}
	if m.notice != "" && m.height > len(help)+4 {
		help = append([]string{m.styles.success.Render(m.notice)}, help...)
	}
	if m.err == nil {
		return clipLines(help, width)
	}
	messages := []string{m.err.Error()}
	var apiErr *aap.APIError
	if errors.As(m.err, &apiErr) && apiErr.Kind == aap.Authentication {
		messages = append(messages,
			"HTTP 401: server rejected the token. Check AAP_TOKEN.",
			"Check AAP_URL and AAP_CONNECTION_MODE match the token's server.",
			"After changing environment settings, restart aaptui.")
	}
	limit := min(6, max(0, m.height-len(help)-4))
	if limit == 0 {
		return clipLines(help, width)
	}
	panel := m.messagePanel("Error: "+strings.Join(messages, "\n"), width, limit)
	return clipLines(append(panel, help...), width)
}

// A wide grapheme can straddle the left cut. Advance past it so the suffix
// fits without clipping the cursor at the right edge.
func tailCells(value string, width int) string {
	for cut := max(0, ansi.StringWidth(value)-width); ; cut++ {
		tail := ansi.TruncateLeft(value, cut, "")
		if ansi.StringWidth(tail) <= width {
			return tail
		}
	}
}

// confirmationView gives the prompt the entire frame and keyboard focus. A
// centered border is used where it fits; short terminals keep the action and
// controls visible before spending rows on the job name or decoration.
func (m *Model) confirmationView() string {
	width := min(64, m.width)
	border := width >= 24 && m.height >= 8
	inner := width
	if border {
		inner -= 4
	}
	prompt := fmt.Sprintf("Cancel job %d (%s)?", m.confirmation.ID, m.confirmation.Type)
	if m.confirmRelaunch {
		prompt = fmt.Sprintf("Relaunch job %d (%s)?", m.confirmation.ID, m.confirmation.Type)
	}
	controls := []string{"y/Enter confirm", "n/Esc dismiss", "q / Ctrl-C quit"}
	lines := strings.Split(ansi.Hardwrap(prompt, inner, true), "\n")
	lines = append(lines, ansi.Truncate(singleLine(m.jobDetail.Name), inner, "…"))
	if m.confirmRelaunch {
		lines = append(lines, strings.Split(ansi.Hardwrap("Creates a new execution.", inner, true), "\n")...)
	}
	for _, control := range controls {
		lines = append(lines, ansi.Truncate(control, inner, ""))
	}
	budget := m.height
	if border {
		budget -= 2
	}
	if len(lines) > budget {
		if m.confirmRelaunch && budget >= 6 {
			lines = clipLines([]string{ansi.Truncate(prompt, inner, ""), ansi.Truncate(singleLine(m.jobDetail.Name), inner, "…"), "Creates a new execution", controls[0], controls[1], controls[2]}, inner)
		} else {
			// Prioritize the question and confirmation keys over optional text.
			lines = []string{ansi.Truncate(prompt, inner, ""), ansi.Truncate("y yes · n no", inner, "")}
			lines = lines[:min(budget, len(lines))]
		}
	}
	text := m.styles.danger.Bold(true).Render(strings.Join(lines, "\n"))
	if border {
		text = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(m.styles.danger.GetForeground()).Padding(0, 1).Width(inner + 2).Render(text)
	}
	panel := strings.Split(text, "\n")
	left := max(0, (m.width-width)/2)
	top := max(0, (m.height-len(panel))/2)
	frame := make([]string, m.height)
	for i, line := range panel {
		frame[top+i] = strings.Repeat(" ", left) + line
	}
	return strings.Join(frame, "\n")
}

func (m *Model) confirmationFooter(width int) []string {
	return clipLines([]string{m.helpLine("y/Enter confirm · n/Esc dismiss · q quit")}, width)
}

func (m *Model) messagePanel(text string, width, limit int) []string {
	style := m.styles.panel
	frameWidth, _ := style.GetFrameSize()
	if width <= frameWidth+1 {
		style = lipgloss.NewStyle()
		frameWidth = 0
	}
	innerWidth := max(1, width-frameWidth)
	lines := strings.Split(ansi.Hardwrap(text, innerWidth, true), "\n")
	if len(lines) > limit {
		lines = lines[:limit]
		lines[limit-1] = ansi.Truncate(lines[limit-1], max(0, innerWidth-1), "") + "…"
	}
	return strings.Split(style.Render(m.styles.danger.Bold(true).Render(strings.Join(lines, "\n"))), "\n")
}

func (m *Model) screenHelp(width int) []string {
	if m.launch != nil {
		return m.launchHelp(width)
	}
	var lines []string
	back := "Esc back · q quit · Ctrl-C quit"
	switch m.screen {
	case mainScreen:
		lines = []string{"↑/↓ select · Enter open · q quit · Ctrl-C quit"}
	case inventoryScreen, groupScreen:
		lines = []string{"Tab menu/details · ↑/↓ select/scroll · Enter open · r refresh", back}
	case templatesScreen, projectsScreen, inventoriesScreen, groupsScreen, jobsScreen:
		lines = []string{"↑/↓ select · Enter open · / search · r refresh · n/p page", back}
	case hostsScreen:
		lines = []string{"↑/↓ select · Enter details · / search · r refresh · n/p page", back}
	case hostScreen:
		lines = []string{"↑/↓ scroll · r refresh · n/p groups page", back}
	case nodeScreen:
		lines = []string{"↑/↓ scroll · Enter child job", back}
	case templateScreen:
		lines = []string{"↑/↓ scroll · Esc back · q quit · Ctrl-C quit"}
	case jobScreen:
		lines = []string{"o output · w children · c cancel · R relaunch · r refresh · ↑/↓ scroll", back}
	case workflowScreen:
		lines = []string{"↑/↓ select · d node details · Enter child · r refresh · n/p page", back}
	case outputScreen:
		lines = []string{"↑/↓ scroll · PgUp/Ctrl-U PgDown/Ctrl-D page · ←/→ pan · f follow · r resume", back}
	}
	if width < 60 {
		switch m.screen {
		case mainScreen:
			lines = []string{"↑/↓ select · Enter open", "q quit · Ctrl-C quit"}
		case inventoryScreen, groupScreen:
			lines = []string{"Tab menu/details · ↑/↓ move", "Enter open · r refresh", back}
		case templatesScreen, projectsScreen, inventoriesScreen, groupsScreen, jobsScreen:
			lines = []string{"↑/↓ select · Enter open", "/ search · r refresh · n/p page", back}
		case hostsScreen:
			lines = []string{"↑/↓ select · Enter details", "/ search · r refresh · n/p page", back}
		case hostScreen:
			lines = []string{"↑/↓ scroll · r refresh", "n/p groups page", back}
		case templateScreen:
			lines = []string{"↑/↓ scroll", back}
		case jobScreen:
			lines = []string{"o output · w children", "c cancel · R relaunch", "r refresh · ↑/↓ scroll", back}
		case workflowScreen:
			lines = []string{"↑/↓ select · d node details", "Enter child · r refresh · n/p page", back}
		case outputScreen:
			lines = []string{"PgUp/PgDown Ctrl-U/Ctrl-D page", "←/→ pan · f follow · r resume", back}
		}
	}
	if m.hasTemplateJobs() {
		lines = []string{"Tab details/jobs · ↑/↓ scroll/select · Enter job · r refresh · n/p page", back}
		if width < 60 {
			lines = []string{"Tab details/jobs · ↑/↓ move", "Enter job · r refresh · n/p page", back}
		}
	}
	if m.screen == templateScreen && !m.hasTemplateJobs() && m.templateDetail.Ref.Type == aap.PlaybookTemplate && m.templates != nil {
		lines = []string{"↑/↓ scroll · r refresh", back}
	}
	if m.screen == templateScreen && m.templateDetail.Ref.Type == aap.PlaybookTemplate && m.launcher != nil {
		lines[0] = "L launch job · " + lines[0]
	}
	if m.screen == outputScreen {
		lines = append(lines[:len(lines)-1], "A All · C Changed · F Failed", lines[len(lines)-1])
	}
	for i, line := range lines {
		lines[i] = m.helpLine(line)
	}
	return lines
}

func (m *Model) helpLine(line string) string {
	parts := strings.Split(line, " · ")
	for i, part := range parts {
		keys, description, _ := strings.Cut(part, " ")
		parts[i] = m.styles.key.Render(keys) + " " + m.styles.muted.Render(description)
	}
	return strings.Join(parts, m.styles.muted.Render(" · "))
}

func clipLines(lines []string, width int) []string {
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return lines
}
