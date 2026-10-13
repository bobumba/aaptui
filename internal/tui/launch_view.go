package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// launchView owns the frame while the form has keyboard focus, just like the
// cancellation/relaunch dialogs. Its chrome leaves room for at least one input
// row; decoration yields first on small terminals.
func (m *Model) launchView() string {
	width, budget := min(80, m.width), m.height
	border := width >= 24 && budget >= 12
	inner := width
	if border {
		inner -= 4
		budget -= 2
	}
	f := m.launch
	header := []string{m.styles.title.Render(fmt.Sprintf("Launch template %d · %s", m.templateDetail.Ref.ID, m.launchText(singleLine(m.templateDetail.Name))))}
	if budget >= 8 {
		header = append(header, m.styles.warning.Render("Submitting creates a new execution."))
	}
	if budget >= 16 {
		header = append(header, m.connectionHeader()...)
	}
	var status []string
	switch {
	case m.err != nil:
		status = m.messagePanel("Error: "+m.launchText(m.err.Error()), inner, min(3, max(1, budget/5)))
	case m.loading:
		status = []string{m.styles.warning.Render(m.operation)}
	case !f.ready:
		status = []string{m.styles.warning.Render("Launch unavailable; Esc dismisses.")}
	case f.uncertain:
		status = []string{m.styles.warning.Render("Check Jobs before retrying this launch.")}
	}
	if f.ready && !f.requirements.AskVariables && budget >= 10 {
		status = append(status, m.styles.warning.Render("Manual variables require Prompt on launch."))
	}
	footer := m.launchHelp(inner)
	var actions []string
	if f.ready {
		label := "[ Launch job ]"
		if f.uncertain {
			label = "[ Launch disabled: check Jobs before retrying ]"
		}
		actions = []string{m.selectRow(label, f.focus == len(f.inputs)+1, inner), m.selectRow("[ Cancel ]", f.focus == len(f.inputs)+2, inner)}
		// Two actions plus help fit comfortably in ordinary terminals. In short
		// ones, show only the focused action so the editor still gets a row.
		if budget >= 8 {
			footer = append(actions, footer...)
		}
	}
	// Reserve the focused content before adding title, messages, or help.
	footer = footer[:min(len(footer), max(0, budget-1))]
	header = header[:min(len(header), max(0, budget-len(footer)-1))]
	status = status[:min(len(status), max(0, budget-len(header)-len(footer)-1))]
	bodyHeight := budget - len(header) - len(status) - len(footer)
	var body []string
	if f.ready {
		if budget < 8 && f.focus > len(f.inputs) {
			body = []string{actions[f.focus-len(f.inputs)-1]}
		} else {
			body = m.launchBody(inner, bodyHeight)
		}
	} else {
		message := "Reading survey and launch requirements…"
		if m.err != nil {
			message = "Launch unavailable; Esc dismisses."
		}
		body = []string{m.styles.warning.Render(message)}
	}
	lines := append(header, status...)
	lines = append(lines, body...)
	lines = append(lines, footer...)
	lines = clipLines(lines, inner)
	panel := strings.Join(lines, "\n")
	if border {
		panel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(m.styles.accent.GetForeground()).Padding(0, 1).Width(width).Render(panel)
	}
	rows := strings.Split(panel, "\n")
	left, top := max(0, (m.width-width)/2), max(0, (m.height-len(rows))/2)
	frame := make([]string, m.height)
	for i, row := range rows {
		if top+i < len(frame) {
			frame[top+i] = strings.Repeat(" ", left) + row
		}
	}
	return strings.Join(frame, "\n")
}

func (m *Model) launchHelp(width int) []string {
	if m.height < 8 {
		return clipLines([]string{m.styles.key.Render("Tab · Esc · Ctrl-C")}, width)
	}
	if width < 30 {
		return clipLines([]string{m.helpLine("Tab/Shift-Tab focus"), m.styles.key.Render("Esc · Ctrl-C")}, width)
	}
	if width < 50 || m.height < 12 {
		return clipLines([]string{m.helpLine("Tab focus · Enter edit/activate"), m.helpLine("Esc dismiss · Ctrl-C quit")}, width)
	}
	return clipLines([]string{m.helpLine("Tab/Shift-Tab focus · Enter edit/activate"), m.helpLine("↑/↓ choose · Space toggle · Esc dismiss · Ctrl-C quit")}, width)
}

func (m *Model) launchBody(width, height int) []string {
	f := m.launch
	var lines []string
	focusStart, focusEnd := 0, 0
	for i := 0; i <= len(f.inputs); i++ {
		field := m.launchField(i, width, height)
		if f.focus == i {
			focusStart, focusEnd = len(lines), len(lines)+len(field)
		}
		lines = append(lines, field...)
	}
	if f.focus > len(f.inputs) {
		// Keep the variables editor above the pinned action buttons.
		focusStart, focusEnd = len(lines)-1, len(lines)
	}
	top := max(0, focusEnd-height)
	top = min(top, focusStart)
	return lines[top:min(len(lines), top+height)]
}

