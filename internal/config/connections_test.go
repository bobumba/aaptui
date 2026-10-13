package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionCollectionMigrationAndUpdates(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "aaptui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	legacy := `{"url":"https://one.example","token":"one-token","connection_mode":"direct","tls_verify":false}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	getHome := func() (string, error) { return home, nil }
	noEnv := func(string) (string, bool) { return "", false }
	entries, _, err := loadConnections("", noEnv, getHome, os.Open)
	if err != nil || len(entries) != 1 || entries[0].Secrets.Token() != "one-token" || entries[0].Settings.Mode != Direct || entries[0].Settings.TLSVerify {
		t.Fatal("legacy connection was lost", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != legacy {
		t.Fatal("reading migrated the legacy file", err)
	}
	two := Settings{URL: "https://two.example", Mode: Gateway, TLSVerify: true}
	if err := saveCredentials(home, two, "two-token"); err != nil {
		t.Fatal(err)
	}
	entries, _, err = loadConnections("", noEnv, getHome, os.Open)
	if err != nil || len(entries) != 2 || entries[0].Settings != two || entries[0].Secrets.Token() != "two-token" || entries[1].Secrets.Token() != "one-token" || entries[1].Settings.Mode != Direct || entries[1].Settings.TLSVerify {
		t.Fatal("save lost another connection", err)
	}
	one := Settings{URL: "https://ONE.example:443/", Mode: Gateway, TLSVerify: true}
	if err := saveCredentials(home, one, "replacement-token"); err != nil {
		t.Fatal(err)
	}
	entries, _, err = loadConnections("", noEnv, getHome, os.Open)
	if err != nil || len(entries) != 2 || entries[0].Settings != one || entries[0].Secrets.Token() != "replacement-token" || entries[1].Settings != two || entries[1].Secrets.Token() != "two-token" {
		t.Fatal("same-origin update lost or duplicated connections", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", entries, entries), "replacement-token") {
		t.Fatal("connection printed credentials")
	}
}

func TestConnectionOverridesStayWithTheirOrigin(t *testing.T) {
	home := t.TempDir()
	one := Settings{URL: "https://one.example", Mode: Direct, TLSVerify: false}
	two := Settings{URL: "https://two.example", Mode: Gateway, TLSVerify: true}
	for _, item := range []struct {
		settings Settings
		token    string
	}{{one, "one-token"}, {two, "two-token"}} {
		if err := saveCredentials(home, item.settings, item.token); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		env   map[string]string
		body  string
		want  Settings
		token string
	}{
		{"saved matching environment URL", map[string]string{"AAP_URL": "https://ONE.example:443/"}, "", Settings{URL: "https://ONE.example:443/", Mode: Direct, TLSVerify: false}, "one-token"},
		{"environment token and settings", map[string]string{"AAP_URL": one.URL, "AAP_TOKEN": "env-token", "AAP_CONNECTION_MODE": "gateway", "AAP_TLS_VERIFY": "true"}, "", Settings{URL: one.URL, Mode: Gateway, TLSVerify: true}, "env-token"},
		{"foreign URL", map[string]string{"AAP_URL": "https://foreign.example"}, "", Settings{URL: "https://foreign.example", Mode: Gateway, TLSVerify: true}, ""},
		{"selected file", nil, `{"url":"https://one.example","tls_verify":true}`, Settings{URL: one.URL, Mode: Direct, TLSVerify: true}, "one-token"},
		{"selected collection", nil, `{"connections":[{"url":"https://one.example","tls_verify":true},{"url":"https://two.example","connection_mode":"direct","tls_verify":false}]}`, Settings{URL: one.URL, Mode: Direct, TLSVerify: true}, "one-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := ""
			if tc.body != "" {
				path = filepath.Join(t.TempDir(), "settings.json")
				if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lookup := func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }
			entries, _, err := loadConnections(path, lookup, func() (string, error) { return home, nil }, os.Open)
			if err != nil || len(entries) < 2 || entries[0].Err != nil || entries[0].Settings != tc.want || entries[0].Secrets.Token() != tc.token {
				t.Fatal("effective connection differs", err)
			}
			for _, c := range entries[1:] {
				if SameOrigin(c.Settings.URL, two.URL) {
					want := two
					if tc.name == "selected collection" {
						want.Mode, want.TLSVerify = Direct, false
					}
					if c.Settings != want || c.Secrets.Token() != "two-token" {
						t.Fatal("overrides changed another origin")
					}
				}
			}
		})
	}
}

func TestConnectionCollectionsValidation(t *testing.T) {
	for _, body := range []string{
		`{"connections":null}`, `{"connections":[null]}`, `{"connections":[{}]}`,
		`{"connections":[{"url":null}]}`, `{"connections":[{"url":" "}]}`,
		`{"connections":[{"url":"https://one.example","token":null}]}`,
		`{"connections":[{"url":"https://one.example","unknown":"sensitive"}]}`,
		`{"connections":[{"url":"https://one.example","connections":[]}]}`,
		`{"connections":[{"url":"https://one.example"},{"url":"https://ONE.example:443/"}]}`,
		`{"url":"https://one.example","connections":[]}`,
	} {
		t.Run(body, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, ".config", "aaptui")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := loadConnections("", func(string) (string, bool) { return "", false }, func() (string, error) { return home, nil }, os.Open)
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("invalid collection accepted or leaked", err)
			}
			if err := saveCredentials(home, Settings{URL: "https://new.example", Mode: Gateway, TLSVerify: true}, "new-token"); err == nil {
				t.Fatal("malformed collection was replaced")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != body {
				t.Fatal("failed save changed collection", err)
			}
		})
	}
}

