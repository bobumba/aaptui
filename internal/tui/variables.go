package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

// variableBox uses the same scrolling as other details. Decoration yields on
// extremely narrow terminals so the content still has room for wide graphemes.
func (m *Model) variableBox(vars *aap.JobVariables, width int, template bool) []string {
	title, empty := "Survey / Extra variables", "No recorded variables"
	if template {
		title, empty = "Extra variables", "No configured variables"
	}
	var fields detailFields
	switch {
	case vars == nil:
		state := "Not supported for this job type"
		if template {
			state = "Unavailable · Not returned by the server"
		}
		fields.field("Variables", state)
	case vars.UnavailableReason != "":
		fields.field("Variables", "Unavailable · "+vars.UnavailableReason)
	case len(vars.Values) == 0:
		fields.field("Variables", empty)
	default:
		for _, variable := range vars.Values {
			fields.field(variable.Name, variable.Value)
		}
	}
	inner := width
	border := width >= 6
	if border {
		inner -= 4
	}
	lines := strings.Split(ansi.Wrap(m.styles.detailLabel.Render(title), inner, ""), "\n")
	lines = append(lines, m.detailTable(fields, inner)...)
	if !border {
		return lines
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.accent.GetForeground()).Padding(0, 1).
		Width(width).Render(strings.Join(lines, "\n"))
	return strings.Split(box, "\n")
}
