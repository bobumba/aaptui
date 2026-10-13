package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/config"
)

func TestStartupMissingCombinations(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, tc := range []struct {
			name, url, token                  string
			prompts, creates, verifies, saves int
		}{
			{"complete", "https://example.org", "supplied", 0, 0, 0, 0},
			{"missing URL", "", "supplied", 1, 0, 1, 1},
			{"missing token", "https://example.org", "", 1, 1, 0, 1},
			{"missing both", "", "", 1, 1, 0, 1},
		} {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				prompts, creates, verifies, saves := 0, 0, 0, 0
				deps := startupDependencies{
					interactive: true,
					prompt: func(_ context.Context, s config.Settings, needToken bool) (config.Settings, loginCredentials, error) {
						prompts++
						if needToken != (tc.token == "") || s.URL != tc.url {
							t.Fatal("prompt discarded existing settings")
						}
						if s.Mode != mode {
							t.Fatal("prompt changed the connection mode")
						}
						s.URL = "https://example.org"
						return s, loginCredentials{"user", "private"}, nil
					},
					createToken: func(_ context.Context, got config.Settings, user, password string) (string, error) {
						creates++
						if got.Mode != mode || user != "user" || password != "private" {
							t.Fatal("missing login credentials")
						}
						return "generated", nil
					},
					verifyToken: func(_ context.Context, _ config.Settings, token string) error {
						verifies++
						if token != "supplied" {
							t.Fatal("supplied token replaced")
						}
						return nil
					},
					saveCredentials: func(s config.Settings, token string) error {
						saves++
						if s.URL != "https://example.org" || token == "" {
							t.Fatal("incomplete credentials persisted")
						}
						return nil
					},
				}
				s, token, err := setupConnection(context.Background(), config.Settings{URL: tc.url, Mode: mode, TLSVerify: true}, tc.token, deps)
				if err != nil || s.URL != "https://example.org" || token == "" || prompts != tc.prompts || creates != tc.creates || verifies != tc.verifies || saves != tc.saves {
					t.Fatal("unexpected startup result", err, prompts, creates, verifies, saves)
				}
			})
		}
	}
}

func TestStartupCancellationFailuresAndDirectMode(t *testing.T) {
	for _, scenario := range []string{"noninteractive", "cancel", "invalid URL", "authentication", "storage", "verification"} {
		t.Run(scenario, func(t *testing.T) {
			saves := 0
			deps := startupDependencies{
				interactive: scenario != "noninteractive",
				prompt: func(_ context.Context, s config.Settings, _ bool) (config.Settings, loginCredentials, error) {
					if scenario == "noninteractive" {
						t.Fatal("unexpected prompt")
					}
					if scenario == "cancel" {
						return s, loginCredentials{}, context.Canceled
					}
					if scenario == "invalid URL" {
						s.URL = "https://example.org/password"
					}
					return s, loginCredentials{"user", "password"}, nil
				},
				createToken: func(context.Context, config.Settings, string, string) (string, error) {
					if scenario != "storage" && scenario != "authentication" {
						t.Fatal("unexpected token creation")
					}
					if scenario == "authentication" {
						return "", fmt.Errorf("authentication failed")
					}
					return "generated", nil
				},
				verifyToken: func(context.Context, config.Settings, string) error { return fmt.Errorf("authentication failed") },
				saveCredentials: func(config.Settings, string) error {
					saves++
					return fmt.Errorf("storage failed")
				},
			}
			s := config.Settings{URL: "https://example.org", Mode: config.Gateway, TLSVerify: true}
			token := ""
			if scenario == "verification" {
				s.URL, token = "", "supplied"
				deps.prompt = func(_ context.Context, s config.Settings, _ bool) (config.Settings, loginCredentials, error) {
					s.URL = "https://example.org"
					return s, loginCredentials{}, nil
				}
			}
			_, _, err := setupConnection(context.Background(), s, token, deps)
			if err == nil || strings.Contains(err.Error(), "password") || (scenario != "storage" && saves != 0) {
				t.Fatal("missing error, secret leak, or saved unsuccessful login", err)
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation was lost", err)
			}
		})
	}
	// Direct mode with complete credentials needs neither prompts nor mutations.
	if _, _, err := setupConnection(context.Background(), config.Settings{URL: "https://example.org", Mode: config.Direct}, "supplied", startupDependencies{}); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionPromptMaskingValidationAndCancellation(t *testing.T) {
	s := config.Settings{Mode: config.Gateway, TLSVerify: true}
	m := newConnectionPrompt(s, true)
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	m.Update(tea.PasteMsg{Content: "https://user:private@example.org"})
	m.Update(enter)
	if m.field != 0 || strings.Contains(m.message, "private") {
		t.Fatal("URL credentials accepted or included in error")
	}
	m.input = "https://example.org"
	m.Update(enter)
	m.Update(tea.PasteMsg{Content: "alice"})
	m.Update(enter)
	m.Update(tea.PasteMsg{Content: "private-password"})
	if strings.Contains(m.View().Content, "private-password") || !strings.Contains(m.View().Content, "***") {
		t.Fatal("password is visible")
	}
	m.Update(tea.WindowSizeMsg{Width: 12, Height: 4})
	for _, line := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(line) > 12 {
			t.Fatal("prompt exceeds terminal width")
		}
	}
	m.Update(enter)
	if !m.done || m.login.password != "private-password" || m.View().Content != "" {
		t.Fatal("prompt did not complete safely")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", m.login, m.login), "private") {
		t.Fatal("login credentials printable")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}} {
		m := newConnectionPrompt(s, true)
		m.login.password = "private"
		m.input = "private"
		_, cmd := m.Update(key)
		if cmd == nil || m.done || m.input != "" || m.login.password != "" {
			t.Fatal("cancellation did not discard credentials")
		}
	}
	m = newConnectionPrompt(config.Settings{Mode: config.Gateway}, false)
	m.input = "https://example.org"
	m.Update(enter)
	if !m.done || m.login.username != "" {
		t.Fatal("URL-only prompt requested credentials")
	}
}

func TestConnectionPromptBoundsAndEditing(t *testing.T) {
	m := newConnectionPrompt(config.Settings{URL: "https://example.org", Mode: config.Gateway}, true)
	for _, text := range []string{"bad\x1b[31m", "bad\n", strings.Repeat("x", 4097)} {
		m.Update(tea.PasteMsg{Content: text})
		if m.input != "" || m.message == "" {
			t.Fatal("invalid or oversized input accepted")
		}
	}
	m.Update(tea.PasteMsg{Content: "éq"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input != "é" {
		t.Fatal("backspace did not preserve Unicode")
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.input != "" || m.message != "" {
		t.Fatal("clear did not clear the field")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != 1 || m.message == "" {
		t.Fatal("empty username accepted")
	}
}
