package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

type loginCredentials struct{ username, password string }

func (loginCredentials) String() string   { return "[redacted]" }
func (loginCredentials) GoString() string { return "[redacted]" }

type startupDependencies struct {
	interactive     bool
	load            func(string) (config.Settings, string, error)
	choose          func(context.Context, bool, string) (bool, error)
	promptNew       func(context.Context) (config.Settings, string, loginCredentials, error)
	prompt          func(context.Context, config.Settings, bool) (config.Settings, loginCredentials, error)
	createToken     func(context.Context, config.Settings, string, string) (string, error)
	verifyToken     func(context.Context, config.Settings, string) error
	saveCredentials func(config.Settings, string) error
}

// startConnection always asks before connecting. New setup intentionally ignores
// selected-file and environment values; they apply only to the current choice.
func startConnection(ctx context.Context, path string, deps startupDependencies) (config.Settings, string, error) {
	if err := ctx.Err(); err != nil {
		return config.Settings{}, "", err
	}
	if !deps.interactive {
		return config.Settings{}, "", fmt.Errorf("startup configuration choice requires an interactive terminal")
	}
	s, token, loadErr := deps.load(path)
	canLoad, status := true, "Current configuration is ready."
	switch {
	case loadErr != nil:
		canLoad, status = false, "Current configuration is invalid or cannot be read. Create a new configuration or correct the settings."
	case s.URL == "" && token == "":
		canLoad, status = false, "No current connection is available. Create a new configuration."
	case s.URL == "" || token == "":
		status = "Current configuration is incomplete; missing details will be prompted."
	}
	createNew, err := deps.choose(ctx, canLoad, status)
	if err != nil {
		return config.Settings{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return config.Settings{}, "", err
	}
	if !createNew {
		if loadErr != nil {
			return config.Settings{}, "", loadErr
		}
		return setupConnection(ctx, s, token, deps)
	}
	s, token, login, err := deps.promptNew(ctx)
	if err != nil {
		return config.Settings{}, "", err
	}
	return authenticateConnection(ctx, s, token, login, deps)
}

func setupConnection(ctx context.Context, s config.Settings, token string, deps startupDependencies) (config.Settings, string, error) {
	if err := ctx.Err(); err != nil {
		return config.Settings{}, "", err
	}
	if s.URL != "" && token != "" {
		return s, token, nil
	}
	if !deps.interactive {
		return config.Settings{}, "", fmt.Errorf("connection setup requires an interactive terminal; supply AAP_URL and AAP_TOKEN or saved credentials")
	}
	if token == "" && s.Mode == config.Direct {
		return config.Settings{}, "", fmt.Errorf("direct-controller token creation is unsupported; use gateway mode or supply AAP_TOKEN")
	}
	s, login, err := deps.prompt(ctx, s, token == "")
	if err != nil {
		return config.Settings{}, "", err
	}
	return authenticateConnection(ctx, s, token, login, deps)
}

func authenticateConnection(ctx context.Context, s config.Settings, token string, login loginCredentials, deps startupDependencies) (config.Settings, string, error) {
	if err := config.Validate(s); err != nil {
		return config.Settings{}, "", fmt.Errorf("connection setup: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return config.Settings{}, "", err
	}
	generated := token == ""
	if generated {
		if s.Mode == config.Direct {
			return config.Settings{}, "", fmt.Errorf("direct-controller token creation is unsupported; use gateway mode or supply an existing token")
		}
		var err error
		token, err = deps.createToken(ctx, s, login.username, login.password)
		login = loginCredentials{}
		if err != nil {
			return config.Settings{}, "", err
		}
	} else {
		if err := config.ValidateToken(token); err != nil {
			return config.Settings{}, "", fmt.Errorf("connection setup: %w", err)
		}
		if err := deps.verifyToken(ctx, s, token); err != nil {
			return config.Settings{}, "", fmt.Errorf("verify supplied token: %w", err)
		}
	}
	if err := config.ValidateToken(token); err != nil {
		return config.Settings{}, "", fmt.Errorf("connection setup: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return config.Settings{}, "", err
	}
	if err := deps.saveCredentials(s, token); err != nil {
		if generated {
			return config.Settings{}, "", fmt.Errorf("%w; token may exist on the gateway; check gateway tokens before creating another", err)
		}
		return config.Settings{}, "", err
	}
	return s, token, nil
}

func terminalStartupDependencies() startupDependencies {
	return startupDependencies{
		interactive: term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()),
		load: func(path string) (config.Settings, string, error) {
			s, secrets, err := config.LoadForSetup(path)
			return s, secrets.Token(), err
		},
		choose:    promptConfigurationChoice,
		promptNew: promptNewConnection,
		prompt:    promptConnection,
		createToken: func(ctx context.Context, s config.Settings, username, password string) (string, error) {
			return aap.CreateToken(ctx, s, username, password, nil)
		},
		verifyToken: func(ctx context.Context, s config.Settings, token string) error {
			client, err := aap.NewClient(s, token, nil)
			if err != nil {
				return err
			}
			_, err = client.Me(ctx)
			return err
		},
		saveCredentials: config.SaveCredentials,
	}
}

type connectionPrompt struct {
	ctx       context.Context
	settings  config.Settings
	login     loginCredentials
	needToken bool
	fullSetup bool
	token     string
	field     int
	input     string
	message   string
	width     int
	done      bool
}

const (
	fieldURL = iota
	fieldUsername
	fieldPassword
	fieldMode
	fieldTLS
	fieldAuthentication
	fieldToken
)

func newConfigurationPrompt() *connectionPrompt {
	m := newConnectionPrompt(config.Settings{Mode: config.Gateway, TLSVerify: true}, true)
	m.fullSetup = true
	return m
}

func newConnectionPrompt(s config.Settings, needToken bool) *connectionPrompt {
	field := fieldURL
	if s.URL != "" {
		field = fieldUsername
	}
	return &connectionPrompt{settings: s, needToken: needToken, field: field, width: 80}
}

func (m *connectionPrompt) Init() tea.Cmd {
	if m.ctx == nil {
		return nil
	}
	// Quit gracefully so Bubble Tea waits for its input reader before closing
	// it. Cancelling the program's own context kills that reader asynchronously.
	return func() tea.Msg {
		<-m.ctx.Done()
		return tea.QuitMsg{}
	}
}

func (m *connectionPrompt) insert(text string) {
	limit := 4096
	if m.field == fieldToken {
		limit = 8192
	}
	if len(m.input)+len(text) > limit {
		m.message = fmt.Sprintf("Input exceeds %d bytes.", limit)
		return
	}
	if strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		m.message = "Use a single line without control characters."
		return
	}
	m.input += text
	m.message = ""
}

func (m *connectionPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
	case tea.PasteMsg:
		m.insert(msg.Content)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d", "esc":
			m.input, m.token, m.login = "", "", loginCredentials{}
			return m, tea.Quit
		case "backspace":
			r := []rune(m.input)
			if len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
			m.message = ""
		case "ctrl+u":
			m.input = ""
			m.message = ""
		case "enter":
			switch m.field {
			case fieldURL:
				s := m.settings
				s.URL = strings.TrimSpace(m.input)
				if err := config.Validate(s); err != nil {
					m.message = err.Error()
					return m, nil
				}
				m.settings = s
				if m.fullSetup {
					m.field = fieldMode
					break
				}
				if !m.needToken {
					m.input, m.done = "", true
					return m, tea.Quit
				}
				m.field = fieldUsername
			case fieldUsername:
				username := strings.TrimSpace(m.input)
				if username == "" || strings.Contains(username, ":") {
					m.message = "Enter a username without a colon."
					return m, nil
				}
				m.login.username = username
				m.field = fieldPassword
			case fieldPassword:
				if m.input == "" {
					m.message = "Enter a password."
					return m, nil
				}
				m.login.password = m.input
				m.input, m.done = "", true
				return m, tea.Quit
			case fieldMode:
				mode := config.ConnectionMode(strings.ToLower(strings.TrimSpace(m.input)))
				if mode == "" {
					mode = config.Gateway
				}
				if mode != config.Gateway && mode != config.Direct {
					m.message = "Connection mode must be gateway or direct."
					return m, nil
				}
				m.settings.Mode, m.field = mode, fieldTLS
			case fieldTLS:
				verify := true
				if m.input != "" {
					var err error
					verify, err = strconv.ParseBool(strings.TrimSpace(m.input))
					if err != nil {
						m.message = "TLS verification must be true or false."
						return m, nil
					}
				}
				m.settings.TLSVerify, m.field = verify, fieldAuthentication
				if m.settings.Mode == config.Direct {
					m.field = fieldToken
				}
			case fieldAuthentication:
				switch strings.ToLower(strings.TrimSpace(m.input)) {
				case "", "login":
					m.field = fieldUsername
				case "token":
					m.field = fieldToken
				default:
					m.message = "Authentication must be login or token."
					return m, nil
				}
			case fieldToken:
				if err := config.ValidateToken(m.input); err != nil {
					m.message = err.Error()
					return m, nil
				}
				m.token, m.input, m.done = m.input, "", true
				return m, tea.Quit
			}
			m.input, m.message = "", ""
		default:
			if msg.Text != "" {
				m.insert(msg.Text)
			}
		}
	}
	return m, nil
}

