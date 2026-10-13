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
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/charmbracelet/x/vt"

	"aaptui/internal/config"
)

func TestSetupPromptRestoresTerminal(t *testing.T) {
	for _, prompt := range []string{"missing settings", "new login", "new direct login", "configuration choice", "saved URL selection"} {
		for _, exit := range []string{"complete", "escape", "ctrl-c", "ctrl-d", "signal"} {
			t.Run(prompt+"/"+exit, func(t *testing.T) {
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
				emulator := vt.NewEmulator(80, 24)
				t.Cleanup(func() {
					if err := emulator.Close(); err != nil {
						t.Error(err)
					}
				})
				// Keys arrive through Program.Send; suppress queries that otherwise
				// wait for a reply through the emulator's input pipe.
				emulator.RegisterCsiHandler(ansi.Command('?', '$', 'p'), func(ansi.Params) bool { return true })
				emulator.RegisterCsiHandler(ansi.Command('?', 0, 'u'), func(ansi.Params) bool { return true })
				emulator.RegisterOscHandler(11, func([]byte) bool { return true })
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				connection := newConnectionPrompt(config.Settings{URL: "https://example.org", Mode: config.Gateway, TLSVerify: true}, true)
				if prompt == "new login" || prompt == "new direct login" {
					connection = newConfigurationPrompt()
				}
				connection.ctx = ctx
				choice := newConfigurationChoice([]config.Connection{{Settings: config.Settings{URL: "https://example.org"}}}, "Select URL.")
				choice.ctx = ctx
				var m tea.Model = connection
				if prompt == "configuration choice" || prompt == "saved URL selection" {
					m = choice
				}
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
							if _, err := emulator.Write(frame); err != nil {
								t.Fatal(err)
							}
							if strings.Contains(emulator.String(), text) {
								return
							}
						case err := <-done:
							finished = true
							t.Fatal("prompt exited before rendering", err)
						case <-ctx.Done():
							t.Fatalf("prompt timed out waiting for %q", text)
						}
					}
				}
				if prompt == "configuration choice" || prompt == "saved URL selection" {
					waitFrame("https://example.org")
					if prompt == "configuration choice" {
						p.Send(tea.KeyPressMsg{Code: tea.KeyDown})
						waitFrame("> Create new configuration")
					}
				} else {
					if prompt == "new login" || prompt == "new direct login" {
						waitFrame("Host URL")
						p.Send(tea.PasteMsg{Content: "https://new.example"})
						p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
						waitFrame("Connection mode")
						if prompt == "new direct login" {
							p.Send(tea.PasteMsg{Content: "direct"})
						}
						p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
						waitFrame("TLS verification (true/false)")
						p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
					}
					waitFrame("Username")
					p.Send(tea.PasteMsg{Content: "alice"})
					p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
					waitFrame("Password")
					p.Send(tea.PasteMsg{Content: "private-secret"})
					waitFrame("***")
				}
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
				if err != nil || strings.Contains(string(data), "private-secret") {
					t.Fatal("secret written to terminal", err)
				}
				completed := connection.done
				if prompt == "configuration choice" || prompt == "saved URL selection" {
					completed = choice.done
				}
				if completed != (exit == "complete") {
					t.Fatal("unexpected prompt outcome")
				}
			})
		}
	}
}

func TestSetupInputEOFQuitsPrompt(t *testing.T) {
	for _, prompt := range []string{"missing settings", "new configuration", "configuration choice"} {
		t.Run(prompt, func(t *testing.T) {
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
			connection := newConnectionPrompt(config.Settings{Mode: config.Gateway, TLSVerify: true}, true)
			if prompt == "new configuration" {
				connection = newConfigurationPrompt()
			}
			connection.ctx = ctx
			choice := newConfigurationChoice([]config.Connection{{Settings: config.Settings{URL: "https://example.org"}}}, "Select URL.")
			choice.ctx = ctx
			var m tea.Model = connection
			if prompt == "configuration choice" || prompt == "saved URL selection" {
				m = choice
			}
			p := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithInput(setupInput{reader, cancel}), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler())
			done := make(chan error, 1)
			go func() { _, err := p.Run(); done <- err }()
			select {
			case err := <-done:
				if err != nil || connection.done || choice.done || ctx.Err() == nil {
					t.Fatal("EOF did not cancel setup", err)
				}
			case <-time.After(5 * time.Second):
				p.Quit()
				<-done
				t.Fatal("prompt waited after EOF")
			}
		})
	}
}
