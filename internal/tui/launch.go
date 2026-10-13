package tui

import (
	"context"
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
