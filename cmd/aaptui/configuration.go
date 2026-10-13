package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type configurationChoice struct {
	ctx      context.Context
	canLoad  bool
	status   string
	selected int
	width    int
	done     bool
}

func newConfigurationChoice(canLoad bool, status string) *configurationChoice {
	m := &configurationChoice{canLoad: canLoad, status: status, width: 80}
	if !canLoad {
		m.selected = 1
	}
	return m
}

func (m *configurationChoice) Init() tea.Cmd {
	if m.ctx == nil {
		return nil
	}
	return func() tea.Msg {
		<-m.ctx.Done()
		return tea.QuitMsg{}
	}
}

func (m *configurationChoice) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c", "ctrl+d", "q":
			return m, tea.Quit
		case "up", "k", "shift+tab":
			m.selected = 0
		case "down", "j":
			m.selected = 1
		case "tab":
			m.selected = 1 - m.selected
		case "enter":
			if m.selected == 0 && !m.canLoad {
				return m, nil
			}
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *configurationChoice) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	choices := []string{"Load current configuration", "Create new configuration"}
	if !m.canLoad {
		choices[0] += " (unavailable)"
	}
	for i := range choices {
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		choices[i] = prefix + choices[i]
	}
	lines := append([]string{"AAP startup configuration", m.status, ""}, choices...)
	lines = append(lines, "", "↑/↓ or Tab: select · Enter: continue · Esc/Ctrl-C/Ctrl-D: cancel")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "")
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func promptConfigurationChoice(ctx context.Context, canLoad bool, status string) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newConfigurationChoice(canLoad, status)
	m.ctx = ctx
	_, err := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithInput(setupInput{os.Stdin, cancel}), tea.WithOutput(terminalOutput{os.Stdout}), tea.WithoutSignalHandler()).Run()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return false, fmt.Errorf("configuration choice terminal failed")
	}
	if !m.done {
		return false, context.Canceled
	}
	return m.selected == 1, nil
}
