package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func TestDetailTableColumnsAndThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		m := New(context.Background(), "")
		cleanupModel(t, m)
		m.styles = newStyles(dark)
		if !m.styles.detailLabel.GetBold() || m.styles.detailLabel.GetForeground() == nil {
			t.Fatal("detail labels need bold and a theme color")
		}
		fields := detailFields{
			{label: "ID", value: "42"},
			{label: "Name", value: "部署 café 👩‍💻"},
			{label: "Enabled", value: "false"},
			{label: "Count", value: "0"},
		}
		wide := m.renderDetailFields(fields, 160)
		narrow := m.renderDetailFields(fields, 80)
		if len(wide) != 2 || len(narrow) != 4 {
			t.Fatalf("expected side-by-side pairs at width 160 and single pairs at 80: %q / %q", wide, narrow)
		}
		column := func(line, value string) int {
			t.Helper()
			text := ansi.Strip(line)
			i := strings.Index(text, value)
			if i < 0 {
				t.Fatalf("missing %q in %q", value, text)
			}
			return ansi.StringWidth(text[:i])
		}
		if column(wide[0], "42") != column(wide[1], "false") ||
			column(wide[0], "Name:") != column(wide[1], "Count:") ||
			column(wide[0], "部署") != column(wide[1], "0") {
			t.Fatalf("field/value columns are misaligned: %q", wide)
		}
		for _, layout := range []struct {
			lines []string
			width int
		}{{wide, 160}, {narrow, 80}} {
			for _, line := range layout.lines {
				if ansi.StringWidth(line) > layout.width {
					t.Fatalf("table exceeds width: %q", line)
				}
			}
		}
		if !strings.HasPrefix(narrow[0], m.styles.detailLabel.Render("ID:     ")+"  42") ||
			!strings.Contains(narrow[0], "\x1b[") ||
			strings.Contains(narrow[0], m.styles.detailLabel.Render("42")) {
			t.Fatalf("field labels and values do not have distinct styling: %q", narrow[0])
		}
	}
	light, dark := newStyles(false), newStyles(true)
	if light.detailLabel.Render("Field") == dark.detailLabel.Render("Field") {
		t.Fatal("detail label color does not adapt to the background")
	}
}

func TestDetailTableWrapsWithoutLosingValues(t *testing.T) {
	for _, width := range []int{8, 30, 80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := New(context.Background(), "")
			cleanupModel(t, m)
			value := "first line\n\n" + strings.Repeat("部署 café 👩‍💻 ", 15) + "last line"
			lines := m.renderDetailFields(detailFields{{label: "Description", value: value}}, width)
			// The label occupies the first column; collect the wrapped value
			// column to check that all graphemes and explicit blank lines remain.
			labelWidth := -1
			var recovered strings.Builder
			sawBlank := false
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("row exceeds %d columns: %q", width, line)
				}
				if labelWidth < 0 {
					plain := ansi.Strip(line)
					pos := strings.Index(plain, "fi")
					if pos < 0 {
						t.Fatalf("first value missing: %q", line)
					}
					labelWidth = ansi.StringWidth(plain[:pos])
				}
				fragment := ansi.Strip(ansi.Cut(line, labelWidth, width))
				if strings.TrimSpace(fragment) == "" {
					sawBlank = true
				}
				recovered.WriteString(fragment)
			}
			compact := func(text string) string { return strings.Join(strings.Fields(text), "") }
			if compact(recovered.String()) != compact(value) || !sawBlank {
				t.Fatalf("wrapped value or blank line lost: %q", lines)
			}
		})
	}
}

