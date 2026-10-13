package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/config"
)

func TestStartupConfigurationChoices(t *testing.T) {
	for _, current := range []string{"complete", "missing URL", "missing token", "missing both", "invalid"} {
		for _, createNew := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/new=%t", current, createNew), func(t *testing.T) {
				s := config.Settings{URL: "https://current.example", Mode: config.Gateway, TLSVerify: true}
				token := "current-token"
				var loadErr error
				switch current {
				case "missing URL":
					s.URL = ""
				case "missing token":
					token = ""
				case "missing both":
					s.URL, token = "", ""
				case "invalid":
					loadErr = errors.New("configuration: invalid settings")
				}
				newSettings := config.Settings{URL: "https://new.example", Mode: config.Direct, TLSVerify: false}
				choices, newPrompts, currentPrompts, saves := 0, 0, 0, 0
				deps := startupDependencies{
					interactive: true,
					load: func(path string) (config.Settings, string, error) {
						if path != "selected.json" {
							t.Fatal("selected path was lost")
						}
						return s, token, loadErr
					},
					choose: func(_ context.Context, canLoad bool, status string) (bool, error) {
						choices++
						if canLoad != (current != "invalid" && current != "missing both") || status == "" || strings.Contains(status, token) && token != "" {
							t.Fatal("incorrect choice availability or unsafe status")
						}
						return createNew, nil
					},
					promptNew: func(context.Context) (config.Settings, string, loginCredentials, error) {
						newPrompts++
						return newSettings, "new-token", loginCredentials{}, nil
					},
					prompt: func(_ context.Context, got config.Settings, needToken bool) (config.Settings, loginCredentials, error) {
						currentPrompts++
						if got != s || needToken != (token == "") {
							t.Fatal("current settings were not retained")
						}
						got.URL = "https://current.example"
						return got, loginCredentials{"user", "password"}, nil
					},
					createToken: func(context.Context, config.Settings, string, string) (string, error) { return "generated-token", nil },
					verifyToken: func(_ context.Context, got config.Settings, gotToken string) error {
						if createNew && (got != newSettings || gotToken != "new-token") {
							t.Fatal("new setup reused current credentials")
						}
						return nil
					},
					saveCredentials: func(config.Settings, string) error { saves++; return nil },
				}
				got, gotToken, err := startConnection(context.Background(), "selected.json", deps)
				if choices != 1 {
					t.Fatal("startup did not ask exactly once")
				}
				if current == "invalid" && !createNew {
					if !errors.Is(err, loadErr) || newPrompts != 0 || currentPrompts != 0 || saves != 0 {
						t.Fatal("invalid current configuration was used", err)
					}
					return
				}
				if err != nil || gotToken == "" {
					t.Fatal("startup failed", err)
				}
				if createNew {
					if got != newSettings || gotToken != "new-token" || newPrompts != 1 || currentPrompts != 0 || saves != 1 {
						t.Fatal("new configuration was not used")
					}
				} else if current == "complete" {
					if got != s || gotToken != token || newPrompts != 0 || currentPrompts != 0 || saves != 0 {
						t.Fatal("complete current configuration was changed")
					}
				} else if newPrompts != 0 || currentPrompts != 1 || saves != 1 {
					t.Fatal("missing current settings were not prompted")
				}
			})
		}
	}
}

func TestStartupChoiceCancellationAndNoninteractive(t *testing.T) {
	for _, scenario := range []string{"noninteractive", "cancelled context", "choice cancellation", "cancel after choice", "new prompt cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled context" {
				cancel()
			}
			deps := startupDependencies{
				interactive: scenario != "noninteractive",
				load: func(string) (config.Settings, string, error) {
					if scenario == "noninteractive" || scenario == "cancelled context" {
						t.Fatal("loaded settings before checking terminal/context")
					}
					return config.Settings{URL: "https://current.example", Mode: config.Gateway}, "current-token", nil
				},
				choose: func(context.Context, bool, string) (bool, error) {
					if scenario == "choice cancellation" {
						return false, context.Canceled
					}
					if scenario == "cancel after choice" {
						cancel()
					}
					return true, nil
				},
				promptNew: func(context.Context) (config.Settings, string, loginCredentials, error) {
					if scenario != "new prompt cancellation" {
						t.Fatal("prompted after cancellation")
					}
					return config.Settings{}, "", loginCredentials{}, context.Canceled
				},
			}
			_, _, err := startConnection(ctx, "", deps)
			if scenario == "noninteractive" {
				if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
					t.Fatal("noninteractive startup did not fail clearly", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation was lost", err)
			}
		})
	}
}

