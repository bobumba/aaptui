//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/sys/unix"

	"aaptui/internal/aap"
	"aaptui/internal/tui"
)

type terminalCapture struct {
	terminalOutput
	frames chan []byte
	offset int64
}

func (w *terminalCapture) Write(p []byte) (int, error) {
	n, err := w.terminalOutput.Write(p)
	if err != nil {
		return n, err
	}
	// Emulate the bytes actually written, rather than the unfiltered renderer
	// input, so this test also catches removal of the output compatibility fix.
	frame := make([]byte, n)
	if _, err := w.File.ReadAt(frame, w.offset); err != nil {
		return n, err
	}
	w.offset += int64(n)
	w.frames <- frame
	return n, nil
}

type terminalTemplates struct{}

func (terminalTemplates) ListTemplates(context.Context, aap.ListOptions) (aap.Page[aap.TemplateSummary], error) {
	page := aap.Page[aap.TemplateSummary]{Count: 50}
	for i := 1; i <= page.Count; i++ {
		page.Items = append(page.Items, aap.TemplateSummary{
			Ref:  aap.TemplateRef{ID: i, Type: aap.PlaybookTemplate},
			Name: fmt.Sprintf("resource-%03d", i),
		})
	}
	return page, nil
}

func (terminalTemplates) Template(_ context.Context, ref aap.TemplateRef) (aap.TemplateDetails, error) {
	return aap.TemplateDetails{TemplateSummary: aap.TemplateSummary{Ref: ref, Name: fmt.Sprintf("resource-%03d", ref.ID)}}, nil
}

func TestScreenTerminalColumnsAndLastTemplate(t *testing.T) {
	input := terminalInput(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	file, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output := &terminalCapture{terminalOutput: terminalOutput{file}, frames: make(chan []byte, 64)}
	m := tui.New(ctx, "gateway").WithTemplates(terminalTemplates{})
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output), tea.WithWindowSize(237, 62), tea.WithEnvironment([]string{"TERM=screen"}), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	t.Cleanup(func() {
		// Graceful shutdown waits for the PTY reader before closing its file
		// descriptors; cancelling the program kills that reader asynchronously.
		p.Send(tea.QuitMsg{})
		runErr := <-done
		cancel()
		if runErr != nil && !errors.Is(runErr, tea.ErrProgramKilled) {
			t.Error(runErr)
		}
		if err := m.Close(); err != nil {
			t.Error(err)
		}
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	emulator := vt.NewEmulator(237, 62)
	t.Cleanup(func() {
		if err := emulator.Close(); err != nil {
			t.Error(err)
		}
	})
	// tmux before 2.9 ignores HPA (CSI n `), despite advertising TERM=screen.
	emulator.RegisterCsiHandler('`', func(ansi.Params) bool { return true })
	// This test drives keys through Program.Send, so terminal queries need no
	// replies. Suppress them rather than blocking on the emulator's input pipe.
	emulator.RegisterCsiHandler(ansi.Command('?', '$', 'p'), func(ansi.Params) bool { return true })
	emulator.RegisterCsiHandler(ansi.Command('?', 0, 'u'), func(ansi.Params) bool { return true })
	emulator.RegisterOscHandler(11, func([]byte) bool { return true })
	row := func(y int) string {
		var text strings.Builder
		for x := 0; x < 237; x++ {
			cell := emulator.CellAt(x, y)
			if cell == nil {
				text.WriteByte(' ')
			} else {
				text.WriteString(cell.Content)
			}
		}
		return text.String()
	}
	waitFor := func(match func() bool) {
		t.Helper()
		for {
			select {
			case frame := <-output.frames:
				if _, err := emulator.Write(frame); err != nil {
					t.Fatal(err)
				}
				if match() {
					return
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for terminal frame")
			}
		}
	}
	waitFor(func() bool { return strings.Contains(row(0), "Main") })
	p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor(func() bool { return strings.Contains(row(4), "resource-001") })
	for i := 0; i < 50; i++ {
		line := row(4 + i)
		if !strings.HasPrefix(line[3:], fmt.Sprint(i+1)) || !strings.HasPrefix(line[10:], fmt.Sprintf("resource-%03d", i+1)) || !strings.HasPrefix(line[214:], "job_template") {
			t.Fatalf("initial row %d is not aligned: %q", i+1, line)
		}
	}
	if strings.TrimSpace(row(54)) != "" {
		t.Fatalf("extra apparent template below the list: %q", row(54))
	}
	for i := 1; i < 50; i++ {
		p.Send(tea.KeyPressMsg{Code: tea.KeyDown})
		waitFor(func() bool { return strings.HasPrefix(row(4+i), " > ") })
	}
	p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor(func() bool {
		return strings.Contains(row(0), "Template details") && strings.Contains(emulator.String(), "50 · resource-050")
	})
	p.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	waitFor(func() bool { return strings.Contains(row(0), "Job Templates") && strings.HasPrefix(row(53), " > ") })
}

// Use a real PTY so Bubble Tea enters raw mode and emits the same cursor
// movements as an interactive session. WithInput(nil) uses different newline
// assumptions and does not reproduce the unsupported HPA commands.
func terminalInput(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	pty, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", pty), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Error(err)
		}
	})
	return slave
}
