package tui

import (
	"context"
	"strings"
	"testing"

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
