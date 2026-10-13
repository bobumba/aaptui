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

type fileSettings struct {
	URL       *string         `json:"url"`
	Mode      *ConnectionMode `json:"connection_mode"`
	TLSVerify *bool           `json:"tls_verify"`
	Token     *string         `json:"token,omitempty"`
}

func Validate(s Settings) error {
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

func loadForSetup(path string, env func(string) (string, bool), home func() (string, error), open func(string) (*os.File, error)) (Settings, Secrets, error) {
	s := Settings{Mode: Gateway, TLSVerify: true}
	fail := func(message string) (Settings, Secrets, error) {
		return Settings{}, Secrets{}, fmt.Errorf("configuration: %s", message)
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
	var saved fileSettings
	sources := []string{privatePath}
	if path != privatePath {
		sources = append(sources, path)
	}
	for _, source := range sources {
		values, err := readSettings(source, open)
		if err != nil {
			if !os.IsNotExist(err) || (explicit && source == path) {
				return fail("cannot read settings file or invalid settings file")
			}
			continue
		}
		if values.Token != nil {
			if source != privatePath {
				return fail("tokens are allowed only in the private home configuration")
			}
			if err := privateFile(source); err != nil {
				return fail("credential storage must use a private directory (0700) and regular file (0600)")
			}
			if strings.TrimSpace(*values.Token) != "" {
				if values.URL == nil || Validate(Settings{URL: *values.URL, Mode: Gateway}) != nil {
					return fail("a saved token requires a valid server URL")
				}
				if err := ValidateToken(*values.Token); err != nil {
					return fail(err.Error())
				}
			}
		}
		if source == privatePath {
			saved = values
		}
		if values.URL != nil {
			s.URL = *values.URL
		}
		if values.Mode != nil {
			s.Mode = *values.Mode
		}
		if values.TLSVerify != nil {
			s.TLSVerify = *values.TLSVerify
		}
	}
	if v, ok := env("AAP_URL"); ok {
		s.URL = v
	}
	if v, ok := env("AAP_CONNECTION_MODE"); ok {
		s.Mode = ConnectionMode(v)
	}
	if v, ok := env("AAP_TLS_VERIFY"); ok {
		b, e := strconv.ParseBool(v)
		if e != nil {
			return fail("AAP_TLS_VERIFY must be a boolean")
		}
		s.TLSVerify = b
	}
	check := s
	if strings.TrimSpace(check.URL) == "" {
		s.URL = ""
		check.URL = "https://missing.invalid"
	}
	if err := Validate(check); err != nil {
		return fail(err.Error())
	}
	token := ""
	if saved.Token != nil && saved.URL != nil && SameOrigin(s.URL, *saved.URL) {
		token = *saved.Token
	}
	if v, ok := env("AAP_TOKEN"); ok {
		token = v
	}
	if strings.TrimSpace(token) == "" {
		token = ""
	} else if err := ValidateToken(token); err != nil {
		return fail(err.Error())
	}
	return s, Secrets{token: token}, nil
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
		if (key != "url" && key != "token" && key != "connection_mode" && key != "tls_verify") || string(bytes.TrimSpace(value)) == "null" {
			return fileSettings{}, fmt.Errorf("invalid settings field")
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
