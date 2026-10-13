package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// privateFile refuses symlinked credential storage and group/world access.
func privateFile(path string) error {
	for _, dir := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("invalid credential directory")
		}
		if dir == filepath.Dir(path) && info.Mode().Perm() != 0700 {
			return fmt.Errorf("credential directory must have permissions 0700")
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return fmt.Errorf("credential file must be regular with permissions 0600")
	}
	return nil
}

// SaveCredentials saves only the URL/token as credentials, alongside settings.
// It never changes an XDG or explicitly selected settings file.
func SaveCredentials(s Settings, token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("save credentials: cannot locate home directory")
	}
	return saveCredentials(home, s, token)
}

func saveCredentials(home string, s Settings, token string) (resultErr error) {
	fail := func(message string) error { return fmt.Errorf("save credentials: %s", message) }
	if !filepath.IsAbs(home) {
		return fail("cannot locate home directory")
	}
	if err := Validate(s); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	if err := ValidateToken(token); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return fail("cannot open home directory")
	}
	defer func() {
		if err := root.Close(); err != nil {
			resultErr = errors.Join(resultErr, fail("cannot close home directory"))
		}
	}()
	for _, dir := range []string{".config", filepath.Join(".config", "aaptui")} {
		if err := root.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return fail("cannot create private directory")
		}
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			return fail("credential directories must not be symbolic links")
		}
	}
	dir, err := root.OpenRoot(filepath.Join(".config", "aaptui"))
	if err != nil {
		return fail("cannot open private directory")
	}
	defer func() {
		if err := dir.Close(); err != nil {
			resultErr = errors.Join(resultErr, fail("cannot close private directory"))
		}
	}()
	if err := dir.Chmod(".", 0700); err != nil {
		return fail("cannot secure private directory")
	}
	const path = "config.json"
	if info, err := dir.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fail("credential file must be a regular file")
		}
	} else if !os.IsNotExist(err) {
		return fail("cannot inspect credential file")
	}
	values, err := readSettings(path, dir.Open)
	if err != nil && !os.IsNotExist(err) {
		return fail("cannot read existing settings; file was not replaced")
	}
	values.URL, values.Token = &s.URL, &token
	// Retain the connection choices used with this URL/token on later launches.
	values.Mode, values.TLSVerify = &s.Mode, &s.TLSVerify
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil || len(data)+1 > 64*1024 {
		return fail("cannot encode credential file")
	}
	name := ".config-" + rand.Text()
	f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("cannot create temporary credential file")
	}
	closed := false
	defer func() {
		if !closed {
			if err := f.Close(); err != nil {
				resultErr = errors.Join(resultErr, fail("cannot close temporary credential file"))
			}
		}
		if err := dir.Remove(name); err != nil && !os.IsNotExist(err) {
			resultErr = errors.Join(resultErr, fail("cannot remove temporary credential file"))
		}
	}()
	if err := f.Chmod(0600); err != nil {
		return fail("cannot secure credential file")
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fail("cannot write credential file")
	}
	if err := f.Sync(); err != nil {
		return fail("cannot sync credential file")
	}
	closed = true
	if err := f.Close(); err != nil {
		return fail("cannot close credential file")
	}
	if err := dir.Rename(name, path); err != nil {
		return fail("cannot replace credential file")
	}
	return nil
}
