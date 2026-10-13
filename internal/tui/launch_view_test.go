package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func TestLaunchDialogAndIndividualFieldBoxes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, survey := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%t/survey=%t", dark, survey), func(t *testing.T) {
				questions := []aap.SurveyQuestion{}
				if survey {
					questions = append(questions, sampleQuestions()...)
					questions[0].Description = "Provide a title for this execution."
					questions = append(questions,
						aap.SurveyQuestion{Name: "Count", Variable: "count", Type: "integer", Default: "2"},
						aap.SurveyQuestion{Name: "Ratio", Variable: "ratio", Type: "float", Default: "1.5"})
				}
				m := launchModel(t, &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: questions}})
				m.styles = newStyles(dark)
				m.width, m.height = 120, 80
				openLaunch(t, m)
				lines := assertFrameSize(t, m)
				view := ansi.Strip(strings.Join(lines, "\n"))
				for _, text := range []string{"Launch template 1", "deploy", "creates a new execution", "> ", "extra_vars", "JSON or YAML", "[ Launch job ]", "[ Cancel ]", "Tab/Shift-Tab", "Esc dismiss", "Ctrl-C quit"} {
					if !strings.Contains(view, text) {
						t.Fatalf("missing %q: %s", text, view)
					}
				}
				// One outer dialog, one box for every input, and a double border
				// plus a text marker for focus, even with all ANSI styling stripped.
				if got := strings.Count(view, "╭"); got != len(questions)+1 {
					t.Fatalf("rounded boxes = %d, want %d: %s", got, len(questions)+1, view)
				}
				if strings.Count(view, "╔") != 1 || strings.Count(view, "╚") != 1 {
					t.Fatalf("focused box missing: %s", view)
				}
				for i, question := range questions {
					if !strings.Contains(view, fmt.Sprintf("%d. %s", i+1, question.Name)) || !strings.Contains(view, question.Type) {
						t.Fatalf("survey label/type missing for %s: %s", question.Name, view)
					}
				}
				if survey {
					for _, text := range []string{"required", "optional", "Provide a title", "(saved password default)", "[x] dev", "[ ] prod"} {
						if !strings.Contains(view, text) {
							t.Fatalf("survey detail missing %q: %s", text, view)
						}
					}
				}
				first, last := -1, -1
				for i, line := range lines {
					if strings.TrimSpace(ansi.Strip(line)) != "" {
						if first < 0 {
							first = i
						}
						last = i
					}
				}
				if first != (m.height-(last-first+1))/2 || !strings.HasPrefix(ansi.Strip(lines[first]), strings.Repeat(" ", 20)+"╭") {
					t.Fatalf("dialog not centered: first=%d last=%d: %s", first, last, view)
				}
			})
		}
	}
}

func TestLaunchFocusSurvivesScrollingAndResize(t *testing.T) {
	questions := sampleQuestions()
	questions[0].Name = strings.Repeat("部署", 40)
	questions[0].Description = strings.Repeat("Description ", 100)
	questions[2].Choices = []string{"dev", "test", "stage", "production", "disaster recovery"}
	m := launchModel(t, &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: questions}})
	openLaunch(t, m)
	m.launch.inputs[1] = launchInput{text: strings.Repeat("secret", 10), cursor: 60}
	m.launch.inputs[4] = launchInput{text: strings.Repeat("older line\n", 20) + "textarea tail"}
	m.launch.inputs[4].cursor = len([]rune(m.launch.inputs[4].text))
	m.launch.extra = launchInput{text: strings.Repeat("earlier: true\n", 20) + "manual tail"}
	m.launch.extra.cursor = len([]rune(m.launch.extra.text))
	m.launch.choices[2] = 4
	for focus := 0; focus < len(questions)+3; focus++ {
		if m.launch.focus != focus {
			t.Fatalf("unexpected focus %d, want %d", m.launch.focus, focus)
		}
		for _, size := range [][2]int{{80, 24}, {30, 12}, {24, 12}, {19, 6}, {40, 12}, {80, 24}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
			if strings.Contains(view, "secret") || !strings.Contains(view, "Esc") {
				t.Fatalf("secret exposed or dismissal hidden at %v: %s", size, view)
			}
			if focus <= len(questions) {
				if focus != 2 && focus != 3 && !strings.Contains(view, "_") {
					t.Fatalf("cursor hidden at focus %d, size %v: %s", focus, size, view)
				}
				if size[1] >= 12 && !strings.Contains(view, "> ") {
					t.Fatalf("focused label hidden at focus %d, size %v: %s", focus, size, view)
				}
				if focus == 2 && !strings.Contains(view, "> [ ] disaster") {
					t.Fatalf("focused choice hidden at %v: %s", size, view)
				}
			} else if !strings.Contains(view, "> [ ") {
				t.Fatalf("focused action hidden at %v: %s", size, view)
			}
			if size == [2]int{80, 24} && focus <= len(questions) && (!strings.Contains(view, "╔") || !strings.Contains(view, "╚")) {
				t.Fatalf("focused box clipped at focus %d: %s", focus, view)
			}
			if size[1] < 12 && (strings.Contains(view, "╭") || strings.Contains(view, "╔")) {
				t.Fatalf("decoration consumes short terminal: %s", view)
			}
		}
		m.Update(launchKey("tab"))
	}
	if m.launch.focus != 0 {
		t.Fatal("focus did not wrap")
	}
	m.Update(launchKey("shift+tab"))
	if !strings.Contains(ansi.Strip(m.View().Content), "> [ Cancel ]") {
		t.Fatal("reverse focus did not reveal Cancel")
	}
}