// launchField bounds each box to the viewport so scrolling never cuts off the
// focused label, input cursor, or borders. Long descriptions yield to editing.
func (m *Model) launchField(index, width, height int) []string {
	f := m.launch
	focused := f.focus == index
	border := width >= 20 && height >= 5 && m.height >= 12
	inner, budget := width, height
	if border {
		inner -= 4
		budget -= 2
	}
	label, metadata, description := "extra_vars", "JSON or YAML · variable mapping", "Survey keys belong in their fields above."
	if len(f.inputs) == 0 {
		description = "Enter a JSON or YAML variable mapping."
	}
	input := f.extra
	kind := "textarea"
	if index < len(f.inputs) {
		q := f.requirements.Questions[index]
		label = fmt.Sprintf("%d. %s", index+1, m.launchText(singleLine(q.Name)))
		metadata = q.Type
		if q.Required {
			metadata += " · required"
		} else {
			metadata += " · optional"
		}
		description, input, kind = m.launchText(singleLine(q.Description)), f.inputs[index], q.Type
	}
	var values []string
	cursor := 0
	switch kind {
	case "multiplechoice", "multiselect":
		q := f.requirements.Questions[index]
		start := max(0, f.choices[index]-1)
		for j := start; j < min(len(q.Choices), start+3); j++ {
			marker := "[ ] "
			for _, value := range strings.Split(input.text, "\n") {
				if value == q.Choices[j] {
					marker = "[x] "
				}
			}
			values = append(values, m.selectRow(marker+m.launchText(singleLine(q.Choices[j])), focused && f.choices[index] == j, inner))
		}
		cursor = f.choices[index] - start
	case "password":
		value := strings.Repeat("*", min(20, len([]rune(input.text))))
		if input.text == "$encrypted$" {
			value = "(saved password default)"
		}
		if focused {
			value = tailCells(value, max(0, inner-1)) + m.styles.cursor.Render("_")
		}
		values = []string{value}
	default:
		values, cursor = input.render(m, max(1, inner), focused)
	}
	var lines []string
	if budget >= 2 {
		lines = append(lines, m.selectRow(label, focused, inner))
	}
	if budget >= 3 {
		lines = append(lines, m.styles.muted.Render(metadata))
	}
	fieldError := ""
	if m.err != nil {
		// Variable validation identifies controls by these existing prefixes.
		// Keep the full error in the dialog and repeat its explanation beside
		// the corresponding input when there is room.
		prefix := "extra_vars: "
		if index < len(f.inputs) {
			prefix = fmt.Sprintf("Survey question %d: ", index+1)
		}
		if message, ok := strings.CutPrefix(m.err.Error(), prefix); ok {
			fieldError = m.launchText(message)
		}
	}
	if fieldError != "" && budget > len(lines)+1 {
		feedback := strings.Split(ansi.Hardwrap("Error: "+fieldError, inner, true), "\n")
		limit := min(2, budget-len(lines)-1)
		if len(feedback) > limit {
			feedback = feedback[:limit]
			feedback[limit-1] = ansi.Truncate(feedback[limit-1], max(0, inner-1), "") + "…"
		}
		for _, line := range feedback {
			lines = append(lines, m.styles.danger.Render(line))
		}
	} else if description != "" && budget >= len(lines)+len(values)+1 {
		desc := strings.Split(ansi.Hardwrap(description, inner, true), "\n")
		limit := min(2, budget-len(lines)-len(values))
		if len(desc) > limit {
			desc = desc[:limit]
			desc[limit-1] = ansi.Truncate(desc[limit-1], max(0, inner-1), "") + "…"
		}
		for _, line := range desc {
			lines = append(lines, m.styles.muted.Render(line))
		}
	}
	available := max(1, budget-len(lines))
	start := min(max(0, cursor-available+1), max(0, len(values)-available))
	lines = append(lines, values[start:min(len(values), start+available)]...)
	lines = clipLines(lines, inner)
	if !border {
		return lines
	}
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.styles.muted.GetForeground())
	if fieldError != "" {
		style = style.BorderForeground(m.styles.danger.GetForeground())
	}
	if focused {
		style = style.Border(lipgloss.DoubleBorder())
		if fieldError == "" {
			style = style.BorderForeground(m.styles.accent.GetForeground())
		}
	}
	return strings.Split(style.Padding(0, 1).Width(width).Render(strings.Join(lines, "\n")), "\n")
}
