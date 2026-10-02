package tui

import "charm.land/lipgloss/v2"

// Styles belong to each model so background changes never affect another TUI.
type styles struct {
	title, muted, accent, selected lipgloss.Style
	success, warning, danger       lipgloss.Style
	key, cursor, panel             lipgloss.Style
}

func newStyles(isDark bool) styles {
	lightDark := lipgloss.LightDark(isDark)
	accent := lightDark(lipgloss.Color("#006A80"), lipgloss.Color("#67D4E8"))
	muted := lightDark(lipgloss.Color("#595959"), lipgloss.Color("#A6A6A6"))
	danger := lightDark(lipgloss.Color("#B42318"), lipgloss.Color("#FF8A80"))
	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		muted:    lipgloss.NewStyle().Foreground(muted),
		accent:   lipgloss.NewStyle().Foreground(accent),
		selected: lipgloss.NewStyle().Bold(true).Reverse(true),
		success:  lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#18703C"), lipgloss.Color("#85D9A0"))),
		warning:  lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#8A5700"), lipgloss.Color("#F2C66D"))),
		danger:   lipgloss.NewStyle().Foreground(danger),
		key:      lipgloss.NewStyle().Bold(true),
		cursor:   lipgloss.NewStyle().Reverse(true),
		panel:    lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(danger).PaddingLeft(1),
	}
}

func (s styles) status(value string) string {
	style := s.muted
	switch value {
	case "running":
		style = s.accent
	case "successful":
		style = s.success
	case "failed", "error":
		style = s.danger
	case "new", "pending", "waiting":
		style = s.warning
	}
	return style.Render(value)
}