func TestLaunchDialogProgressErrorsAndDisabledSubmission(t *testing.T) {
	f := &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true}}
	m := launchModel(t, f)
	_, prepare := m.Update(key("L"))
	view := ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
	if !strings.Contains(view, "Preparing launch") || !strings.Contains(view, "Reading survey") || !strings.Contains(view, "╭") {
		t.Fatalf("preparation prompt missing: %s", view)
	}
	m.Update(prepare())
	m.launch.extra.text = "broken: [token-secret"
	m.Update(launchKey("tab"))
	if _, cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("invalid variables submitted")
	}
	view = ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
	if !strings.Contains(view, "Error: extra_vars:") || strings.Contains(view, "token-secret") || !strings.Contains(view, "> [ Launch job ]") {
		t.Fatalf("validation feedback missing or unsafe: %s", view)
	}
	m.launch.extra.text = "{}"
	_, submit := m.Update(key("enter"))
	view = ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
	if !strings.Contains(view, "Launching job") || !strings.Contains(view, "[ Cancel ]") {
		t.Fatalf("submission progress missing: %s", view)
	}
	f.launchErr = errors.New("safe failure token-secret")
	f.result = aap.LaunchResult{Ambiguous: true}
	m.Update(submit())
	view = ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
	if !strings.Contains(view, "safe failure [redacted]") || !strings.Contains(view, "Launch disabled: check Jobs") || strings.Contains(view, "token-secret") {
		t.Fatalf("ambiguous failure missing or unsafe: %s", view)
	}
	if _, retry := m.Update(key("enter")); retry != nil || f.posts != 1 {
		t.Fatal("ambiguous launch retried")
	}
	m.Update(key("esc"))
	if m.launch != nil || m.detailTop != 3 {
		t.Fatal("dismissal lost source context")
	}
}

func TestLaunchPreparationErrorFitsDialog(t *testing.T) {
	m := launchModel(t, &fakeLaunch{prepareErr: errors.New("Not permitted")})
	_, cmd := m.Update(key("L"))
	m.Update(cmd())
	view := ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
	for _, text := range []string{"Error: Not permitted", "Launch unavailable", "Esc dismiss", "╭"} {
		if !strings.Contains(view, text) {
			t.Fatalf("preparation failure missing %q: %s", text, view)
		}
	}
}

func TestLaunchValidationFeedbackStaysWithField(t *testing.T) {
	m := launchModel(t, &fakeLaunch{requirements: aap.LaunchRequirements{AskVariables: true, Questions: sampleQuestions()}})
	openLaunch(t, m)
	m.launch.inputs[0].text = ""
	m.launch.focus = len(m.launch.inputs) + 1
	if _, cmd := m.Update(key("enter")); cmd != nil || m.err == nil {
		t.Fatal("missing required-answer validation")
	}
	// Return to the invalid field without changing the error or other values.
	m.Update(launchKey("tab"))
	m.Update(launchKey("tab"))
	view := ansi.Strip(strings.Join(assertFrameSize(t, m), "\n"))
	for _, text := range []string{"Survey question 1:", "> 1. Title", "Error: an answer is required", "╔", "╚", "_"} {
		if !strings.Contains(view, text) {
			t.Fatalf("field validation missing %q: %s", text, view)
		}
	}
	if m.launch.inputs[1].text != "$encrypted$" {
		t.Fatal("validation changed another field")
	}
}
