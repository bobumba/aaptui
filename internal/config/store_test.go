package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedCredentialsAndOverrides(t *testing.T) {
	home := t.TempDir()
	s := Settings{URL: "https://AAP.example:443/", Mode: Gateway, TLSVerify: true}
	if err := saveCredentials(home, s, "saved-token"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "aaptui", "config.json")
	for _, tc := range []struct {
		name, url, token string
		env              map[string]string
	}{
		{"saved", s.URL, "saved-token", nil},
		{"same origin", "https://aap.example", "saved-token", map[string]string{"AAP_URL": "https://aap.example"}},
		{"different host", "https://other.example", "", map[string]string{"AAP_URL": "https://other.example"}},
		{"different scheme", "http://aap.example", "", map[string]string{"AAP_URL": "http://aap.example"}},
		{"different port", "https://aap.example:8443", "", map[string]string{"AAP_URL": "https://aap.example:8443"}},
		{"environment token", s.URL, "env-token", map[string]string{"AAP_TOKEN": "env-token"}},
		{"blank token", s.URL, "", map[string]string{"AAP_TOKEN": " "}},
		{"blank URL", "", "", map[string]string{"AAP_URL": " "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }
			got, secrets, err := loadForSetup("", lookup, func() (string, error) { return home, nil }, os.Open)
			if err != nil || got.URL != tc.url || secrets.Token() != tc.token {
				t.Fatalf("URL=%q token present=%t err=%v", got.URL, secrets.Token() != "", err)
			}
		})
	}
	for target, perm := range map[string]os.FileMode{filepath.Dir(path): 0700, path: 0600} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != perm {
			t.Fatalf("private permissions: %v", err)
		}
	}
	// Replacing credentials retains the effective connection choices and leaves
	// exactly one complete JSON file, with no password or temporary files.
	s.Mode, s.TLSVerify = Direct, false
	if err := saveCredentials(home, s, "replacement-token"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil || len(fields) != 4 || fields["connection_mode"] != "direct" || fields["tls_verify"] != false {
		t.Fatal("saved configuration is incomplete")
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 || files[0].Name() != "config.json" {
		t.Fatal("unexpected credential files", err)
	}
}

func TestSetupSettingsFilesAndValidation(t *testing.T) {
	home := t.TempDir()
	lookupEmpty := func(string) (string, bool) { return "", false }
	getHome := func() (string, error) { return home, nil }
	s, token, err := loadForSetup("", lookupEmpty, getHome, os.Open)
	if err != nil || s.URL != "" || token.Token() != "" || !s.TLSVerify || s.Mode != Gateway {
		t.Fatal("missing settings did not preserve defaults", err)
	}
	if err := saveCredentials(home, Settings{URL: "https://saved.example", Mode: Gateway, TLSVerify: true}, "saved"); err != nil {
		t.Fatal(err)
	}
	xdg := t.TempDir()
	if err := os.Mkdir(filepath.Join(xdg, "aaptui"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(xdg, "aaptui", "config.json")
	if err := os.WriteFile(path, []byte(`{"url":"https://selected.example","tls_verify":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	lookup := func(k string) (string, bool) {
		if k == "XDG_CONFIG_HOME" {
			return xdg, true
		}
		return "", false
	}
	s, token, err = loadForSetup("", lookup, getHome, os.Open)
	if err != nil || s.URL != "https://selected.example" || s.TLSVerify || token.Token() != "" {
		t.Fatal("selected settings did not override home settings safely", err)
	}
	for _, body := range []string{`{"token":"sensitive"}`, `{"url":"https://user:sensitive@example.org"}`, `{"connection_mode":"sensitive"}`, `{"tls_verify":null}`, `{"unknown":"sensitive"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, _, err := loadForSetup(path, lookupEmpty, getHome, os.Open)
		if err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("unsafe or absent validation error", err)
		}
	}
	for key, value := range map[string]string{"AAP_URL": "https://example.org/path", "AAP_TOKEN": "secret\nvalue", "AAP_CONNECTION_MODE": "secret", "AAP_TLS_VERIFY": "secret"} {
		lookup := func(k string) (string, bool) {
			if k == key {
				return value, true
			}
			return "", false
		}
		_, _, err := loadForSetup("", lookup, getHome, os.Open)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid environment accepted or leaked", err)
		}
	}
}

func TestCredentialStorageFailures(t *testing.T) {
	for _, scenario := range []string{"malformed", "symlink file", "symlink directory", "file directory", "missing home", "public file", "public directory"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			s := Settings{URL: "https://example.org", Mode: Gateway, TLSVerify: true}
			if err := saveCredentials(home, s, "original-token"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, ".config", "aaptui", "config.json")
			switch scenario {
			case "malformed":
				if err := os.WriteFile(path, []byte(`{"password":"sensitive"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink file", "file directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if scenario == "symlink file" {
					if err := os.Symlink(filepath.Join(t.TempDir(), "secret"), path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink directory":
				home = t.TempDir()
				if err := os.Symlink(t.TempDir(), filepath.Join(home, ".config")); err != nil {
					t.Fatal(err)
				}
			case "missing home":
				home = filepath.Join(home, "missing")
			case "public file", "public directory":
				target := path
				if scenario == "public directory" {
					target = filepath.Dir(path)
				}
				if err := os.Chmod(target, 0777); err != nil {
					t.Fatal(err)
				}
				_, _, err := loadForSetup("", func(string) (string, bool) { return "", false }, func() (string, error) { return home, nil }, os.Open)
				if err == nil {
					t.Fatal("insecure credential storage accepted")
				}
				return
			}
			before, readErr := os.ReadFile(path)
			err := saveCredentials(home, s, "new-sensitive-token")
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("missing or unsafe storage error", err)
			}
			if readErr == nil {
				after, err := os.ReadFile(path)
				if err != nil || string(before) != string(after) {
					t.Fatal("failed save replaced existing settings", err)
				}
			}
		})
	}
}
