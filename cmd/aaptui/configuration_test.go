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
	for _, current := range []string{"complete", "missing token", "empty", "invalid"} {
		for _, createNew := range []bool{false, true} {
			if current == "empty" && !createNew {
				continue
			}
			t.Run(fmt.Sprintf("%s/new=%t", current, createNew), func(t *testing.T) {
				startupTestHome(t)
				s := config.Settings{URL: "https://current.example", Mode: config.Gateway, TLSVerify: true}
				if err := config.SaveCredentials(s, "current-token"); err != nil {
					t.Fatal(err)
				}
				connections, err := config.LoadConnections("")
				if err != nil {
					t.Fatal(err)
				}
				if current == "missing token" {
					connections[0].Secrets = config.Secrets{}
				}
				if current == "invalid" {
					connections[0].Err = errors.New("configuration: invalid settings")
				}
				if current == "empty" {
					connections = nil
				}
				fresh := config.Settings{URL: "https://new.example", Mode: config.Direct, TLSVerify: false}
				choices, prompts, saves := 0, 0, 0
				deps := startupDependencies{
					interactive: true,
					load: func(path string) ([]config.Connection, error) {
						if path != "selected.json" {
							t.Fatal("selected path was lost")
						}
						return connections, nil
					},
					choose: func(_ context.Context, entries []config.Connection, status string) (int, error) {
						choices++
						if len(entries) != len(connections) || status == "" || strings.Contains(status, "current-token") {
							t.Fatal("incorrect or unsafe menu")
						}
						if createNew {
							return len(entries), nil
						}
						return 0, nil
					},
					promptNew: func(context.Context) (config.Settings, loginCredentials, error) {
						prompts++
						return fresh, loginCredentials{"new-user", "new-password"}, nil
					},
					prompt: func(_ context.Context, got config.Settings, needToken bool) (config.Settings, loginCredentials, error) {
						prompts++
						if got != s || !needToken {
							t.Fatal("selected settings were lost")
						}
						return got, loginCredentials{"user", "password"}, nil
					},
					createToken: func(_ context.Context, got config.Settings, user, password string) (string, error) {
						if createNew && (got != fresh || user != "new-user" || password != "new-password") {
							t.Fatal("new setup reused credentials")
						}
						return "generated-token", nil
					},
					saveCredentials: func(config.Settings, string) error { saves++; return nil },
				}
				got, token, err := startConnection(context.Background(), "selected.json", deps)
				if choices != 1 {
					t.Fatal("startup did not ask exactly once")
				}
				if current == "invalid" && !createNew {
					if !errors.Is(err, connections[0].Err) || prompts != 0 || saves != 0 {
						t.Fatal("invalid connection was used", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if createNew {
					if got != fresh || token != "generated-token" || prompts != 1 || saves != 1 {
						t.Fatal("new connection was not used")
					}
				} else if current == "complete" {
					if got != s || token != "current-token" || prompts != 0 || saves != 0 {
						t.Fatal("complete connection was changed")
					}
				} else if got != s || token != "generated-token" || prompts != 1 || saves != 1 {
					t.Fatal("missing credentials were not prompted")
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
				load: func(string) ([]config.Connection, error) {
					if scenario == "noninteractive" || scenario == "cancelled context" {
						t.Fatal("loaded settings before checking terminal/context")
					}
					return []config.Connection{{Settings: config.Settings{URL: "https://current.example", Mode: config.Gateway}}}, nil
				},
				choose: func(context.Context, []config.Connection, string) (int, error) {
					if scenario == "choice cancellation" {
						return -1, context.Canceled
					}
					if scenario == "cancel after choice" {
						cancel()
					}
					return 1, nil
				},
				promptNew: func(context.Context) (config.Settings, loginCredentials, error) {
					if scenario != "new prompt cancellation" {
						t.Fatal("prompted after cancellation")
					}
					return config.Settings{}, loginCredentials{}, context.Canceled
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
	entries := []config.Connection{
		{Settings: config.Settings{URL: "https://one.example"}},
		{Settings: config.Settings{URL: "https://two.example"}},
		{Settings: config.Settings{URL: "https://user:secret@bad.example"}, Err: errors.New("invalid")},
	}
	m := newConfigurationChoice(entries, "Choose URL")
	if strings.Contains(m.View().Content, "secret") || strings.Contains(m.View().Content, "Load current configuration") || !strings.Contains(m.View().Content, entries[1].Settings.URL) {
		t.Fatal("unsafe or missing URL list")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyTab}, {Code: tea.KeyTab, Mod: tea.ModShift}, {Code: tea.KeyUp}} {
		m.Update(key)
	}
	if m.selected != 0 {
		t.Fatal("navigation did not restore selection")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.done {
		t.Fatal("invalid entry selected")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.done || m.selected != len(entries) || m.View().Content != "" {
		t.Fatal("new setup did not complete")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}} {
		m := newConfigurationChoice(entries, "Ready")
		_, cmd := m.Update(key)
		if cmd == nil || m.done {
			t.Fatal("choice cancellation failed")
		}
	}
	m = newConfigurationChoice(nil, "No URLs")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.done || m.selected != 0 {
		t.Fatal("empty list did not offer new setup")
	}
	m = newConfigurationChoice(entries[:1], "Ready")
	if !strings.Contains(m.View().Content, entries[0].Settings.URL) {
		t.Fatal("single URL was hidden")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.done || m.selected != 0 {
		t.Fatal("URL selection did not complete")
	}
}

func TestConfigurationChoiceScrolling(t *testing.T) {
	var entries []config.Connection
	for i := 0; i < 30; i++ {
		entries = append(entries, config.Connection{Settings: config.Settings{URL: fmt.Sprintf("https://aap-%d.example", i)}})
	}
	m := newConfigurationChoice(entries, "Ready")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	for i := 0; i < 29; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if !strings.Contains(m.View().Content, "> https://aap-29.example") {
		t.Fatal("focused URL is not visible")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 12, Height: 6}, {Width: 8, Height: 2}, {Width: 1, Height: 1}} {
		m.Update(size)
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > size.Height {
			t.Fatal("menu exceeds terminal height")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.Width {
				t.Fatal("menu exceeds terminal width")
			}
		}
	}
}

func TestNewConnectionPrompt(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, tlsInput := range []string{"", "false"} {
			t.Run(string(mode)+"/TLS="+tlsInput, func(t *testing.T) {
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
				modeInput := string(mode)
				if mode == config.Gateway {
					modeInput = ""
				}
				enter(modeInput, fieldTLS)
				enter(tlsInput, fieldUsername)
				if m.settings.Mode != mode || m.settings.TLSVerify != (tlsInput == "") {
					t.Fatal("connection settings were not retained")
				}
				if !strings.Contains(m.View().Content, "Username") || strings.Contains(m.View().Content, "API token") {
					t.Fatal("new configuration did not request credentials")
				}
				enter("alice", fieldPassword)
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
				if m.login.username != "alice" || m.login.password != "private-secret" {
					t.Fatal("login entry did not collect credentials")
				}
			})
		}
	}
}

func TestNewConnectionPromptValidationAndCancellation(t *testing.T) {
	for field, input := range map[int]string{fieldURL: "https://user:private@example.org", fieldMode: "private", fieldTLS: "private", fieldUsername: "user:private", fieldPassword: ""} {
		m := newConfigurationPrompt()
		m.field, m.input = field, input
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.done || m.field != field || m.message == "" || strings.Contains(m.message, "private") {
			t.Fatal("invalid field accepted or leaked into error", field)
		}
	}
	for _, field := range []int{fieldURL, fieldMode, fieldTLS, fieldUsername, fieldPassword} {
		for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}} {
			m := newConfigurationPrompt()
			m.field, m.input, m.login = field, "private", loginCredentials{"private", "private"}
			_, cmd := m.Update(key)
			if cmd == nil || m.done || m.input != "" || m.login != (loginCredentials{}) {
				t.Fatal("cancellation retained secrets", field)
			}
		}
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
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, scenario := range []string{"successful login", "authentication failure", "cancel during authentication", "storage failure", "invalid current file", "missing selected file"} {
			t.Run(string(mode)+"/"+scenario, func(t *testing.T) {
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
				path := "/api/gateway/v1/tokens/"
				if mode == config.Direct {
					path = "/api/v2/tokens/"
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					user, password, ok := r.BasicAuth()
					if r.Method != http.MethodPost || r.URL.Path != path || !ok || user != "alice" || password != "login-secret" {
						t.Error("unexpected login request or reused old credentials")
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
					w.WriteHeader(http.StatusCreated)
					if _, err := fmt.Fprint(w, `{"token":"new-secret"}`); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				fresh := config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}
				deps := terminalStartupDependencies()
				deps.interactive = true
				deps.choose = func(_ context.Context, entries []config.Connection, status string) (int, error) {
					if (scenario == "invalid current file" || scenario == "missing selected file") && len(entries) != 0 {
						t.Fatal("invalid current file was available")
					}
					for _, secret := range []string{"old-secret", "environment-secret"} {
						if strings.Contains(status, secret) {
							t.Fatal("choice status exposed credentials")
						}
					}
					return len(entries), nil
				}
				deps.promptNew = func(context.Context) (config.Settings, loginCredentials, error) {
					return fresh, loginCredentials{"alice", "login-secret"}, nil
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
					if err != nil || saved != fresh || secrets.Token() != "new-secret" || strings.Contains(string(after), "login-secret") || strings.Contains(string(after), "alice") {
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
	deps.choose = func(context.Context, []config.Connection, string) (int, error) { return 0, nil }
	deps.promptNew = func(context.Context) (config.Settings, loginCredentials, error) {
		t.Fatal("loading current configuration prompted for a new one")
		return config.Settings{}, loginCredentials{}, nil
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

func TestStartupSelectsEachSavedURLWithOverrides(t *testing.T) {
	startupTestHome(t)
	one := config.Settings{URL: "https://one.example", Mode: config.Direct, TLSVerify: false}
	two := config.Settings{URL: "https://two.example", Mode: config.Gateway, TLSVerify: true}
	for _, item := range []struct {
		settings config.Settings
		token    string
	}{{one, "one-token"}, {two, "two-token"}} {
		if err := config.SaveCredentials(item.settings, item.token); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AAP_URL", "https://env.example")
	t.Setenv("AAP_TOKEN", "env-token")
	t.Setenv("AAP_CONNECTION_MODE", "gateway")
	t.Setenv("AAP_TLS_VERIFY", "true")
	for _, item := range []struct {
		settings config.Settings
		token    string
	}{{one, "one-token"}, {two, "two-token"}, {config.Settings{URL: "https://env.example", Mode: config.Gateway, TLSVerify: true}, "env-token"}} {
		deps := terminalStartupDependencies()
		deps.interactive = true
		deps.choose = func(_ context.Context, entries []config.Connection, _ string) (int, error) {
			if len(entries) != 3 {
				t.Fatal("saved or environment URL is missing")
			}
			for i, c := range entries {
				if c.Settings.URL == item.settings.URL {
					return i, nil
				}
			}
			t.Fatal("selected URL is missing")
			return -1, nil
		}
		deps.prompt = func(context.Context, config.Settings, bool) (config.Settings, loginCredentials, error) {
			t.Fatal("complete saved connection requested authentication")
			return config.Settings{}, loginCredentials{}, nil
		}
		deps.createToken = func(context.Context, config.Settings, string, string) (string, error) {
			t.Fatal("saved connection created a token")
			return "", nil
		}
		deps.saveCredentials = func(config.Settings, string) error {
			t.Fatal("selection rewrote credentials")
			return nil
		}
		s, token, err := startConnection(context.Background(), "", deps)
		if err != nil || s != item.settings || token != item.token {
			t.Fatal("selected URL used another connection's settings or token", err)
		}
	}
}
