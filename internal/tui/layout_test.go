package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func assertFrameSize(t *testing.T, m *Model) []string {
	t.Helper()
	view := m.View()
	if !view.AltScreen {
		t.Fatal("view left alternate screen")
	}
	lines := strings.Split(view.Content, "\n")
	if len(lines) != m.height {
		t.Fatalf("frame has %d rows, want %d: %q", len(lines), m.height, view.Content)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("row exceeds %d columns: %q", m.width, line)
		}
	}
	return lines
}

func TestStyledFramesFitTerminal(t *testing.T) {
	for screen := mainScreen; screen <= outputScreen; screen++ {
		for _, size := range [][2]int{{1, 1}, {2, 2}, {19, 6}, {30, 10}, {40, 12}, {80, 24}} {
			for _, state := range []string{"normal", "loading", "error", "search", "confirmation"} {
				t.Run(fmt.Sprintf("%d/%dx%d/%s", screen, size[0], size[1], state), func(t *testing.T) {
					m := New(context.Background(), "gateway · https://aap.example.com")
					cleanupModel(t, m)
					m.screen = screen
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					name := "部署 café 👩‍💻 " + strings.Repeat("long name ", 10)
					m.templatePage.Items = []aap.TemplateSummary{{Name: name}}
					m.jobPage.Items = []aap.JobSummary{{Name: name, Status: "running"}}
					m.nodePage.Items = []aap.WorkflowNode{{Name: name, Status: "pending", Reason: "Not launched"}}
					m.templateDetail.Description = name
					m.jobDetail.Description = name
					m.outputUpdate = aap.OutputUpdate{Chunk: aap.OutputChunk{Text: strings.Repeat(name+"\n", 20)}, Retrying: true}
					switch state {
					case "loading":
						m.loading = true
					case "error":
						m.err = errors.New(strings.Repeat("service unavailable ", 20))
					case "search":
						m.editing = true
						m.search = name
					case "confirmation":
						m.confirming = true
						m.confirmation = aap.JobRef{ID: 42, Type: aap.WorkflowJob}
						m.jobDetail.Name = name
					}
					assertFrameSize(t, m)
				})
			}
		}
	}
}