func TestUnavailableConnectionAndExternalTokens(t *testing.T) {
	home := t.TempDir()
	if err := saveCredentials(home, Settings{URL: "https://saved.example", Mode: Direct, TLSVerify: false}, "saved-token"); err != nil {
		t.Fatal(err)
	}
	lookup := func(k string) (string, bool) {
		if k == "AAP_URL" {
			return "https://user:sensitive@bad.example", true
		}
		return "", false
	}
	entries, _, err := loadConnections("", lookup, func() (string, error) { return home, nil }, os.Open)
	if err != nil || len(entries) != 2 || entries[0].Err == nil || entries[1].Err != nil || entries[1].Secrets.Token() != "saved-token" || strings.Contains(entries[0].Err.Error(), "sensitive") {
		t.Fatal("invalid effective connection blocked saved connection", err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"connections":[{"url":"https://saved.example","token":"sensitive"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConnections(path, func(string) (string, bool) { return "", false }, func() (string, error) { return home, nil }, os.Open); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("external token accepted or leaked", err)
	}
}

func TestInvalidSavedEntryLeavesOtherEntriesAvailable(t *testing.T) {
	for _, invalid := range []string{
		`{"url":"https://bad.example","connection_mode":"sensitive","token":"bad-token"}`,
		`{"url":"https://bad.example","token":"sensitive\nvalue"}`,
		`{"url":"https://user:sensitive@bad.example","token":"bad-token"}`,
		`{"url":"https://bad\u202e.example","token":"bad-token"}`,
	} {
		t.Run(invalid, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, ".config", "aaptui")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			body := `{"connections":[` + invalid + `,{"url":"https://good.example","token":"good-token"}]}`
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			entries, _, err := loadConnections("", func(string) (string, bool) { return "", false }, func() (string, error) { return home, nil }, os.Open)
			if err != nil || len(entries) != 2 || entries[0].Err == nil || strings.Contains(entries[0].Err.Error(), "sensitive") {
				t.Fatal("invalid saved entry accepted or leaked", err)
			}
			found := false
			for _, c := range entries {
				if c.Settings.URL == "https://good.example" && c.Err == nil && c.Secrets.Token() == "good-token" {
					found = true
				}
			}
			if !found {
				t.Fatal("valid connection was blocked by another entry")
			}
		})
	}
}

func TestSavingCollectionPreservesDefaultsAndSizeFailure(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "aaptui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	body := `{"connection_mode":"direct","tls_verify":false}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s := Settings{URL: "https://saved.example", Mode: Gateway, TLSVerify: true}
	if err := saveCredentials(home, s, "saved-token"); err != nil {
		t.Fatal(err)
	}
	values, err := readSettings(path, os.Open)
	if err != nil || values.Mode == nil || *values.Mode != Direct || values.TLSVerify == nil || *values.TLSVerify {
		t.Fatal("unrelated defaults were lost", err)
	}
	// A valid compact file can exceed the write budget after indentation/addition.
	var records []string
	for i := 0; i < 8; i++ {
		records = append(records, fmt.Sprintf(`{"url":"https://host-%d.example","token":"%s"}`, i, strings.Repeat("t", 8000)))
	}
	body = `{"connections":[` + strings.Join(records, ",") + `]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveCredentials(home, s, strings.Repeat("x", 8000)); err == nil {
		t.Fatal("oversized collection was saved")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatal("failed write changed connections", err)
	}
}
