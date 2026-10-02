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
	if m.screen == outputScreen && len(l.header) > 3 {
		start := m.outputUpdate.Chunk.Start + m.outputOffset(l.bodyHeight)
		end := min(m.outputUpdate.Chunk.End, start+l.bodyHeight)
		l.header[3] = ansi.Truncate(m.styles.muted.Render(fmt.Sprintf("Job %d · lines %d–%d of %d", m.jobDetail.Ref.ID, start, end, m.outputUpdate.Chunk.AbsoluteEnd)), l.width, "")
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
	title := []string{"Main", "Job Templates", "Jobs", "Template details", "Job details", "Workflow children", "Output", "Projects"}[m.screen]
	if m.screen == templateScreen && m.templateDetail.Ref.Type == aap.ProjectTemplate {
		title = "Project details"
	}
	title = m.styles.title.Render("AAP TUI · " + title)
	if m.loading {
		title += m.styles.warning.Render(" · Loading…")
	}
	lines := []string{title, m.styles.muted.Render(m.connection), m.styles.muted.Render(strings.Repeat("─", width))}
	switch m.screen {
	case templatesScreen, projectsScreen, jobsScreen, workflowScreen:
		lines = append(lines, m.columnHeader(width))
	case outputScreen:
		lines = append(lines, m.styles.muted.Render(fmt.Sprintf("Job %d · lines %d–%d of %d", m.jobDetail.Ref.ID, m.outputUpdate.Chunk.Start, m.outputUpdate.Chunk.End, m.outputUpdate.Chunk.AbsoluteEnd)))
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
		lines = append(lines, state+" · "+m.styles.status(string(m.outputUpdate.Status)))
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

func (m *Model) confirmationFooter(width int) []string {
	prompt := fmt.Sprintf("Cancel job %d (%s)?", m.confirmation.ID, m.confirmation.Type)
	help := "y/Enter confirm · n/Esc dismiss"
	limit := max(1, m.height-5)
	if m.height < 8 {
		help = "y yes · n no"
		limit = 1
	} else {
		prompt += "\n" + singleLine(m.jobDetail.Name)
	}
	panel := m.messagePanel(prompt, width, min(4, limit))
	return clipLines(append(panel, m.helpLine(help)), width)
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
	var lines []string
	back := "Esc back · q quit · Ctrl-C quit"
	switch m.screen {
	case mainScreen:
		lines = []string{"↑/↓ select · Enter open · q quit · Ctrl-C quit"}
	case templatesScreen, projectsScreen, jobsScreen:
		lines = []string{"↑/↓ select · Enter open · / search · r refresh · n/p page", back}
	case templateScreen:
		lines = []string{"↑/↓ scroll · Esc back · q quit · Ctrl-C quit"}
	case jobScreen:
		lines = []string{"o output · w children · c cancel · r refresh · ↑/↓ scroll", back}
	case workflowScreen:
		lines = []string{"↑/↓ select · Enter child · r refresh · n/p page", back}
	case outputScreen:
		lines = []string{"↑/↓ PgUp/PgDown scroll · ←/→ pan · f follow · r resume", back}
	}
	if width < 60 {
		switch m.screen {
		case mainScreen:
			lines = []string{"↑/↓ select · Enter open", "q quit · Ctrl-C quit"}
		case templatesScreen, projectsScreen, jobsScreen:
			lines = []string{"↑/↓ select · Enter open", "/ search · r refresh · n/p page", back}
		case templateScreen:
			lines = []string{"↑/↓ scroll", back}
		case jobScreen:
			lines = []string{"o output · w children", "c cancel · r refresh · ↑/↓ scroll", back}
		case workflowScreen:
			lines = []string{"↑/↓ select · Enter child", "r refresh · n/p page", back}
		case outputScreen:
			lines = []string{"↑/↓ PgUp/PgDown scroll", "←/→ pan · f follow · r resume", back}
		}
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