func TestSelectionVisibleWithMessages(t *testing.T) {
	for _, screen := range []screen{templatesScreen, jobsScreen, workflowScreen} {
		for _, width := range []int{30, 80} {
			m := New(context.Background(), "gateway")
			m.screen, m.width, m.height = screen, width, 10
			m.err = errors.New(strings.Repeat("retry later ", 20))
			for i := 0; i < 50; i++ {
				name := fmt.Sprintf("resource%d", i)
				m.templatePage.Items = append(m.templatePage.Items, aap.TemplateSummary{Name: name})
				m.jobPage.Items = append(m.jobPage.Items, aap.JobSummary{Name: name, Status: "running"})
				m.nodePage.Items = append(m.nodePage.Items, aap.WorkflowNode{Name: name, Status: "pending"})
			}
			m.selected = 49
			lines := assertFrameSize(t, m)
			found := false
			for _, line := range lines {
				text := strings.TrimSpace(ansi.Strip(line))
				if strings.HasPrefix(text, "> ") && strings.Contains(text, "resource49") {
					found = true
				}
			}
			if !found {
				t.Fatalf("screen %d width %d: selection hidden: %q", screen, width, lines)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestColumnsAlignUnicodeAndCompactRows(t *testing.T) {
	m := New(context.Background(), "gateway")
	cleanupModel(t, m)
	m.screen = jobsScreen
	for _, width := range []int{30, 50, 78} {
		row := ansi.Strip(m.resourceRow("42", "部署 café", "job", "running", false, width))
		header := ansi.Strip(m.columnHeader(width))
		statusColumn := func(text, label string) int {
			i := strings.Index(text, label)
			if i < 0 {
				t.Fatalf("missing %q in %q", label, text)
			}
			return ansi.StringWidth(text[:i])
		}
		if statusColumn(row, "running") != statusColumn(header, "STATUS") {
			t.Fatalf("status column misaligned at width %d: %q / %q", width, row, header)
		}
		if !strings.Contains(row, "部署 café") {
			t.Fatalf("name lost at width %d: %q", width, row)
		}
		if strings.Contains(row, "job") != (width >= 64) {
			t.Fatalf("unexpected type column at width %d: %q", width, row)
		}
		selected := m.resourceRow("42", "部署 café", "job", "running", true, width)
		if ansi.StringWidth(selected) != width || !strings.HasPrefix(ansi.Strip(selected), "> ") {
			t.Fatalf("selection does not fill its row: %q", selected)
		}
	}
}

func TestLongSearchKeepsCursorVisible(t *testing.T) {
	for _, width := range []int{30, 80} {
		m := New(context.Background(), "gateway")
		m.screen, m.width, m.height, m.editing = jobsScreen, width, 10, true
		m.search = strings.Repeat("部署", 40) + "tail"
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "Search:") || !strings.Contains(view, "tail_") || !strings.Contains(view, "Esc exit input") {
			t.Fatalf("search cursor/help hidden at width %d: %q", width, view)
		}
		assertFrameSize(t, m)
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConfirmationAndAuthenticationMessages(t *testing.T) {
	m := cancelModel(&fakeCancel{})
	cleanupModel(t, m)
	m.width, m.height = 30, 10
	m.jobDetail.Name = "nightly deploy"
	m.Update(key("c"))
	view := ansi.Strip(m.View().Content)
	for _, text := range []string{"Cancel job 1 (job)?", "nightly deploy", "y/Enter confirm", "n/Esc"} {
		if !strings.Contains(view, text) {
			t.Fatalf("confirmation missing %q: %q", text, view)
		}
	}
	if strings.Contains(view, "c cancel") {
		t.Fatal("ordinary shortcuts shown while confirmation has focus")
	}
	assertFrameSize(t, m)
	m.Update(key("esc"))
	m.width, m.height = 80, 24
	m.err = &aap.APIError{Kind: aap.Authentication, Operation: "read job"}
	view = ansi.Strip(m.View().Content)
	for _, text := range []string{"Error:", "HTTP 401", "AAP_TOKEN", "AAP_CONNECTION_MODE", "restart aaptui", "Esc back"} {
		if !strings.Contains(view, text) {
			t.Fatalf("authentication help missing %q: %q", text, view)
		}
	}
	assertFrameSize(t, m)
}

type measuredOutput struct {
	fakeOutput
	start, end, nextLines int
}

func (f *measuredOutput) Read(ctx context.Context, start, end int) (aap.OutputUpdate, error) {
	f.start, f.end = start, end
	return f.fakeOutput.Read(ctx, start, end)
}

func (f *measuredOutput) Next(lines int) (aap.OutputUpdate, error) {
	f.nextLines = lines
	return f.fakeOutput.Next(lines)
}

func TestOutputRequestsAndPagingMatchVisibleRows(t *testing.T) {
	m := New(context.Background(), "gateway")
	cleanupModel(t, m)
	f := &measuredOutput{}
	m.screen, m.width, m.height, m.output = outputScreen, 80, 24, f
	m.outputUpdate.CompletionKnown = true
	m.outputUpdate.Chunk.AbsoluteEnd = 1000
	// Five header rows and two help rows leave seventeen output rows.
	m.nextOutput()()
	if f.nextLines != 17 {
		t.Fatalf("live window = %d, want 17", f.nextLines)
	}
	m.readOutput(50)()
	if f.start != 50 || f.end != 67 {
		t.Fatalf("read window = %d–%d, want 50–67", f.start, f.end)
	}
	m.loading = false
	m.outputTop = 50
	_, cmd := m.Update(key("pgdown"))
	cmd()
	if f.start != 67 || f.end != 84 {
		t.Fatalf("page window = %d–%d, want 67–84", f.start, f.end)
	}
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	if cmd := m.nextOutput(); cmd != nil {
		t.Fatal("scheduled another live request while one was pending")
	}
	m.outputPending = false
	m.nextOutput()()
	if f.nextLines != 2 {
		t.Fatalf("resized window = %d, want 2", f.nextLines)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 1000})
	m.outputPending = false
	m.nextOutput()()
	if f.nextLines != aap.OutputLines {
		t.Fatalf("output window exceeds API bound: %d", f.nextLines)
	}
}

func TestOutputTailAndPanAfterResize(t *testing.T) {
	m := New(context.Background(), "gateway")
	cleanupModel(t, m)
	f := &measuredOutput{}
	m.screen, m.width, m.height, m.output = outputScreen, 40, 12, f
	m.following = true
	var text strings.Builder
	for i := 10; i < 30; i++ {
		fmt.Fprintf(&text, "line%02d 部署 %s\n", i, strings.Repeat("x", 50))
	}
	m.outputUpdate = aap.OutputUpdate{Chunk: aap.OutputChunk{Start: 10, End: 30, AbsoluteEnd: 30, Text: text.String()}, CompletionKnown: true}
	lines := assertFrameSize(t, m)
	// At 40 columns, five header and three footer rows leave four log rows.
	for i, line := range lines[5:9] {
		want := ansi.Cut(fmt.Sprintf("line%02d 部署 %s", 26+i, strings.Repeat("x", 50)), 0, 38)
		if line != " "+want+" " {
			t.Fatalf("log styling/content changed: %q, want %q", line, want)
		}
	}
	if !strings.Contains(ansi.Strip(lines[3]), "lines 26–30") {
		t.Fatalf("range does not describe visible tail: %q", lines[3])
	}
	m.outputLeft = 8
	lines = assertFrameSize(t, m)
	want := "部署 " + strings.Repeat("x", 33)
	if lines[5] != " "+want+" " {
		t.Fatalf("pan did not use content width: %q", lines[5])
	}
	_, cmd := m.Update(key("k"))
	cmd()
	if m.following || f.start != 25 || f.end != 29 {
		t.Fatalf("scroll did not begin at visible tail: following=%t range=%d–%d", m.following, f.start, f.end)
	}
}
