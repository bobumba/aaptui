package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// renderDetailFields lays out sections independently so field/value columns stay
// aligned without allowing a long label to consume the value column.
func (m *Model) renderDetailFields(fields detailFields, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	for start := 0; start < len(fields); {
		if fields[start].label == "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			heading := m.styles.detailLabel.Render(fields[start].value)
			lines = append(lines, strings.Split(ansi.Wrap(heading, width, ""), "\n")...)
			start++
		}
		end := start
		for end < len(fields) && fields[end].label != "" {
			end++
		}
		lines = append(lines, m.detailTable(fields[start:end], width)...)
		start = end
	}
	return lines
}

func (m *Model) detailTable(fields detailFields, width int) []string {
	const gap = " │ "
	columns := 1
	if width >= 120 {
		columns = 2
	}
	pairWidth := (width - (columns-1)*ansi.StringWidth(gap)) / columns
	labelWidth := 0
	for _, field := range fields {
		labelWidth = max(labelWidth, ansi.StringWidth(field.label)+1)
	}
	labelWidth = min(labelWidth, 32, max(1, (pairWidth-2)/2))

	var lines []string
	var row [][]string
	flush := func() {
		if len(row) == 0 {
			return
		}
		height := 0
		for _, pair := range row {
			height = max(height, len(pair))
		}
		for i := 0; i < height; i++ {
			var cells []string
			for _, pair := range row {
				line := ""
				if i < len(pair) {
					line = pair[i]
				}
				cells = append(cells, padDetailCell(line, pairWidth))
			}
			lines = append(lines, strings.Join(cells, m.styles.muted.Render(gap)))
		}
		row = nil
	}
	for _, field := range fields {
		// Descriptions and explicit multiline values get a full row. Wrapping
		// ordinary values stays within their column and grows the entire row.
		if field.label == "Description" || strings.Contains(field.value, "\n") {
			flush()
			lines = append(lines, m.detailPair(field, width, labelWidth)...)
			continue
		}
		row = append(row, m.detailPair(field, pairWidth, labelWidth))
		if len(row) == columns {
			flush()
		}
	}
	flush()
	return lines
}

func (m *Model) detailPair(field detailField, width, labelWidth int) []string {
	label := field.label + ":"
	if width < 8 {
		// A stacked pair still exposes the entire field on very small screens.
		lines := strings.Split(ansi.Wrap(m.styles.detailLabel.Render(label), width, ""), "\n")
		return append(lines, strings.Split(ansi.Wrap(field.value, width, ""), "\n")...)
	}
	labels := strings.Split(ansi.Wrap(label, labelWidth, ""), "\n")
	values := strings.Split(ansi.Wrap(field.value, width-labelWidth-2, ""), "\n")
	lines := make([]string, max(len(labels), len(values)))
	for i := range lines {
		label, value := "", ""
		if i < len(labels) {
			label = labels[i]
		}
		if i < len(values) {
			value = values[i]
		}
		lines[i] = m.styles.detailLabel.Render(padDetailCell(label, labelWidth)) + "  " + value
	}
	return lines
}

func padDetailCell(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}
