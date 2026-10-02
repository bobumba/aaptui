package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"url":"https://file.example","tls_verify":false,"connection_mode":"direct"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"AAP_TOKEN": "secret", "AAP_URL": "https://env.example"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	s, secret, err := load(path, lookup, func() (string, error) { return dir, nil }, os.Open)
	if err != nil || s.URL != env["AAP_URL"] || s.TLSVerify || s.Mode != Direct || secret.Token() != "secret" {
		t.Fatalf("settings=%+v err=%v", s, err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", secret, secret, secret), "secret") {
		t.Fatal("secret printed")
	}
	env["AAP_TLS_VERIFY"] = "true"
	s, _, err = load(path, lookup, os.UserHomeDir, os.Open)
	if err != nil || !s.TLSVerify {
		t.Fatal(s, err)
	}
	for _, body := range []string{`{"token":"secret"}`, `{"url":123}`, `null`, `{} {}`, `{"tls_verify":"secret"}`, `{"tls_verify":null}`, `{"url":null}`, `{"connection_mode":null}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, _, err = load(path, lookup, os.UserHomeDir, os.Open)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe error %v", err)
		}
	}
}
func TestDiscovery(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"AAP_URL": "https://example.org", "AAP_TOKEN": "secret", "XDG_CONFIG_HOME": dir}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	if _, _, err := load("", lookup, os.UserHomeDir, os.Open); err != nil {
		t.Fatal(err)
	}
	if _, _, err := load(filepath.Join(dir, "missing"), lookup, os.UserHomeDir, os.Open); err == nil {
		t.Fatal("missing explicit file accepted")
	}
	delete(env, "XDG_CONFIG_HOME")
	if err := os.MkdirAll(filepath.Join(dir, ".config", "aaptui"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".config", "aaptui", "config.json"), []byte(`{"tls_verify":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, _, err := load("", lookup, func() (string, error) { return dir, nil }, os.Open)
	if err != nil || s.TLSVerify {
		t.Fatal(s, err)
	}
	delete(env, "AAP_TOKEN")
	if _, _, err = load("", lookup, func() (string, error) { return dir, nil }, os.Open); err == nil {
		t.Fatal("missing token accepted")
	}
}
func TestValidate(t *testing.T) {
	for _, u := range []string{"", "http://x", "https://user:secret@x", "https://x/path", "https://x?secret", "https://x#secret", "https://x?", "https://x#", "https:///"} {
		if Validate(Settings{URL: u, Mode: Gateway}) == nil {
			t.Errorf("accepted %s", u)
		}
	}
	if Validate(Settings{URL: "https://x", Mode: "other"}) == nil {
		t.Fatal("invalid mode")
	}
}
func TestInvalidEnvironmentAndOversizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`+strings.Repeat(" ", 64*1024)), 0600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"AAP_URL": "https://example.org", "AAP_TOKEN": "secret"}
	lookup := func(k string) (string, bool) { v, ok := base[k]; return v, ok }
	if _, _, err := load(path, lookup, os.UserHomeDir, os.Open); err == nil {
		t.Fatal("oversize file accepted")
	}
	for key, value := range map[string]string{"AAP_URL": "", "AAP_TOKEN": "", "AAP_CONNECTION_MODE": "bad secret mode", "AAP_TLS_VERIFY": "secret"} {
		old, exists := base[key]
		base[key] = value
		_, _, err := load(filepath.Join(t.TempDir(), "missing"), lookup, os.UserHomeDir, os.Open)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe settings error", err)
		}
		if exists {
			base[key] = old
		} else {
			delete(base, key)
		}
	}
}