func TestConfigurationChoiceNavigation(t *testing.T) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	for _, canLoad := range []bool{true, false} {
		m := newConfigurationChoice(canLoad, "Connection status")
		if (m.selected == 0) != canLoad {
			t.Fatal("unexpected default choice")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		_, cmd := m.Update(enter)
		if m.done != canLoad || (cmd != nil) != canLoad {
			t.Fatal("unavailable current configuration can be selected")
		}
		m = newConfigurationChoice(canLoad, "Connection status")
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m.Update(enter)
		if !m.done || m.selected != 1 || m.View().Content != "" {
			t.Fatal("new choice was not selected")
		}
	}
	m := newConfigurationChoice(true, "Connection status")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.selected != 1 {
		t.Fatal("Tab did not change selection")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.selected != 0 {
		t.Fatal("Shift-Tab did not return to current configuration")
	}
	m.Update(tea.WindowSizeMsg{Width: 12})
	for _, line := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(line) > 12 {
			t.Fatal("choice exceeds terminal width")
		}
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}} {
		m := newConfigurationChoice(true, "Ready")
		_, cmd := m.Update(key)
		if cmd == nil || m.done {
			t.Fatal("choice cancellation failed")
		}
	}
}

func TestNewConnectionPrompt(t *testing.T) {
	for _, scenario := range []string{"gateway login", "gateway token", "direct token"} {
		t.Run(scenario, func(t *testing.T) {
			m := newConfigurationPrompt()
			enter := func(text string, next int) {
				t.Helper()
				m.Update(tea.PasteMsg{Content: text})
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.field != next || m.message != "" {
					t.Fatal("unexpected prompt field", m.field, m.message)
				}
			}
			enter("https://new.example", fieldMode)
			mode := ""
			if scenario == "direct token" {
				mode = "direct"
			}
			enter(mode, fieldTLS)
			if scenario == "direct token" {
				enter("false", fieldToken)
				if m.settings.Mode != config.Direct || m.settings.TLSVerify || !strings.Contains(m.View().Content, "existing token") {
					t.Fatal("direct settings or explanation missing")
				}
			} else {
				enter("", fieldAuthentication)
				if m.settings.Mode != config.Gateway || !m.settings.TLSVerify {
					t.Fatal("gateway defaults changed")
				}
				if scenario == "gateway login" {
					enter("", fieldUsername)
					enter("alice", fieldPassword)
				} else {
					enter("token", fieldToken)
				}
			}
			secretField := m.field
			m.Update(tea.PasteMsg{Content: "private-secret"})
			if strings.Contains(m.View().Content, "private-secret") || !strings.Contains(m.View().Content, "***") {
				t.Fatal("secret entry was not masked")
			}
			m.Update(tea.WindowSizeMsg{Width: 12})
			for _, line := range strings.Split(m.View().Content, "\n") {
				if ansi.StringWidth(line) > 12 {
					t.Fatal("new connection prompt exceeds terminal width")
				}
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !m.done || m.input != "" || m.View().Content != "" {
				t.Fatal("new connection prompt did not finish")
			}
			if secretField == fieldToken && (m.token != "private-secret" || m.login != (loginCredentials{})) {
				t.Fatal("token entry requested login")
			}
			if secretField == fieldPassword && (m.login.password != "private-secret" || m.token != "") {
				t.Fatal("login entry did not collect credentials")
			}
		})
	}
}

func TestNewConnectionPromptValidationAndCancellation(t *testing.T) {
	for field, input := range map[int]string{fieldURL: "https://user:private@example.org", fieldMode: "private", fieldTLS: "private", fieldAuthentication: "private", fieldToken: "private token", fieldUsername: "user:private", fieldPassword: ""} {
		m := newConfigurationPrompt()
		m.field, m.input = field, input
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.done || m.field != field || m.message == "" || strings.Contains(m.message, "private") {
			t.Fatal("invalid field accepted or leaked into error", field)
		}
	}
	for _, field := range []int{fieldURL, fieldMode, fieldTLS, fieldAuthentication, fieldUsername, fieldPassword, fieldToken} {
		for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}} {
			m := newConfigurationPrompt()
			m.field, m.input, m.token, m.login = field, "private", "private", loginCredentials{"private", "private"}
			_, cmd := m.Update(key)
			if cmd == nil || m.done || m.input != "" || m.token != "" || m.login != (loginCredentials{}) {
				t.Fatal("cancellation retained secrets", field)
			}
		}
	}
	m := newConfigurationPrompt()
	m.field = fieldToken
	m.Update(tea.PasteMsg{Content: strings.Repeat("x", 8193)})
	if m.input != "" || m.message == "" {
		t.Fatal("oversized token accepted")
	}
	m.Update(tea.PasteMsg{Content: strings.Repeat("x", 8192)})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.done || len(m.token) != 8192 {
		t.Fatal("maximum valid token rejected")
	}
}

func startupTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, key := range []string{"AAP_URL", "AAP_TOKEN", "AAP_CONNECTION_MODE", "AAP_TLS_VERIFY"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestNewConfigurationAuthenticationAndReplacement(t *testing.T) {
	for _, scenario := range []string{"gateway login", "gateway token", "direct token", "authentication failure", "cancel during authentication", "storage failure", "invalid current file", "missing selected file"} {
		t.Run(scenario, func(t *testing.T) {
			home := startupTestHome(t)
			old := config.Settings{URL: "https://old.example", Mode: config.Gateway, TLSVerify: true}
			if err := config.SaveCredentials(old, "old-secret"); err != nil {
				t.Fatal(err)
			}
			privatePath := filepath.Join(home, ".config", "aaptui", "config.json")
			selectedPath := filepath.Join(t.TempDir(), "settings.json")
			selected := []byte(`{"url":"https://selected.example","connection_mode":"direct","tls_verify":false}`)
			if scenario != "missing selected file" {
				if err := os.WriteFile(selectedPath, selected, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "invalid current file" {
				if err := os.WriteFile(privatePath, []byte(`{"unknown":"old-secret"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(privatePath)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("AAP_URL", "https://environment.example")
			t.Setenv("AAP_TOKEN", "environment-secret")
			t.Setenv("AAP_CONNECTION_MODE", "direct")
			t.Setenv("AAP_TLS_VERIFY", "false")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if scenario == "gateway login" {
					user, password, ok := r.BasicAuth()
					if r.Method != http.MethodPost || r.URL.Path != "/api/gateway/v1/tokens/" || !ok || user != "alice" || password != "login-secret" {
						t.Error("unexpected gateway login request")
					}
					w.WriteHeader(http.StatusCreated)
					if _, err := fmt.Fprint(w, `{"token":"new-secret"}`); err != nil {
						t.Error(err)
					}
					return
				}
				prefix := "/api/controller/v2/"
				if scenario == "direct token" {
					prefix = "/api/v2/"
				}
				if r.Method != http.MethodGet || r.URL.Path != prefix+"me/" || r.Header.Get("Authorization") != "Bearer new-secret" {
					t.Error("unexpected token verification or reused old credentials")
				}
				if scenario == "cancel during authentication" {
					cancel()
				}
				if scenario == "authentication failure" {
					w.WriteHeader(http.StatusUnauthorized)
					if _, err := fmt.Fprint(w, "new-secret login-secret old-secret"); err != nil {
						t.Error(err)
					}
					return
				}
				if _, err := fmt.Fprint(w, `{"count":1,"results":[{"id":1,"username":"alice"}]}`); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			fresh := config.Settings{URL: server.URL, Mode: config.Gateway, TLSVerify: true}
			if scenario == "direct token" {
				fresh.Mode, fresh.TLSVerify = config.Direct, false
			}
			deps := terminalStartupDependencies()
			deps.interactive = true
			deps.choose = func(_ context.Context, canLoad bool, status string) (bool, error) {
				if (scenario == "invalid current file" || scenario == "missing selected file") && canLoad {
					t.Fatal("invalid current file was available")
				}
				for _, secret := range []string{"old-secret", "environment-secret"} {
					if strings.Contains(status, secret) {
						t.Fatal("choice status exposed credentials")
					}
				}
				return true, nil
			}
			deps.promptNew = func(context.Context) (config.Settings, string, loginCredentials, error) {
				if scenario == "gateway login" {
					return fresh, "", loginCredentials{"alice", "login-secret"}, nil
				}
				return fresh, "new-secret", loginCredentials{}, nil
			}
			if scenario == "storage failure" {
				deps.saveCredentials = func(config.Settings, string) error { return errors.New("save credentials: storage failure") }
			}
			got, token, err := startConnection(ctx, selectedPath, deps)
			failed := scenario == "authentication failure" || scenario == "cancel during authentication" || scenario == "storage failure" || scenario == "invalid current file"
			if failed {
				if err == nil || token != "" {
					t.Fatal("failed startup returned a usable connection")
				}
				for _, secret := range []string{"old-secret", "new-secret", "login-secret", "environment-secret"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatal("startup error exposed credentials")
					}
				}
			} else if err != nil || got != fresh || token != "new-secret" {
				t.Fatal("new connection was overridden or failed", err)
			}
			if requests != 1 {
				t.Fatal("authentication was skipped or retried", requests)
			}
			after, readErr := os.ReadFile(privatePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failed {
				if string(before) != string(after) {
					t.Fatal("failed setup replaced existing credentials")
				}
			} else {
				for _, key := range []string{"AAP_URL", "AAP_TOKEN", "AAP_CONNECTION_MODE", "AAP_TLS_VERIFY"} {
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
				saved, secrets, err := config.Load("")
				if err != nil || saved != fresh || secrets.Token() != "new-secret" || strings.Contains(string(after), "login-secret") {
					t.Fatal("new connection did not round trip safely", err)
				}
				for path, mode := range map[string]os.FileMode{privatePath: 0600, filepath.Dir(privatePath): 0700} {
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm() != mode {
						t.Fatal("new connection storage is not private", err)
					}
				}
			}
			if scenario == "missing selected file" {
				if _, err := os.Stat(selectedPath); !os.IsNotExist(err) {
					t.Fatal("new setup wrote to the selected settings path", err)
				}
			} else if data, err := os.ReadFile(selectedPath); err != nil || string(data) != string(selected) {
				t.Fatal("selected settings were changed", err)
			}
		})
	}
}

func TestCurrentConfigurationPrecedence(t *testing.T) {
	startupTestHome(t)
	if err := config.SaveCredentials(config.Settings{URL: "https://saved.example", Mode: config.Gateway, TLSVerify: true}, "saved-secret"); err != nil {
		t.Fatal(err)
	}
	selectedPath := filepath.Join(t.TempDir(), "selected.json")
	if err := os.WriteFile(selectedPath, []byte(`{"url":"https://selected.example","connection_mode":"direct","tls_verify":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AAP_URL", "https://environment.example")
	t.Setenv("AAP_TOKEN", "environment-secret")
	deps := terminalStartupDependencies()
	deps.interactive = true
	deps.choose = func(context.Context, bool, string) (bool, error) { return false, nil }
	deps.promptNew = func(context.Context) (config.Settings, string, loginCredentials, error) {
		t.Fatal("loading current configuration prompted for a new one")
		return config.Settings{}, "", loginCredentials{}, nil
	}
	deps.saveCredentials = func(config.Settings, string) error {
		t.Fatal("current complete configuration was rewritten")
		return nil
	}
	s, token, err := startConnection(context.Background(), selectedPath, deps)
	if err != nil || s.URL != "https://environment.example" || s.Mode != config.Direct || s.TLSVerify || token != "environment-secret" {
		t.Fatal("current configuration precedence changed", err)
	}
}
