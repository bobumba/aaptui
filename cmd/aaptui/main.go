package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
	"aaptui/internal/config"
	"aaptui/internal/tui"
)

func run(args []string) (resultErr error) {
	flags := flag.NewFlagSet("aaptui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "configuration file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stdout, "Usage: aaptui [--config PATH]\nAuthentication: AAP_TOKEN environment variable")
			return nil
		}
		return fmt.Errorf("invalid command-line options; use --help")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	s, secrets, err := config.Load(*path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connection := fmt.Sprintf("%s · TLS verification: %t", s.Mode, s.TLSVerify)
	client, err := aap.NewClient(s, secrets.Token(), nil)
	if err != nil {
		return err
	}
	m := tui.New(ctx, connection).WithTemplates(client).WithJobs(client).WithWorkflows(client).WithCancellation(client).WithOutput(func(ctx context.Context, ref aap.JobRef) tui.OutputSession { return client.NewOutputSession(ctx, ref) })
	defer func() {
		if err := m.Close(); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if _, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithoutSignalHandler()).Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("terminal session failed")
	}
	return nil
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
