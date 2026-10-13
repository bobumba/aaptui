package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
	"aaptui/internal/tui"
)

func run() error {
	return runWithArgs(os.Args[1:])
}

func runWithArgs(args []string) (resultErr error) {
	flags := flag.NewFlagSet("aaptui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "configuration file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stdout, "Usage: aaptui [--config PATH]\nStartup: choose the current configuration or create a new connection in an interactive terminal")
			return nil
		}
		return fmt.Errorf("invalid command-line options; use --help")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s, token, err := startConnection(ctx, *path, terminalStartupDependencies())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	connection := fmt.Sprintf("%s · TLS verification: %t", s.Mode, s.TLSVerify)
	if strings.HasPrefix(strings.ToLower(s.URL), "http://") {
		connection = fmt.Sprintf("%s · HTTP · TLS verification: not applicable", s.Mode)
	}
	client, err := aap.NewClient(s, token, nil)
	if err != nil {
		return err
	}
	m := tui.New(ctx, connection).WithIdentity(client.CleanText(s.URL), client).WithTemplates(client).WithTemplateJobs(client).WithProjects(client).WithInventories(client).WithInventoryContents(client).WithObjectDetails(client).WithHostGroups(client).WithJobs(client).WithWorkflows(client).WithCancellation(client).WithRelaunch(client).WithOutput(func(ctx context.Context, ref aap.JobRef) tui.OutputSession { return client.NewOutputSession(ctx, ref) })
	m.WithLaunch(client).WithRunningJobs(func(ctx context.Context) tui.RunningJobsPoller { return client.NewRunningJobsPoller(ctx) })
	defer func() {
		if err := m.Close(); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if _, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithOutput(terminalOutput{os.Stdout}), tea.WithoutSignalHandler()).Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("terminal session failed")
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
