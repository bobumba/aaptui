package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRejectSecretArguments(t *testing.T) {
	for _, args := range [][]string{{"--token", "secret"}, {"--secret"}, {"secret"}} {
		err := run(args)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe argument error", err)
		}
	}
}
func TestDependencyStartupDoesNotWriteFiles(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command(executable, "-test.run=^TestRejectSecretArguments$")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("startup: %v %s", err, output)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("dependency wrote unsolicited files", files)
	}
}
