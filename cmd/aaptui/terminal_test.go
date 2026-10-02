package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorColumns(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{"HPA", "\x1b[4`123    resource\x1b[215`job_template", "\x1b[4G123    resource\x1b[215Gjob_template"},
		{"default column", "\x1b[`name", "\x1b[Gname"},
		{"literal backticks", "name `example` [4` 部署", "name `example` [4` 部署"},
		{"other commands", "\x1b[5;4H\x1b[193X\x1b[38;5;248mname\x1b[m\r\n\x1b[54d", "\x1b[5;4H\x1b[193X\x1b[38;5;248mname\x1b[m\r\n\x1b[54d"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			if got := string(cursorColumns(input)); got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
			if string(input) != tt.input {
				t.Fatal("modified the renderer's input buffer")
			}
		})
	}
}

func TestTerminalOutput(t *testing.T) {
	for _, method := range []string{"write", "string", "copy"} {
		t.Run(method, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "output")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			w := terminalOutput{file}
			if w.Fd() != file.Fd() {
				t.Fatal("terminal file descriptor was lost")
			}
			input := "\x1b[215`job_template"
			var n int
			switch method {
			case "write":
				n, err = w.Write([]byte(input))
			case "string":
				n, err = io.WriteString(w, input)
			case "copy":
				// LimitReader has no WriteTo, exercising the output's ReadFrom.
				var copied int64
				copied, err = io.Copy(w, io.LimitReader(strings.NewReader(input), int64(len(input))))
				n = int(copied)
			}
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("write: %v; close: %v", err, closeErr)
			}
			if n != len(input) {
				t.Fatalf("wrote %d bytes, want %d", n, len(input))
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte("\x1b[215Gjob_template")) {
				t.Fatalf("output = %q", got)
			}
		})
	}
}

func TestTerminalOutputReportsWriteErrors(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := (terminalOutput{file}).Write([]byte("\x1b[4`name")); err == nil || n != 0 {
		t.Fatalf("closed file write = %d, %v", n, err)
	}
}
