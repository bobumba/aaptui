package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
)

func TestSearchViewKeepsFooterAtBottom(t *testing.T) {
	for _, screen := range []screen{templatesScreen, projectsScreen, jobsScreen} {
		for _, size := range [][2]int{{80, 24}, {30, 10}, {1, 1}} {
			t.Run(fmt.Sprintf("screen%d/%dx%d", screen, size[0], size[1]), func(t *testing.T) {
				m := New(context.Background(), "gateway").WithTemplates(&fakeTemplates{}).WithProjects(&fakeProjects{}).WithJobs(&fakeJobs{})
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
					width := size[0]
					if width >= 40 {
						width -= 2
					}
					if got, want := strings.TrimSpace(ansi.Strip(lines[len(lines)-1])), ansi.Truncate(footer, width, ""); got != want {
						t.Fatalf("bottom row = %q, want %q", got, want)
					}
				}
				help := "Esc back · q quit · Ctrl-C quit"
				check(help)
				m.Update(key("/"))
				m.Update(key("demo"))
				inputHelp := "Search: demo_ · Enter apply · Esc exit input"
				if size[0] < 60 {
					inputHelp = "Enter apply · Esc exit input"
				}
				check(inputHelp)
				m.Update(key("backspace"))
				if size[0] >= 60 {
					inputHelp = "Search: dem_ · Enter apply · Esc exit input"
				}
				check(inputHelp)
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

func runTerminal(t *testing.T, m *Model) (*tea.Program, func(func(string) bool)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	writes := make(terminalWrites, 64)
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(writes), tea.WithWindowSize(80, 24), tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Error(err)
		}
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
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
	return p, waitFor
}

func TestListResultsRepaintUnchangedView(t *testing.T) {
	for _, screen := range []screen{templatesScreen, projectsScreen, jobsScreen, workflowScreen, inventoriesScreen, groupsScreen, hostsScreen} {
		t.Run(fmt.Sprintf("screen%d", screen), func(t *testing.T) {
			m := New(context.Background(), "gateway")
			m.screen = screen
			result := resultMsg{}
			switch screen {
			case templatesScreen, projectsScreen:
				m.templatePage.Items = []aap.TemplateSummary{{Name: "initial resource"}}
				result.value = m.templatePage
			case jobsScreen:
				m.jobPage.Items = []aap.JobSummary{{Name: "initial resource"}}
				result.value = m.jobPage
			case workflowScreen:
				m.nodePage.Items = []aap.WorkflowNode{{Name: "initial resource"}}
				result.value = m.nodePage
			case inventoriesScreen:
				m.inventoryPage.Items = []aap.InventorySummary{{Name: "initial resource"}}
				result.value = m.inventoryPage
			case groupsScreen:
				m.groupPage.Items = []aap.GroupSummary{{Name: "initial resource"}}
				result.value = m.groupPage
			case hostsScreen:
				m.hostPage.Items = []aap.HostSummary{{Name: "initial resource"}}
				result.value = m.hostPage
			}
			p, waitFor := runTerminal(t, m)
			waitFor(func(s string) bool { return strings.Contains(s, "initial resource") })
			// A clear command can arrive after the result's frame has already
			// been flushed. It must repaint without waiting for selection to move,
			// including when a refresh returns the same data.
			p.Send(result)
			waitFor(func(s string) bool {
				return strings.Contains(s, "\x1b[2J") && strings.Contains(s, "initial resource")
			})
		})
	}
}

func TestEscapeRepaintsRestoredScreen(t *testing.T) {
	for _, screen := range []screen{templatesScreen, projectsScreen, jobsScreen, templateScreen, jobScreen, workflowScreen, outputScreen} {
		t.Run(fmt.Sprintf("screen%d", screen), func(t *testing.T) {
			m := New(context.Background(), "gateway")
			parent := "Job Templates"
			switch screen {
			case templateScreen:
				m.screen = templatesScreen
				m.templatePage.Items = []aap.TemplateSummary{{Name: "parent resource"}}
				parent = "parent resource"
			case jobScreen:
				m.screen = jobsScreen
				m.jobPage.Items = []aap.JobSummary{{Name: "parent resource"}}
				parent = "parent resource"
			case workflowScreen, outputScreen:
				m.screen = jobScreen
				m.jobDetail.Name = "parent resource"
				parent = "parent resource"
			}
			m.move(screen)
			m.templatePage.Items = []aap.TemplateSummary{{Name: "child resource"}}
			m.jobPage.Items = []aap.JobSummary{{Name: "child resource"}}
			m.nodePage.Items = []aap.WorkflowNode{{Name: "child resource"}}
			m.templateDetail.Name = "child resource"
			m.jobDetail.Name = "child resource"
			m.outputUpdate.Chunk.Text = "child resource\n"
			if screen == outputScreen {
				m.output = &fakeOutput{}
				m.sessions = append(m.sessions, m.output)
			}
			p, waitFor := runTerminal(t, m)
			waitFor(func(s string) bool { return strings.Contains(s, "child resource") })
			p.Send(key("esc"))
			waitFor(func(s string) bool {
				return strings.Contains(s, "\x1b[2J") && strings.Contains(s, parent) && !strings.Contains(s, "child resource")
			})
		})
	}
}

func TestSearchResultsRepaintTerminal(t *testing.T) {
	for _, screen := range []screen{templatesScreen, projectsScreen, jobsScreen} {
		t.Run(fmt.Sprintf("screen%d", screen), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			m := New(ctx, "gateway").WithTemplates(&fakeTemplates{}).WithProjects(&fakeProjects{}).WithJobs(&fakeJobs{})
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