func (m *connectionPrompt) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	label := []string{"Host URL", "Username", "Password", "Connection mode (gateway/direct) [gateway]", "TLS verification (true/false) [true]", "Authentication (login/token) [login]", "API token"}[m.field]
	value := m.input
	if m.field == fieldPassword || m.field == fieldToken {
		value = strings.Repeat("*", len([]rune(value)))
	}
	connection := fmt.Sprintf("Mode: %s · TLS verification: %t", m.settings.Mode, m.settings.TLSVerify)
	if strings.HasPrefix(m.settings.URL, "http://") {
		connection = fmt.Sprintf("Mode: %s · HTTP · TLS verification: not applicable", m.settings.Mode)
	}
	title := "AAP connection setup"
	if m.fullSetup {
		title = "New AAP connection · saved after successful authentication"
	}
	lines := []string{title, connection}
	if m.field == fieldToken && m.settings.Mode == config.Direct {
		lines = append(lines, "Direct mode requires an existing token; token creation requires gateway mode.")
	}
	lines = append(lines, label+": "+value, m.message, "Enter: continue · Backspace: erase · Ctrl-U: clear · Esc/Ctrl-C/Ctrl-D: cancel")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "")
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func promptConnection(ctx context.Context, s config.Settings, needToken bool) (config.Settings, loginCredentials, error) {
	s, _, login, err := runConnectionPrompt(ctx, newConnectionPrompt(s, needToken))
	return s, login, err
}

