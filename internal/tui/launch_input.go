package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

// launchInput is a bounded rune editor. Tab is handled by the form, and Enter
// inserts a newline only in textarea/extra_vars; it never submits from an input.
type launchInput struct {
	text   string
	cursor int
}

func (in *launchInput) insert(text string, multiline bool) error {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if r == '\n' {
			if multiline {
				return r
			}
			return ' '
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	if len(in.text)+len(text) > aap.LaunchInputBytes {
		return fmt.Errorf("Input exceeds 64 KiB")
	}
	r := []rune(in.text)
	in.cursor = min(max(in.cursor, 0), len(r))
	in.text = string(r[:in.cursor]) + text + string(r[in.cursor:])
	in.cursor += len([]rune(text))
	return nil
}

func (in *launchInput) update(msg tea.KeyPressMsg, multiline bool) error {
	r := []rune(in.text)
	in.cursor = min(max(in.cursor, 0), len(r))
	switch msg.String() {
	case "left":
		in.cursor = max(0, in.cursor-1)
	case "right":
		in.cursor = min(len(r), in.cursor+1)
	case "home", "ctrl+a":
		for in.cursor > 0 && r[in.cursor-1] != '\n' {
			in.cursor--
		}
	case "end", "ctrl+e":
		for in.cursor < len(r) && r[in.cursor] != '\n' {
			in.cursor++
		}
	case "up", "down":
		start := in.cursor
		for start > 0 && r[start-1] != '\n' {
			start--
		}
		col := in.cursor - start
		if msg.String() == "up" && start > 0 {
			end := start - 1
			start = end
			for start > 0 && r[start-1] != '\n' {
				start--
			}
			in.cursor = min(start+col, end)
		} else if msg.String() == "down" {
			end := in.cursor
			for end < len(r) && r[end] != '\n' {
				end++
			}
			if end < len(r) {
				start = end + 1
				end = start
				for end < len(r) && r[end] != '\n' {
					end++
				}
				in.cursor = min(start+col, end)
			}
		}
	case "backspace":
		if in.cursor > 0 {
			in.text = string(r[:in.cursor-1]) + string(r[in.cursor:])
			in.cursor--
		}
	case "delete":
		if in.cursor < len(r) {
			in.text = string(r[:in.cursor]) + string(r[in.cursor+1:])
		}
	case "ctrl+k":
		end := in.cursor
		for end < len(r) && r[end] != '\n' {
			end++
		}
		in.text = string(r[:in.cursor]) + string(r[end:])
	case "ctrl+u":
		in.text = ""
		in.cursor = 0
	case "enter":
		if multiline {
			return in.insert("\n", true)
		}
	default:
		if msg.Text != "" {
			return in.insert(msg.Text, multiline)
		}
	}
	return nil
}

func (in launchInput) render(m *Model, width int, focused bool) ([]string, int) {
	r := []rune(in.text)
	cursor := min(max(in.cursor, 0), len(r))
	before := string(r[:cursor])
	row := strings.Count(before, "\n")
	all := strings.Split(in.text, "\n")
	start := max(0, row-1)
	end := min(len(all), start+3)
	var lines []string
	for i := start; i < end; i++ {
		value := m.launchText(all[i])
		if focused && i == row {
			// Redact the complete line before inserting a cursor. Cleaning two
			// fragments separately could expose a token split by the cursor.
			if value != all[i] {
				lines = append(lines, tailCells(value, max(0, width-1))+m.styles.cursor.Render("_"))
				continue
			}
			offset := strings.LastIndex(before, "\n") + 1
			left := m.launchText(before[offset:])
			right := m.launchText(string(r[cursor:]))
			right, _, _ = strings.Cut(right, "\n")
			left = tailCells(left, max(0, width-1))
			value = left + m.styles.cursor.Render("_") + ansi.Truncate(right, max(0, width-ansi.StringWidth(left)-1), "")
		}
		lines = append(lines, ansi.Truncate(value, width, ""))
	}
	return lines, row - start
}
