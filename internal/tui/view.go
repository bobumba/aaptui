package tui

import (
	"aaptui/internal/aap"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m *Model) View() tea.View {
	title := []string{"Main", "Job Templates", "Jobs", "Template details", "Job details", "Workflow children", "Output"}[m.screen]
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("AAP TUI · " + title))
	b.WriteString("\n" + m.connection + "\n\n")
	if m.screen == mainScreen {
		for i, label := range []string{"Job Templates", "Jobs"} {
			marker := "  "
			if i == m.selected {
				marker = "> "
			}
			b.WriteString(marker + label + "\n")
		}
	} else if m.screen == templatesScreen {
		if len(m.templatePage.Items) == 0 {
			switch {
			case m.loading:
				b.WriteString("Loading templates…\n")
			case m.err != nil:
				b.WriteString("Unable to load templates.\n")
			default:
				b.WriteString("No templates.\n")
			}
		}
		start := max(0, m.selected-min(aap.OutputLines, max(1, m.height-9))+1)
		for i := start; i < min(len(m.templatePage.Items), start+min(aap.OutputLines, max(1, m.height-9))); i++ {
			v := m.templatePage.Items[i]
			marker := "  "
			if i == m.selected {
				marker = "> "
			}
			fmt.Fprintf(&b, "%s%d · %s · %s\n", marker, v.Ref.ID, v.Name, v.Ref.Type)
		}
	} else if m.screen == templateScreen {
		d := m.templateDetail
		fmt.Fprintf(&b, "%d · %s · %s\n%s\nStatus: %s\n", d.Ref.ID, d.Name, d.Ref.Type, d.Description, d.Status)
		if d.Playbook != nil {
			fmt.Fprintf(&b, "Playbook: %s\nJob type: %s\nLimit: %s\n", d.Playbook.Playbook, d.Playbook.JobType, d.Playbook.Limit)
		}
		if d.Project != nil {
			fmt.Fprintf(&b, "SCM: %s\nURL: %s\nBranch: %s\n", d.Project.SCMType, d.Project.SCMURL, d.Project.SCMBranch)
		}
		if d.Inventory != nil {
			fmt.Fprintf(&b, "Source: %s\nPath: %s\n", d.Inventory.Source, d.Inventory.SourcePath)
		}
		if d.SystemJobType != "" {
			fmt.Fprintf(&b, "System job: %s\n", d.SystemJobType)
		}
		if d.ApprovalTimeout != nil {
			fmt.Fprintf(&b, "Approval timeout: %d\n", *d.ApprovalTimeout)
		}
	} else if m.screen == jobsScreen {
		if len(m.jobPage.Items) == 0 {
			switch {
			case m.loading:
				b.WriteString("Loading jobs…\n")
			case m.err != nil:
				b.WriteString("Unable to load jobs.\n")
			default:
				b.WriteString("No jobs.\n")
			}
		}
		start := max(0, m.selected-min(aap.OutputLines, max(1, m.height-9))+1)
		for i := start; i < min(len(m.jobPage.Items), start+min(aap.OutputLines, max(1, m.height-9))); i++ {
			v := m.jobPage.Items[i]
			marker := "  "
			if i == m.selected {
				marker = "> "
			}
			fmt.Fprintf(&b, "%s%d · %s · %s · %s\n", marker, v.Ref.ID, v.Name, v.Ref.Type, v.Status)
		}
	} else if m.screen == jobScreen {
		d := m.jobDetail
		fmt.Fprintf(&b, "%d · %s · %s\n%s\nStatus: %s · elapsed %.1fs\nStarted: %s\nFinished: %s\n", d.Ref.ID, d.Name, d.Ref.Type, d.Description, d.Status, d.Elapsed, d.Started, d.Finished)
		if d.Playbook != nil {
			fmt.Fprintf(&b, "Playbook: %s\n", d.Playbook.Playbook)
		}
		if d.Project != nil {
			fmt.Fprintf(&b, "SCM: %s · %s\n", d.Project.SCMType, d.Project.SCMBranch)
		}
		if d.Inventory != nil {
			fmt.Fprintf(&b, "Source: %s\n", d.Inventory.Source)
		}
		if d.ModuleName != "" {
			fmt.Fprintf(&b, "Module: %s %s\n", d.ModuleName, d.ModuleArgs)
		}
		if d.SystemJobType != "" {
			fmt.Fprintf(&b, "System job: %s\n", d.SystemJobType)
		}
		if d.ApprovalTimeout != nil {
			fmt.Fprintf(&b, "Approval timeout: %ds\n", *d.ApprovalTimeout)
		}
		fmt.Fprintf(&b, "Output: %s %s\nCancel: %s %s\nChildren: %s %s\n", d.Capabilities.Output.State, d.Capabilities.Output.Reason, d.Capabilities.Cancel.State, d.Capabilities.Cancel.Reason, d.Capabilities.Children.State, d.Capabilities.Children.Reason)
	} else if m.screen == workflowScreen {
		if len(m.nodePage.Items) == 0 {
			b.WriteString("No workflow nodes.\n")
		}
		start := max(0, m.selected-min(aap.OutputLines, max(1, m.height-9))+1)
		for i := start; i < min(len(m.nodePage.Items), start+min(aap.OutputLines, max(1, m.height-9))); i++ {
			v := m.nodePage.Items[i]
			marker := "  "
			if i == m.selected {
				marker = "> "
			}
			fmt.Fprintf(&b, "%s%s · %s · %s · %s\n", marker, v.Identifier, v.Name, v.Status, v.Reason)
		}
	} else if m.screen == outputScreen {
		fmt.Fprintf(&b, "Job %d · lines %d–%d of %d\n", m.jobDetail.Ref.ID, m.outputUpdate.Chunk.Start, m.outputUpdate.Chunk.End, m.outputUpdate.Chunk.AbsoluteEnd)
		state := "paused scrolling"
		if m.following {
			state = "following"
		}
		if m.outputUpdate.Complete {
			state += " · complete"
		} else if !m.outputUpdate.CompletionKnown {
			state += " · final-output evidence unknown"
		}
		if m.outputUpdate.Retrying {
			state += " · reconnecting"
		}
		fmt.Fprintf(&b, "%s · %s\n", state, m.outputUpdate.Status)
		for _, line := range strings.Split(strings.TrimSuffix(m.outputUpdate.Chunk.Text, "\n"), "\n") {
			b.WriteString(ansi.Cut(line, m.outputLeft, m.outputLeft+m.width) + "\n")
		}
	} else {
		b.WriteString("No resources loaded.\n")
	}
	content := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	footer := []string{}
	if m.confirming {
		footer = append(footer, fmt.Sprintf("Cancel job %d (%s · %s)?", m.confirmation.ID, m.jobDetail.Name, m.confirmation.Type), "y/Enter confirm · n/Esc dismiss")
	}
	if m.editing {
		footer = append(footer, "Search: "+m.search+"_ · Enter apply · Esc exit input")
	} else {
		switch m.screen {
		case mainScreen:
			footer = append(footer, "↑/↓ select · Enter open · q quit · Ctrl-C quit")
		case templatesScreen, jobsScreen:
			footer = append(footer, "↑/↓ select · Enter open · / search · r refresh · n/p page", "Esc back · q quit · Ctrl-C quit")
		case templateScreen:
			footer = append(footer, "↑/↓ scroll · Esc back · q quit · Ctrl-C quit")
		case jobScreen:
			footer = append(footer, "o output · w children · c cancel · r refresh · ↑/↓ scroll", "Esc back · q quit · Ctrl-C quit")
		case workflowScreen:
			footer = append(footer, "↑/↓ select · Enter child · r refresh · n/p page", "Esc back · q quit · Ctrl-C quit")
		case outputScreen:
			footer = append(footer, "↑/↓ PgUp/PgDown scroll · ←/→ pan · f follow · r resume", "Esc back · q quit · Ctrl-C quit")
		}
	}
	if m.loading {
		footer = append([]string{"Loading…"}, footer...)
	}
	if m.err != nil {
		messages := []string{m.err.Error()}
		var apiErr *aap.APIError
		if errors.As(m.err, &apiErr) && apiErr.Kind == aap.Authentication {
			messages = append(messages,
				"HTTP 401: server rejected the token. Check AAP_TOKEN.",
				"Check AAP_URL and AAP_CONNECTION_MODE match the token's server.",
				"After changing environment settings, restart aaptui.")
		}
		footer = append(messages, footer...)
	}
	available := max(0, m.height-len(footer))
	if (m.screen == jobScreen || m.screen == templateScreen) && len(content) >= 3 && available > 3 {
		header := content[:3]
		body := strings.Split(ansi.Hardwrap(strings.Join(content[3:], "\n"), m.width, true), "\n")
		top := min(m.detailTop, max(0, len(body)-(available-3)))
		content = append(header, body[top:]...)
	}
	if len(content) > available {
		content = content[:available]
	}
	lines := append(content, footer...)
	if len(lines) > m.height {
		lines = lines[len(lines)-m.height:]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