func promptNewConnection(ctx context.Context) (config.Settings, string, loginCredentials, error) {
	return runConnectionPrompt(ctx, newConfigurationPrompt())
}

func runConnectionPrompt(ctx context.Context, m *connectionPrompt) (config.Settings, string, loginCredentials, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.ctx = ctx
	defer func() { m.input, m.token, m.login = "", "", loginCredentials{} }()
	input := setupInput{File: os.Stdin, eof: cancel}
	_, err := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithInput(input), tea.WithOutput(terminalOutput{os.Stdout}), tea.WithoutSignalHandler()).Run()
	if ctx.Err() != nil {
		return config.Settings{}, "", loginCredentials{}, ctx.Err()
	}
	if err != nil {
		return config.Settings{}, "", loginCredentials{}, fmt.Errorf("connection setup terminal failed")
	}
	if !m.done {
		return config.Settings{}, "", loginCredentials{}, context.Canceled
	}
	return m.settings, m.token, m.login, nil
}

// Bubble Tea treats EOF as the end of input scanning without quitting the
// model. Turn input loss into cancellation while preserving the terminal FD.
type setupInput struct {
	*os.File
	eof context.CancelFunc
}

func (r setupInput) Read(p []byte) (int, error) {
	n, err := r.File.Read(p)
	if errors.Is(err, io.EOF) {
		r.eof()
	}
	return n, err
}
