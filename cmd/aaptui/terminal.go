package main

import (
	"bytes"
	"io"
	"os"
)

// terminalOutput preserves the file descriptor for terminal size and color
// detection while using CHA (CSI n G) instead of HPA (CSI n `). Older tmux
// versions advertising TERM=screen support CHA but ignore HPA, which causes
// rows to wrap and leaves apparent list entries that cannot be selected.
type terminalOutput struct{ *os.File }

func (w terminalOutput) Write(p []byte) (int, error) {
	return w.File.Write(cursorColumns(p))
}

func (w terminalOutput) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w terminalOutput) ReadFrom(r io.Reader) (int64, error) {
	// Hide ReadFrom so io.Copy routes every chunk through Write.
	return io.Copy(struct{ io.Writer }{w}, r)
}

// Bubble Tea flushes complete escape sequences in each renderer write. Changing
// only the command byte preserves both its parameters and Write's byte count.
func cursorColumns(p []byte) []byte {
	var out []byte
	for i := 0; i+2 < len(p); i++ {
		if p[i] != '\x1b' || p[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(p) && p[j] >= '0' && p[j] <= '9' {
			j++
		}
		if j < len(p) && p[j] == '`' {
			if out == nil {
				out = bytes.Clone(p)
			}
			out[j] = 'G'
		}
	}
	if out != nil {
		return out
	}
	return p
}
