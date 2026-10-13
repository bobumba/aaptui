package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

type Launcher interface {
	PrepareLaunch(context.Context, aap.TemplateRef) (aap.LaunchRequirements, error)
	Launch(context.Context, aap.LaunchRequirements, []string, string) (aap.LaunchResult, error)
}

type launchPrepared struct{ requirements aap.LaunchRequirements }
type launchSubmitted struct{ result aap.LaunchResult }

type launchForm struct {
	requirements     aap.LaunchRequirements
	inputs           []launchInput
	extra            launchInput
	choices          []int
	focus            int
	ready, uncertain bool
}

func (f *launchForm) setRequirements(r aap.LaunchRequirements) {
	f.requirements = r
	f.inputs = make([]launchInput, len(r.Questions))
	f.choices = make([]int, len(r.Questions))
	for i, q := range r.Questions {
		f.inputs[i] = launchInput{text: q.Default, cursor: len([]rune(q.Default))}
		for j, choice := range q.Choices {
			if choice == q.Default {
				f.choices[i] = j
				break
			}
		}
	}
	f.extra = launchInput{text: "{}", cursor: 2}
	f.ready = true
}

func (m *Model) WithLaunch(l Launcher) *Model {
	m.launcher = l
	if cleaner, ok := l.(interface{ CleanText(string) string }); ok {
		m.cleanLaunchText = cleaner.CleanText
	}
	return m
}

