// Package config loads non-secret connection settings and an environment-only token.
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
}

func Validate(s Settings) error {
	u, err := url.Parse(s.URL)
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || strings.Contains(s.URL, "#") {
		return fmt.Errorf("server URL must be an HTTPS origin without credentials, query, fragment, or path")
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
	s := Settings{Mode: Gateway, TLSVerify: true}
	fail := func(message string) (Settings, Secrets, error) {
		return Settings{}, Secrets{}, fmt.Errorf("configuration: %s", message)
	}
	explicit := path != ""
	if !explicit {
		dir, _ := env("XDG_CONFIG_HOME")
		if dir == "" {
			h, err := home()
			if err != nil {
				return fail("cannot locate home directory")
			}
			dir = filepath.Join(h, ".config")
		}
		path = filepath.Join(dir, "aaptui", "config.json")
	}
	f, err := open(path)
	if err != nil {
		if explicit || !os.IsNotExist(err) {
			return fail("cannot read settings file")
		}
	} else {
		data, readErr := io.ReadAll(io.LimitReader(f, 64*1024+1))
		closeErr := f.Close()
		if readErr != nil || len(data) > 64*1024 {
			return fail("settings file cannot be read or exceeds 64 KiB")
		}
		if closeErr != nil {
			return fail("cannot close settings file")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		var values *fileSettings
		decodeErr := dec.Decode(&values)
		var trailing any
		if decodeErr == nil && dec.Decode(&trailing) != io.EOF {
			decodeErr = fmt.Errorf("trailing data")
		}
		if decodeErr != nil || values == nil {
			return fail("invalid settings file (only url, connection_mode, and tls_verify are allowed)")
		}
		rawFields := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &rawFields); err != nil {
			return fail("invalid settings file")
		}
		for _, value := range rawFields {
			if string(bytes.TrimSpace(value)) == "null" {
				return fail("settings fields must not be null")
			}
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
	if err := Validate(s); err != nil {
		return fail(err.Error())
	}
	token, _ := env("AAP_TOKEN")
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return fail("AAP_TOKEN must contain a nonempty token without newlines")
	}
	return s, Secrets{token: token}, nil
}
