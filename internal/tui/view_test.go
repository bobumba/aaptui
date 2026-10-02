package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func TestSearchViewKeepsFooterAtBottom(t *testing.T) {
	for _, screen := range []screen{templatesScreen, jobsScreen} {
		for _, size := range [][2]int{{80, 24}, {30, 10}, {1, 1}} {
			t.Run(fmt.Sprintf("screen%d/%dx%d", screen, size[0], size[1]), func(t *testing.T) {
				m := New(context.Background(), "gateway").WithTemplates(&fakeTemplates{}).WithJobs(&fakeJobs{})
				defer m.Close()
				m.screen = screen
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for i := 0; i < 30; i++ {
					name := fmt.Sprintf("original result %d", i)
					m.templatePage.Items = append(m.templatePage.Items, aap.TemplateSummary{Name: name})
					m.jobPage.Items = append(m.jobPage.Items, aap.JobSummary{Name: name})
				}
				check := func(footer string) {
					t.Helper()
					view := m.View()
					if !view.AltScreen {
						t.Fatal("search must remain in the alternate screen")
					}
					lines := strings.Split(view.Content, "\n")
					if len(lines) != size[1] {
						t.Fatalf("frame has %d rows, want %d: %q", len(lines), size[1], view.Content)
					}
					for _, line := range lines {
						if ansi.StringWidth(line) > size[0] {
							t.Fatalf("line exceeds terminal width: %q", line)
						}
					}
					if got, want := lines[len(lines)-1], ansi.Truncate(footer, size[0], ""); got != want {
						t.Fatalf("bottom row = %q, want %q", got, want)
					}
				}
				help := "Esc back · q quit · Ctrl-C quit"
				check(help)
				m.Update(key("/"))
				m.Update(key("demo"))
				check("Search: demo_ · Enter apply · Esc exit input")
				m.Update(key("backspace"))
				check("Search: dem_ · Enter apply · Esc exit input")
				_, cmd := m.Update(key("enter"))
				if cmd == nil || !m.loading {
					t.Fatal("search did not start a request")
				}
				check(help)
				_, repaint := m.Update(cmd())
				if repaint == nil || repaint() != tea.ClearScreen() {
					t.Fatal("replacement search results did not request a full redraw")
				}
				check(help)
				if strings.Contains(m.View().Content, "original result") {
					t.Fatal("old results remain in the search frame")
				}
				// Empty results and a resized terminal must also retain the footer.
				m.templatePage = aap.Page[aap.TemplateSummary]{}
				m.jobPage = aap.Page[aap.JobSummary]{}
				check(help)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1] + 3})
				size[1] += 3
				check(help)
				m.Update(key("/"))
				m.Update(key("esc"))
				check(help)
			})
		}
	}
}

// Capture renderer writes so this regression exercises terminal redraws as well
// as the model's view. The existing headless test disables the renderer.
type terminalWrites chan string

func (w terminalWrites) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func TestSearchResultsRepaintTerminal(t *testing.T) {
	for _, screen := range []screen{templatesScreen, jobsScreen} {
		t.Run(fmt.Sprintf("screen%d", screen), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			m := New(ctx, "gateway").WithTemplates(&fakeTemplates{}).WithJobs(&fakeJobs{})
			m.screen = screen
			m.templatePage.Items = []aap.TemplateSummary{{Name: "original result"}}
			m.jobPage.Items = []aap.JobSummary{{Name: "original result"}}
			writes := make(terminalWrites, 32)
			p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(writes), tea.WithWindowSize(80, 24), tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithoutSignalHandler())
			done := make(chan struct{})
			var runErr error
			go func() {
				_, runErr = p.Run()
				close(done)
			}()
			defer func() {
				cancel()
				<-done
				if err := m.Close(); err != nil {
					t.Error(err)
				}
			}()
			waitFor := func(match func(string) bool) {
				t.Helper()
				for {
					select {
					case output := <-writes:
						if match(output) {
							return
						}
					case <-ctx.Done():
						t.Fatal("timed out waiting for terminal redraw")
					}
				}
			}
			waitFor(func(s string) bool { return strings.Contains(s, "original result") })
			p.Send(key("/"))
			waitFor(func(s string) bool { return strings.Contains(s, "Search:") })
			p.Send(key("demo"))
			p.Send(key("enter"))
			waitFor(func(s string) bool {
				return strings.Contains(s, "\x1b[2J") && strings.Contains(s, "demo") && !strings.Contains(s, "original result")
			})
			p.Send(key("q"))
			<-done
			if runErr != nil {
				t.Fatal(runErr)
			}
		})
	}
}
