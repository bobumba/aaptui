package tui

import (
	"context"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func cleanupModel(t *testing.T, m *Model) {
	t.Helper()
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
}

func TestBackgroundStylesAreLocal(t *testing.T) {
	m := New(context.Background(), "gateway")
	cleanupModel(t, m)
	other := New(context.Background(), "gateway")
	cleanupModel(t, other)
	dark := m.styles.title.Render("Title")
	if cmd := m.Init(); cmd == nil {
		t.Fatal("background query missing")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if m.styles.title.Render("Title") == dark {
		t.Fatal("light background did not change styles")
	}
	if other.styles.title.Render("Title") != dark {
		t.Fatal("background change affected another model")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if m.styles.title.Render("Title") != dark {
		t.Fatal("dark background did not restore styles")
	}
}

func TestStatusesRetainText(t *testing.T) {
	for _, dark := range []bool{false, true} {
		s := newStyles(dark)
		for _, status := range []string{"new", "pending", "waiting", "running", "successful", "failed", "error", "canceled", "unknown", "future_status"} {
			if got := ansi.Strip(s.status(status)); got != status {
				t.Fatalf("status text = %q, want %q", got, status)
			}
		}
		if selected := ansi.Strip(s.selected.Render("> Job")); !strings.HasPrefix(selected, "> ") {
			t.Fatal("selection marker missing without color")
		}
	}
}
