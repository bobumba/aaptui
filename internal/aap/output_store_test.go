package aap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T, limits OutputLimits) *OutputStore {
	t.Helper()
	s, err := newOutputStore(t.TempDir(), "secret", limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func TestStoreOverlapRedactionPermissions(t *testing.T) {
	s := testStore(t, DefaultOutputLimits())
	if err := s.Accept(OutputChunk{0, 2, 2, "secret\x1b[31mone\npart"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Accept(OutputChunk{1, 3, 3, "partial\nlast\n"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(0, 3)
	if err != nil || got != "[redacted]one\npartial\nlast\n" {
		t.Fatal(got, err)
	}
	if s.Cursor().Line != 3 {
		t.Fatal(s.Cursor())
	}
	if err = s.Accept(OutputChunk{0, 1, 3, "secretone\n"}); err != nil || s.Cursor().Line != 3 {
		t.Fatal(err)
	}
	info, err := os.Stat(s.dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal(info, err)
	}
	for _, f := range s.files {
		info, err := os.Stat(f.path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal(info, err)
		}
		data, err := os.ReadFile(f.path)
		if err != nil || strings.Contains(string(data), "secret") {
			t.Fatal("secret cached", err)
		}
	}
	dir := s.dir
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("cache remained")
	}
}
func TestStoreBudgetsReloadAndFailure(t *testing.T) {
	s := testStore(t, OutputLimits{MemoryBytes: 1, DiskBytes: 256, MaxLines: 2, MaxFiles: 2})
	for i := 0; i < 3; i++ {
		if err := s.Accept(OutputChunk{i, i + 1, i + 1, "line\n"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.memory) != 0 || s.diskBytes > 256 || len(s.index) > 2 || len(s.files) > 2 {
		t.Fatal("budgets exceeded")
	}
	if _, err := s.Read(0, 1); err == nil {
		t.Fatal("evicted line returned")
	}
	if got, err := s.Read(1, 3); err != nil || got != "line\nline\n" {
		t.Fatal(got, err)
	}
	s.writeFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	if err := s.Accept(OutputChunk{3, 4, 4, "new\n"}); err == nil || s.Cursor().Line != 3 {
		t.Fatal("cursor advanced on storage failure")
	}
	if err := s.Accept(OutputChunk{5, 6, 6, "gap\n"}); err == nil {
		t.Fatal("gap accepted")
	}
	if err := s.Accept(OutputChunk{2, 4, 4, "one\n"}); err == nil {
		t.Fatal("bad range accepted")
	}
}
func TestCacheSymlinkAndReadFailure(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, ".cache")); err != nil {
		t.Fatal(err)
	}
	if _, err := newOutputStore(home, "secret", DefaultOutputLimits()); err == nil {
		t.Fatal("cache escaped home")
	}
	s := testStore(t, OutputLimits{MemoryBytes: 1, DiskBytes: 1024, MaxLines: 10, MaxFiles: 10})
	if err := s.Accept(OutputChunk{0, 1, 1, "one\n"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.files[0].path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(0, 1); err == nil {
		t.Fatal("read failure ignored")
	}
}
func TestStoreReportsCleanupFailure(t *testing.T) {
	s := testStore(t, DefaultOutputLimits())
	if err := s.Accept(OutputChunk{0, 1, 1, "one\n"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(s.dir, 0700); err != nil {
			t.Error(err)
		}
	}()
	if err := s.Close(); err == nil {
		t.Fatal("cleanup failure was ignored")
	}
}
