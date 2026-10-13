// Package config loads connection settings and private, origin-bound credentials.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type ConnectionMode string

const (
	Gateway ConnectionMode = "gateway"
	Direct  ConnectionMode = "direct"
)

type Settings struct {
	URL       string
	Mode      ConnectionMode
	TLSVerify bool
}

// Secrets deliberately has no exported fields or printable token representation.
type Secrets struct{ token string }

func (s Secrets) Token() string    { return s.token }
func (s Secrets) String() string   { return "[redacted]" }
func (s Secrets) GoString() string { return "[redacted]" }

// Connection keeps a saved URL's settings and private credentials together.
// Err marks a connection unavailable without exposing invalid input.
type Connection struct {
	Settings Settings
	Secrets  Secrets
	Err      error
}

type connectionSettings struct {
	URL       *string         `json:"url,omitempty"`
	Mode      *ConnectionMode `json:"connection_mode,omitempty"`
	TLSVerify *bool           `json:"tls_verify,omitempty"`
	Token     *string         `json:"token,omitempty"`
}

type fileSettings struct {
	connectionSettings
	Connections []connectionSettings `json:"connections,omitempty"`
}

func Validate(s Settings) error {
	if strings.ContainsFunc(s.URL, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		return fmt.Errorf("server URL must not contain terminal control characters")
	}
	u, err := url.Parse(s.URL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || strings.Contains(s.URL, "#") {
		return fmt.Errorf("server URL must be an HTTP or HTTPS origin without credentials, query, fragment, or path")
	}
	if s.Mode != Gateway && s.Mode != Direct {
		return fmt.Errorf("connection mode must be gateway or direct")
	}
	return nil
}

func Load(path string) (Settings, Secrets, error) {
	return load(path, os.LookupEnv, os.UserHomeDir, os.Open)
}
func load(path string, env func(string) (string, bool), home func() (string, error), open func(string) (*os.File, error)) (Settings, Secrets, error) {
	s, secrets, err := loadForSetup(path, env, home, open)
	if err != nil {
		return Settings{}, Secrets{}, err
	}
	if err := Validate(s); err != nil {
		return Settings{}, Secrets{}, fmt.Errorf("configuration: %w", err)
	}
	if secrets.token == "" {
		return Settings{}, Secrets{}, fmt.Errorf("configuration: an API token is required")
	}
	return s, secrets, nil
}

// LoadForSetup allows missing URL/token values so startup can prompt for them.
// Invalid supplied values still fail before any terminal or network activity.
func LoadForSetup(path string) (Settings, Secrets, error) {
	return loadForSetup(path, os.LookupEnv, os.UserHomeDir, os.Open)
}

// LoadConnections returns selectable URLs, with the effective file/environment
// connection first. Overrides affect only that entry; other saved origins retain
// their own settings and credentials. Missing URLs are omitted from the list.
func LoadConnections(path string) ([]Connection, error) {
	connections, _, err := loadConnections(path, os.LookupEnv, os.UserHomeDir, os.Open)
	return connections, err
}

func loadForSetup(path string, env func(string) (string, bool), home func() (string, error), open func(string) (*os.File, error)) (Settings, Secrets, error) {
	_, effective, err := loadConnections(path, env, home, open)
	if err != nil {
		return Settings{}, Secrets{}, err
	}
	if effective.Err != nil {
		return Settings{}, Secrets{}, effective.Err
	}
	return effective.Settings, effective.Secrets, nil
}

func fileConnections(values fileSettings) []connectionSettings {
	connections := append([]connectionSettings(nil), values.Connections...)
	if values.URL != nil && strings.TrimSpace(*values.URL) != "" {
		connections = append([]connectionSettings{values.connectionSettings}, connections...)
	}
	for i := range connections {
		if connections[i].Mode == nil {
			connections[i].Mode = values.Mode
		}
		if connections[i].TLSVerify == nil {
			connections[i].TLSVerify = values.TLSVerify
		}
	}
	return connections
}

func applySettings(s Settings, values connectionSettings) Settings {
	if values.URL != nil {
		s.URL = *values.URL
	}
	if values.Mode != nil {
		s.Mode = *values.Mode
	}
	if values.TLSVerify != nil {
		s.TLSVerify = *values.TLSVerify
	}
	return s
}

// mergeSettings retains field presence when combining non-secret overlays.
func mergeSettings(base, overlay connectionSettings) connectionSettings {
	if overlay.URL != nil {
		base.URL = overlay.URL
	}
	if overlay.Mode != nil {
		base.Mode = overlay.Mode
	}
	if overlay.TLSVerify != nil {
		base.TLSVerify = overlay.TLSVerify
	}
	return base
}

func connection(s Settings, token string) Connection {
	c := Connection{Settings: s}
	check := s
	if strings.TrimSpace(check.URL) == "" {
		c.Settings.URL = ""
		check.URL = "https://missing.invalid"
	}
	if err := Validate(check); err != nil {
		c.Err = fmt.Errorf("configuration: %w", err)
		return c
	}
	if strings.TrimSpace(token) != "" {
		if err := ValidateToken(token); err != nil {
			c.Err = fmt.Errorf("configuration: %w", err)
			return c
		}
		c.Secrets = Secrets{token: token}
	}
	return c
}

func loadConnections(path string, env func(string) (string, bool), home func() (string, error), open func(string) (*os.File, error)) ([]Connection, Connection, error) {
	fail := func(message string) ([]Connection, Connection, error) {
		return nil, Connection{}, fmt.Errorf("configuration: %s", message)
	}
	h, err := home()
	if err != nil || !filepath.IsAbs(h) {
		return fail("cannot locate home directory")
	}
	privatePath := filepath.Join(h, ".config", "aaptui", "config.json")
	explicit := path != ""
	if !explicit {
		dir, _ := env("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(h, ".config")
		}
		path = filepath.Join(dir, "aaptui", "config.json")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return fail("cannot locate settings file")
	}
	sources := []string{privatePath}
	if path != privatePath {
		sources = append(sources, path)
	}
	defaults := Settings{Mode: Gateway, TLSVerify: true}
	s := defaults
	var saved, entries []connectionSettings
	var selected connectionSettings
	for _, source := range sources {
		values, err := readSettings(source, open)
		if err != nil {
			if !os.IsNotExist(err) || (explicit && source == path) {
				return fail("cannot read settings file or invalid settings file")
			}
			continue
		}
		records := fileConnections(values)
		// Check token fields even in a legacy file with a missing URL.
		for _, record := range append([]connectionSettings{values.connectionSettings}, values.Connections...) {
			if record.Token == nil {
				continue
			}
			if source != privatePath {
				return fail("tokens are allowed only in the private home configuration")
			}
			if err := privateFile(source); err != nil {
				return fail("credential storage must use a private directory (0700) and regular file (0600)")
			}
			if strings.TrimSpace(*record.Token) != "" && record.URL == nil {
				return fail("a saved token requires a valid server URL")
			}
		}
		if source == privatePath {
			saved = records
			if values.URL == nil {
				defaults = applySettings(defaults, values.connectionSettings)
			}
		}
		if source != privatePath {
			selected = values.connectionSettings
			if len(records) > 0 {
				// The selected file's first connection supplies its effective
				// overrides; other entries remain separately selectable.
				selected = mergeSettings(selected, records[0])
			}
		}
		entries = append(records, entries...)
		s = applySettings(s, values.connectionSettings)
		if len(records) > 0 {
			s = applySettings(s, records[0])
		}
	}
	if v, ok := env("AAP_URL"); ok {
		s.URL = v
	}
	// Resolve this origin's settings before applying its overrides. A foreign
	// URL never inherits another saved connection's mode or TLS preference.
	effectiveURL := s.URL
	s = defaults
	for _, record := range saved {
		if record.URL != nil && SameOrigin(effectiveURL, *record.URL) {
			s = applySettings(s, record)
			break
		}
	}
	s = applySettings(s, selected)
	s.URL = effectiveURL
	if v, ok := env("AAP_CONNECTION_MODE"); ok {
		s.Mode = ConnectionMode(v)
	}
	var overrideErr error
	if v, ok := env("AAP_TLS_VERIFY"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			overrideErr = fmt.Errorf("configuration: AAP_TLS_VERIFY must be a boolean")
		} else {
			s.TLSVerify = b
		}
	}
	tokenFor := func(url string) string {
		for _, record := range saved {
			if record.URL != nil && SameOrigin(url, *record.URL) && record.Token != nil {
				return *record.Token
			}
		}
		return ""
	}
	token := tokenFor(s.URL)
	if v, ok := env("AAP_TOKEN"); ok {
		token = v
	}
	effective := connection(s, token)
	if overrideErr != nil {
		effective.Err = overrideErr
	}
	var result []Connection
	if effective.Settings.URL != "" {
		result = append(result, effective)
	}
	for _, record := range entries {
		settings := defaults
		for _, private := range saved {
			if record.URL != nil && private.URL != nil && SameOrigin(*record.URL, *private.URL) {
				settings = applySettings(settings, private)
				break
			}
		}
		settings = applySettings(settings, record)
		duplicate := false
		for _, c := range result {
			if settings.URL == c.Settings.URL || SameOrigin(settings.URL, c.Settings.URL) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		result = append(result, connection(settings, tokenFor(settings.URL)))
	}
	return result, effective, nil
}

func readSettings(path string, open func(string) (*os.File, error)) (fileSettings, error) {
	f, err := open(path)
	if err != nil {
		return fileSettings{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, 64*1024+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(data) > 64*1024 {
		return fileSettings{}, fmt.Errorf("cannot read or close settings file, or file exceeds 64 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var values *fileSettings
	var trailing any
	if dec.Decode(&values) != nil || values == nil || dec.Decode(&trailing) != io.EOF {
		return fileSettings{}, fmt.Errorf("invalid settings file")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return fileSettings{}, fmt.Errorf("invalid settings file")
	}
	for key, value := range fields {
		if (key != "url" && key != "token" && key != "connection_mode" && key != "tls_verify" && key != "connections") || string(bytes.TrimSpace(value)) == "null" {
			return fileSettings{}, fmt.Errorf("invalid settings field")
		}
	}
	if raw, ok := fields["connections"]; ok {
		if values.URL != nil || values.Token != nil {
			return fileSettings{}, fmt.Errorf("cannot mix legacy credentials and connections")
		}
		var records []map[string]json.RawMessage
		if json.Unmarshal(raw, &records) != nil {
			return fileSettings{}, fmt.Errorf("invalid connection list")
		}
		for i, record := range records {
			if record == nil || record["url"] == nil || values.Connections[i].URL == nil {
				return fileSettings{}, fmt.Errorf("saved connections require a URL")
			}
			if strings.TrimSpace(*values.Connections[i].URL) == "" {
				return fileSettings{}, fmt.Errorf("saved connections require a URL")
			}
			for j := 0; j < i; j++ {
				if SameOrigin(*values.Connections[i].URL, *values.Connections[j].URL) {
					return fileSettings{}, fmt.Errorf("duplicate saved connection origin")
				}
			}
			for _, value := range record {
				if string(bytes.TrimSpace(value)) == "null" {
					return fileSettings{}, fmt.Errorf("invalid settings field")
				}
			}
		}
	}
	return *values, nil
}

// ValidateToken rejects characters that cannot safely appear in a bearer header.
func ValidateToken(token string) error {
	if len(token) == 0 || len(token) > 8192 {
		return fmt.Errorf("API token must be nonempty and at most 8192 bytes")
	}
	for _, r := range token {
		if r <= ' ' || r > '~' {
			return fmt.Errorf("API token must contain only printable ASCII without whitespace")
		}
	}
	return nil
}

// SameOrigin accounts for case, a trailing slash, and default HTTP(S) ports.
func SameOrigin(a, b string) bool {
	parse := func(raw string) (string, string, string) {
		if Validate(Settings{URL: raw, Mode: Gateway}) != nil {
			return "", "", ""
		}
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", ""
		}
		port := u.Port()
		if port == "" {
			port = "80"
			if u.Scheme == "https" {
				port = "443"
			}
		}
		return u.Scheme, strings.ToLower(u.Hostname()), port
	}
	as, ah, ap := parse(a)
	bs, bh, bp := parse(b)
	return as != "" && as == bs && ah == bh && ap == bp
}