func (m *Model) launchText(s string) string {
	if m.cleanLaunchText != nil {
		return m.cleanLaunchText(s)
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (!unicode.IsControl(r) && !unicode.Is(unicode.Cf, r)) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

func (m *Model) prepareLaunch() tea.Cmd {
	ref := m.templateDetail.Ref
	m.launch = &launchForm{}
	cmd := m.begin(func(ctx context.Context) (any, error) {
		r, err := m.launcher.PrepareLaunch(ctx, ref)
		return launchPrepared{r}, err
	})
	m.operation = "Preparing launch…"
	return cmd
}

func (m *Model) dismissLaunch() {
	pending := m.operation == "Launching job…"
	m.invalidate()
	if pending {
		m.notice = "Launch may still have created a job; refresh Jobs before retrying."
	}
}

func (m *Model) updateLaunch(msg tea.KeyPressMsg) tea.Cmd {
	f := m.launch
	key := msg.String()
	if key == "esc" {
		m.dismissLaunch()
		return nil
	}
	if m.loading {
		return nil
	}
	if !f.ready {
		return nil
	}
	count := len(f.inputs) + 3
	switch key {
	case "tab":
		f.focus = (f.focus + 1) % count
		return nil
	case "shift+tab":
		f.focus = (f.focus + count - 1) % count
		return nil
	}
	if f.focus == len(f.inputs)+2 {
		if key == "enter" || key == " " || key == "space" {
			m.dismissLaunch()
		}
		return nil
	}
	if f.focus == len(f.inputs)+1 {
		if (key != "enter" && key != " " && key != "space") || f.uncertain {
			return nil
		}
		answers := make([]string, len(f.inputs))
		for i, input := range f.inputs {
			answers[i] = input.text
		}
		manual := f.extra.text
		if _, err := aap.LaunchVariables(f.requirements, answers, manual); err != nil {
			m.err = err
			return nil
		}
		prepared := f.requirements
		cmd := m.begin(func(ctx context.Context) (any, error) {
			result, err := m.launcher.Launch(ctx, prepared, answers, manual)
			return launchSubmitted{result}, err
		})
		m.operation = "Launching job…"
		return cmd
	}
	input := &f.extra
	multiline := true
	if f.focus < len(f.inputs) {
		i := f.focus
		q := f.requirements.Questions[i]
		input = &f.inputs[i]
		if q.Type == "multiplechoice" || q.Type == "multiselect" {
			switch key {
			case "up":
				f.choices[i] = max(0, f.choices[i]-1)
			case "down":
				f.choices[i] = min(len(q.Choices)-1, f.choices[i]+1)
			case " ", "space", "enter":
				choice := q.Choices[f.choices[i]]
				if q.Type == "multiplechoice" {
					input.text = choice
				} else {
					var values []string
					selected := map[string]bool{}
					for _, value := range strings.Split(input.text, "\n") {
						selected[value] = true
					}
					selected[choice] = !selected[choice]
					for _, value := range q.Choices {
						if selected[value] {
							values = append(values, value)
						}
					}
					input.text = strings.Join(values, "\n")
				}
			case "backspace", "delete":
				input.text = ""
			}
			input.cursor = len([]rune(input.text))
			return nil
		}
		multiline = q.Type == "textarea"
		if q.Type == "password" && input.text == "$encrypted$" && (msg.Text != "" || key == "backspace" || key == "delete") {
			input.text, input.cursor = "", 0
		}
	}
	if err := input.update(msg, multiline); err != nil {
		m.err = err
	}
	return nil
}

func (m *Model) pasteLaunch(text string) {
	f := m.launch
	if m.loading || !f.ready || f.focus > len(f.inputs) {
		return
	}
	input, multiline := &f.extra, true
	if f.focus < len(f.inputs) {
		q := f.requirements.Questions[f.focus]
		if q.Type == "multiplechoice" || q.Type == "multiselect" {
			return
		}
		input, multiline = &f.inputs[f.focus], q.Type == "textarea"
		if q.Type == "password" && input.text == "$encrypted$" {
			input.text, input.cursor = "", 0
		}
	}
	if err := input.insert(text, multiline); err != nil {
		m.err = err
	}
}

func (m *Model) launchBody(width, height int) []string {
	if height <= 0 {
		return nil
	}
	f := m.launch
	if !f.ready {
		if m.err != nil {
			return []string{m.styles.warning.Render("Launch unavailable; Esc dismisses.")}
		}
		return []string{m.styles.warning.Render("Reading survey and launch requirements…; Esc dismisses.")}
	}
	lines := []string{m.styles.warning.Render("Submitting creates a new execution.")}
	if !f.requirements.AskVariables {
		lines = append(lines, m.styles.warning.Render("Manual variables require Prompt on launch."))
	}
	focusRow := 0
	for i, q := range f.requirements.Questions {
		label := fmt.Sprintf("%d. %s", i+1, m.launchText(q.Name))
		label += " · " + q.Type
		if q.Required {
			label += " (required)"
		} else {
			label += " (optional)"
		}
		lines = append(lines, m.selectRow(label, f.focus == i, width))
		if q.Description != "" {
			lines = append(lines, m.styles.muted.Render(m.launchText(singleLine(q.Description))))
		}
		input := f.inputs[i]
		switch q.Type {
		case "multiplechoice", "multiselect":
			start := max(0, f.choices[i]-1)
			for j := start; j < min(len(q.Choices), start+3); j++ {
				marker := "[ ] "
				for _, value := range strings.Split(input.text, "\n") {
					if value == q.Choices[j] {
						marker = "[x] "
					}
				}
				lines = append(lines, m.selectRow(marker+m.launchText(q.Choices[j]), f.focus == i && f.choices[i] == j, width))
				if f.focus == i && f.choices[i] == j {
					focusRow = len(lines) - 1
				}
			}
		case "password":
			value := strings.Repeat("*", min(20, len([]rune(input.text))))
			if input.text == "$encrypted$" {
				value = "(saved password default)"
			}
			if f.focus == i {
				value += "_"
				focusRow = len(lines)
			}
			lines = append(lines, "  "+value)
		default:
			rendered, cursorRow := input.render(m, max(1, width-2), f.focus == i)
			if f.focus == i {
				focusRow = len(lines) + cursorRow
			}
			for _, line := range rendered {
				lines = append(lines, "  "+line)
			}
		}
	}
	lines = append(lines, m.selectRow("extra_vars (JSON or YAML; survey keys belong above)", f.focus == len(f.inputs), width))
	rendered, cursorRow := f.extra.render(m, max(1, width-2), f.focus == len(f.inputs))
	if f.focus == len(f.inputs) {
		focusRow = len(lines) + cursorRow
	}
	for _, line := range rendered {
		lines = append(lines, "  "+line)
	}
	launchLabel := "[ Launch job ]"
	if f.uncertain {
		launchLabel = "[ Launch disabled: check Jobs before retrying ]"
	}
	lines = append(lines, m.selectRow(launchLabel, f.focus == len(f.inputs)+1, width))
	if f.focus == len(f.inputs)+1 {
		focusRow = len(lines) - 1
	}
	lines = append(lines, m.selectRow("[ Cancel ]", f.focus == len(f.inputs)+2, width))
	if f.focus == len(f.inputs)+2 {
		focusRow = len(lines) - 1
	}
	top := min(max(0, focusRow-height/2), max(0, len(lines)-height))
	return lines[top:min(len(lines), top+height)]
}