func TestDetailTableLongLabelsAndRows(t *testing.T) {
	m := New(context.Background(), "")
	cleanupModel(t, m)
	fields := detailFields{
		{label: "Organization host limit error", value: "false"},
		{label: "Repository", value: strings.Repeat("x", 170) + "END"},
		{label: "Zero", value: "0"},
	}
	lines := m.renderDetailFields(fields, 120)
	var label, value strings.Builder
	for _, line := range lines {
		if ansi.StringWidth(line) > 120 {
			t.Fatalf("long row exceeds table width: %q", line)
		}
		if strings.Contains(ansi.Strip(line), "Zero:") {
			if !strings.Contains(line, "0") {
				t.Fatal("odd trailing field lost its value")
			}
			break
		}
		// At 120 columns each pair occupies 58 cells with 28 for labels.
		label.WriteString(ansi.Strip(ansi.Cut(line, 0, 28)))
		value.WriteString(ansi.Strip(ansi.Cut(line, 91, 120)))
	}
	if strings.Join(strings.Fields(label.String()), "") != "Organizationhostlimiterror:" ||
		strings.Join(strings.Fields(value.String()), "") != fields[1].value {
		t.Fatalf("long label or value lost: %q", lines)
	}
	if lines := m.renderDetailFields(fields, 0); lines != nil {
		t.Fatal("zero-width table should be empty")
	}
	for _, width := range []int{1, 4, 7} {
		lines := m.renderDetailFields(detailFields{{label: "ID", value: "42"}}, width)
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("stacked pair exceeds width %d: %q", width, line)
			}
		}
		if strings.Join(strings.Fields(ansi.Strip(strings.Join(lines, "\n"))), "") != "ID:42" {
			t.Fatalf("stacked pair lost content: %q", lines)
		}
	}
}

func TestResourceDetailTablesResizeScrollAndBack(t *testing.T) {
	for _, resource := range []struct {
		name    string
		screen  screen
		project bool
	}{
		{"job", jobScreen, false}, {"template", templateScreen, false},
		{"project", templateScreen, true}, {"inventory", inventoryScreen, false},
		{"group", groupScreen, false}, {"host", hostScreen, false}, {"node", nodeScreen, false},
	} {
		t.Run(resource.name, func(t *testing.T) {
			m := New(context.Background(), "").WithTemplateJobs(&fakeTemplateJobs{})
			cleanupModel(t, m)
			m.move(resource.screen)
			m.width, m.height = 180, 24
			long := strings.Repeat("resource details ", 150) + "TAIL"
			m.jobDetail.Description, m.templateDetail.Description = long, long
			m.inventoryDetail.Description, m.groupDetail.Description = long, long
			m.hostDetail.Description, m.nodeDetail.Reason = long, long
			m.templateDetail.Ref.Type = aap.PlaybookTemplate
			if resource.project {
				m.templateDetail.Ref.Type = aap.ProjectTemplate
			}
			m.templateJobPage.Items = []aap.JobSummary{{Name: "selected job"}}
			if resource.screen == inventoryScreen || resource.screen == groupScreen {
				m.Update(key("tab"))
			}
			m.Update(key("j"))
			for _, width := range []int{180, 30, 80, 180} {
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
				assertFrameSize(t, m)
				if m.screen != resource.screen || m.selected != 0 || m.detailTop != 1 {
					t.Fatal("resize changed navigation")
				}
			}
			sawTail := false
			for i := 0; i < 200; i++ {
				m.Update(key("j"))
				sawTail = sawTail || strings.Contains(ansi.Strip(m.View().Content), "TAIL")
				if sawTail {
					break
				}
			}
			if !sawTail || m.selected != 0 {
				t.Fatal("cannot scroll to the full field without changing selection")
			}
			if m.hasTemplateJobs() {
				m.Update(key("tab"))
				if !m.templateJobsFocus || !strings.Contains(ansi.Strip(m.View().Content), "> ") {
					t.Fatal("job table focus lost")
				}
			}
			if resource.screen == inventoryScreen || resource.screen == groupScreen {
				m.Update(key("tab"))
				if m.objectDetailsFocus || !strings.Contains(ansi.Strip(m.View().Content), "> ") {
					t.Fatal("menu focus lost")
				}
			}
			m.Update(key("esc"))
			if m.screen != mainScreen {
				t.Fatal("back navigation lost")
			}
		})
	}
}
