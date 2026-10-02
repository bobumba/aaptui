package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(s string) tea.KeyPressMsg {
	k := tea.Key{Text: s}
	switch s {
	case "enter":
		k.Text = ""
		k.Code = tea.KeyEnter
	case "esc":
		k.Text = ""
		k.Code = tea.KeyEscape
	case "ctrl+c":
		k.Text = ""
		k.Code = 'c'
		k.Mod = tea.ModCtrl
	case "backspace":
		k.Text = ""
		k.Code = tea.KeyBackspace
	default:
		if len(s) == 1 {
			k.Code = rune(s[0])
		}
	}
	return tea.KeyPressMsg(k)
}
func TestNavigationInputShutdown(t *testing.T) {
	m := New(context.Background(), "gateway")
	defer m.Close()
	m.Update(key("enter"))
	if m.screen != templatesScreen {
		t.Fatal("open")
	}
	m.Update(key("/"))
	m.Update(key("q"))
	if m.quitting || m.search != "q" {
		t.Fatal("input quit")
	}
	m.Update(key("esc"))
	if m.editing || m.screen != templatesScreen {
		t.Fatal("input escape")
	}
	m.Update(key("esc"))
	if m.screen != mainScreen {
		t.Fatal("back")
	}
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	if m.width != 30 || m.height != 10 {
		t.Fatal("resize")
	}
	m.Update(key("ctrl+c"))
	if !m.quitting || m.ctx.Err() == nil {
		t.Fatal("shutdown")
	}
}
func TestStaleResult(t *testing.T) {
	m := New(context.Background(), "")
	defer m.Close()
	cmd := m.begin(func(ctx context.Context) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	m.move(jobsScreen)
	m.Update(cmd())
	if m.err != nil || m.loading {
		t.Fatal("stale response applied")
	}
}
func TestHeadlessProgramQuit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := New(ctx, "")
	defer m.Close()
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	go p.Send(key("q"))
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if !m.quitting {
		t.Fatal("program did not process quit")
	}
}
func TestLongDetailsScrollAndHelp(t *testing.T) {
	m := New(context.Background(), "")
	defer m.Close()
	m.screen = jobScreen
	m.width = 40
	m.height = 12
	m.jobDetail.Description = strings.Repeat("details\n", 30) + "last detail"
	m.Update(key("j"))
	if m.detailTop != 1 {
		t.Fatal("detail scroll")
	}
	sawLastDetail := false
	for i := 0; i < 60; i++ {
		m.Update(key("j"))
		sawLastDetail = sawLastDetail || strings.Contains(ansi.Strip(m.View().Content), "last detail")
	}
	view := ansi.Strip(m.View().Content)
	if !sawLastDetail || !strings.Contains(view, "Children:") || !strings.Contains(view, "Esc back") {
		t.Fatal("details/help inaccessible", view)
	}
}
