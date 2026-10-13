//go:build linux

package main

import (
	"context"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"aaptui/internal/config"
)

func TestSetupPromptRestoresTerminal(t *testing.T) {
	for _, exit := range []string{"complete", "escape", "ctrl-c", "ctrl-d", "signal"} {
		t.Run(exit, func(t *testing.T) {
			input := terminalInput(t)
			before, err := term.GetState(input.Fd())
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.CreateTemp(t.TempDir(), "terminal")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := file.Close(); err != nil {
					t.Error(err)
				}
			})
			output := &terminalCapture{terminalOutput: terminalOutput{file}, frames: make(chan []byte, 64)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m := newConnectionPrompt(config.Settings{URL: "https://example.org", Mode: config.Gateway, TLSVerify: true}, true)
			m.ctx = ctx
			p := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithInput(input), tea.WithOutput(output), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler())
			done := make(chan error, 1)
			go func() { _, err := p.Run(); done <- err }()
			finished := false
			defer func() {
				if !finished {
					p.Send(tea.QuitMsg{})
					<-done
				}
			}()
			waitFrame := func(text string) {
				t.Helper()
				for {
					select {
					case frame := <-output.frames:
						if strings.Contains(string(frame), text) {
							return
						}
					case err := <-done:
						finished = true
						t.Fatal("prompt exited before rendering", err)
					case <-ctx.Done():
						t.Fatal("prompt timed out")
					}
				}
			}
			waitFrame("Username")
			p.Send(tea.PasteMsg{Content: "alice"})
			p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
			waitFrame("Password")
			p.Send(tea.PasteMsg{Content: "private-password"})
			waitFrame("***")
			switch exit {
			case "complete":
				p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
			case "escape":
				p.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
			case "ctrl-c":
				p.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			case "ctrl-d":
				p.Send(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
			case "signal":
				cancel()
			}
			select {
			case err := <-done:
				finished = true
				if exit != "signal" && err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("prompt did not exit")
			}
			after, err := term.GetState(input.Fd())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("terminal state was not restored", err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil || strings.Contains(string(data), "private-password") {
				t.Fatal("password written to terminal", err)
			}
			if m.done != (exit == "complete") {
				t.Fatal("unexpected prompt outcome")
			}
		})
	}
}

func TestSetupInputEOFQuitsPrompt(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newConnectionPrompt(config.Settings{Mode: config.Gateway, TLSVerify: true}, true)
	m.ctx = ctx
	p := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithInput(setupInput{reader, cancel}), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case err := <-done:
		if err != nil || m.done || ctx.Err() == nil {
			t.Fatal("EOF did not cancel setup", err)
		}
	case <-time.After(5 * time.Second):
		p.Quit()
		<-done
		t.Fatal("prompt waited after EOF")
	}
}
