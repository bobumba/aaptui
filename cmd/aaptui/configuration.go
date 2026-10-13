package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/config"
)

type configurationOption struct {
	label     string
	available bool
}

type configurationChoice struct {
	ctx      context.Context
	options  []configurationOption
	status   string
	selected int
	width    int
	height   int
	done     bool
}

func newConfigurationChoice(connections []config.Connection, status string) *configurationChoice {
	m := &configurationChoice{status: status, width: 80, height: 24}
	for _, c := range connections {
		label := "Invalid saved URL"
		if config.Validate(config.Settings{URL: c.Settings.URL, Mode: config.Gateway}) == nil {
			label = c.Settings.URL
		}
		for _, other := range connections {
			if token := other.Secrets.Token(); token != "" {
				label = strings.ReplaceAll(label, token, "[redacted]")
			}
		}
		if c.Err != nil {
			label += " (unavailable)"
		}
		m.options = append(m.options, configurationOption{label, c.Err == nil})
	}
	m.options = append(m.options, configurationOption{"Create new configuration", true})
	for !m.options[m.selected].available {
		m.selected++
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
		m.height = max(1, msg.Height)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c", "ctrl+d", "q":
			return m, tea.Quit
		case "up", "k", "shift+tab":
			m.selected = max(0, m.selected-1)
		case "down", "j":
			m.selected = min(len(m.options)-1, m.selected+1)
		case "tab":
			m.selected = (m.selected + 1) % len(m.options)
		case "enter":
			if !m.options[m.selected].available {
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
	// Window the list around selection so long configurations fit the terminal.
	rows := max(1, m.height-5)
	start := max(0, m.selected-rows+1)
	end := min(len(m.options), start+rows)
	lines := []string{"AAP startup configuration", m.status, ""}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		lines = append(lines, prefix+m.options[i].label)
	}
	lines = append(lines, "", "↑/↓ or Tab: select · Enter: continue · Esc/Ctrl-C/Ctrl-D: cancel")
	if m.height < 6 {
		lines = lines[3 : len(lines)-2]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "")
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func promptConfigurationChoice(ctx context.Context, connections []config.Connection, status string) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newConfigurationChoice(connections, status)
	m.ctx = ctx
	_, err := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithInput(setupInput{os.Stdin, cancel}), tea.WithOutput(terminalOutput{os.Stdout}), tea.WithoutSignalHandler()).Run()
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err != nil {
		return -1, fmt.Errorf("configuration choice terminal failed")
	}
	if !m.done {
		return -1, context.Canceled
	}
	return m.selected, nil
}
