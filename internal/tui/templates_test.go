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

type fakeTemplates struct{ search string }

func (f *fakeTemplates) ListTemplates(ctx context.Context, o aap.ListOptions) (aap.Page[aap.TemplateSummary], error) {
	f.search = o.Search
	return aap.Page[aap.TemplateSummary]{Items: []aap.TemplateSummary{{Ref: aap.TemplateRef{ID: 7, Type: aap.PlaybookTemplate}, Name: "demo"}}}, nil
}
func (f *fakeTemplates) Template(ctx context.Context, r aap.TemplateRef) (aap.TemplateDetails, error) {
	return aap.TemplateDetails{TemplateSummary: aap.TemplateSummary{Ref: r, Name: "demo"}, Playbook: &aap.PlaybookSettings{Playbook: "site.yml"}}, nil
}
func TestTemplateNavigation(t *testing.T) {
	f := &fakeTemplates{}
	m := New(context.Background(), "").WithTemplates(f)
	defer m.Close()
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	if len(m.templatePage.Items) != 1 {
		t.Fatal("list")
	}
	m.Update(key("/"))
	m.Update(key("q"))
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if f.search != "q" || m.quitting {
		t.Fatal("search")
	}
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if m.screen != templateScreen || !strings.Contains(m.View().Content, "site.yml") {
		t.Fatal("details")
	}
	_, cmd = m.Update(key("esc"))
	if cmd != nil {
		m.Update(cmd())
	}
	if m.screen != templatesScreen {
		t.Fatal("back")
	}
}

func TestEveryTemplateCanBeSelectedAndOpened(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {237, 62}} {
		for _, count := range []int{1, 2, 50, 100} {
			for _, down := range []tea.KeyPressMsg{key("j"), {Code: tea.KeyDown}} {
				t.Run(fmt.Sprintf("%dx%d/%d/%s", size[0], size[1], count, down.String()), func(t *testing.T) {
					m := New(context.Background(), "gateway").WithTemplates(&fakeTemplates{})
					cleanupModel(t, m)
					m.screen = templatesScreen
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					for i := 0; i < count; i++ {
						m.templatePage.Items = append(m.templatePage.Items, aap.TemplateSummary{
							Ref:  aap.TemplateRef{ID: i + 1, Type: aap.PlaybookTemplate},
							Name: fmt.Sprintf("resource-%03d", i+1),
						})
					}
					for i := 0; i < count; i++ {
						if m.selected != i {
							t.Fatalf("selection = %d, want %d", m.selected, i)
						}
						name := m.templatePage.Items[i].Name
						found := false
						for _, line := range assertFrameSize(t, m) {
							row := strings.TrimSpace(ansi.Strip(line))
							found = found || (strings.HasPrefix(row, "> ") && strings.Contains(row, name))
						}
						if !found {
							t.Fatalf("selected template %q is not visible", name)
						}
						_, cmd := m.Update(key("enter"))
						if cmd == nil {
							t.Fatalf("template %q could not be opened", name)
						}
						m.Update(cmd())
						if m.screen != templateScreen || m.templateDetail.Ref.ID != i+1 {
							t.Fatalf("opened template %d on screen %d, want %d", m.templateDetail.Ref.ID, m.screen, i+1)
						}
						m.Update(key("esc"))
						m.Update(down)
					}
					if m.selected != count-1 {
						t.Fatalf("selection moved past the last template: %d", m.selected)
					}
				})
			}
		}
	}
}
