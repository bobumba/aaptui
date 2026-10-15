package aap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aaptui/internal/config"
)

func taskEvent(counter int, changed, failed bool, text string) map[string]any {
	return map[string]any{"counter": counter, "event": "runner_on_ok", "changed": changed, "failed": failed, "task": "deploy secret", "host_name": "web-1", "stdout": text}
}

func writeTaskEvents(t *testing.T, w http.ResponseWriter, events any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{"results": events}); err != nil {
		t.Error(err)
	}
}

// Synthetic large logical collection; the server applies outcome and keyset filters.
func TestTaskOutputFiltersAndBoundedHistory(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Direct, config.Gateway} {
		for _, kind := range []JobType{PlaybookJob, AdHocCommand} {
			t.Run(string(mode)+"/"+string(kind), func(t *testing.T) {
				requests := 0
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					requests++
					prefix, route := "/api/v2/", "jobs/7/job_events/"
					if mode == config.Gateway {
						prefix = "/api/controller/v2/"
					}
					if kind == AdHocCommand {
						route = "ad_hoc_commands/7/ad_hoc_command_events/"
					}
					q := r.URL.Query()
					if r.URL.Path != prefix+route || r.Header.Get("Authorization") != "Bearer secret" || q.Get("limit") != "9" || q.Get("no_truncate") != "1" || q.Get("event__startswith") != "runner_" || q.Get("page") != "" {
						t.Error("unexpected request", r.URL)
					}
					if (q.Get("changed") == "true") == (q.Get("failed") == "true") {
						t.Error("expected exactly one outcome filter")
					}
					before, _ := strconv.Atoi(q.Get("counter__lt"))
					after, _ := strconv.Atoi(q.Get("counter__gt"))
					var events []map[string]any
					// Only generate the tail of a million-event job; unmatched host
					// results contain misleading words but have false outcome flags.
					for counter := 999950; counter <= 1000000; counter++ {
						changed, failed := counter%2 == 0, counter%3 == 0
						if (q.Get("changed") == "true" && !changed) || (q.Get("failed") == "true" && !failed) || (before > 0 && counter >= before) || (after > 0 && counter <= after) {
							continue
						}
						events = append(events, taskEvent(counter, changed, failed, fmt.Sprintf("changed failed secret\x1b[31m result %d\ndiagnostics\n", counter)))
					}
					if after == 0 {
						slices.Reverse(events)
					} else if q.Get("order_by") != "counter" {
						t.Error("wrong newer ordering")
					}
					if len(events) > 9 {
						events = events[:9]
					}
					writeTaskEvents(t, w, events)
				})
				for _, filter := range []OutputFilter{OutputChanged, OutputFailed} {
					ref := JobRef{7, kind}
					page, err := c.TaskOutput(context.Background(), ref, filter, 0, 0)
					if err != nil || page.Last < 999999 || !page.Older || page.Newer || strings.Contains(page.Text, "secret") || strings.Contains(page.Text, "\x1b") || !strings.Contains(page.Text, "TASK [deploy [redacted]] · web-1") || !strings.Contains(page.Text, "\ndiagnostics\n") {
						t.Fatal(page, err)
					}
					older, err := c.TaskOutput(context.Background(), ref, filter, page.First, 0)
					if err != nil || older.Last >= page.First || !older.Newer {
						t.Fatal(older, err)
					}
					newer, err := c.TaskOutput(context.Background(), ref, filter, 0, older.Last)
					if err != nil || newer.Text != page.Text || newer.Newer {
						t.Fatal(newer, err)
					}
				}
				if requests != 6 {
					t.Fatal("unexpected traversal", requests)
				}
			})
		}
	}
}

func TestTaskOutputOutcomeSemantics(t *testing.T) {
	// One task has unchanged, changed, failed/unreachable, ignored-failure,
	// and changed+failed host results. Parent and recap flags are aggregates.
	events := []map[string]any{
		taskEvent(1, false, false, "ok host\n"), taskEvent(2, true, false, "changed host\n"),
		taskEvent(3, false, true, "unreachable host\n"), taskEvent(4, false, false, "ignored failure\n"),
		taskEvent(5, true, true, "changed and failed host\n"), taskEvent(6, true, true, "recap\n"),
	}
	events[2]["event"] = "runner_on_unreachable"
	events[3]["event"] = "runner_on_failed"
	events[3]["event_data"] = map[string]any{"ignore_errors": true}
	events[5]["event"] = "playbook_on_stats"
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		var selected []map[string]any
		for _, e := range events {
			if !strings.HasPrefix(e["event"].(string), "runner_") {
				continue
			}
			flag := "changed"
			if r.URL.Query().Get("failed") == "true" {
				flag = "failed"
			}
			if e[flag].(bool) {
				selected = append(selected, e)
			}
		}
		slices.Reverse(selected)
		writeTaskEvents(t, w, selected)
	})
	for _, filter := range []OutputFilter{OutputChanged, OutputFailed} {
		p, err := c.TaskOutput(context.Background(), JobRef{1, PlaybookJob}, filter, 0, 0)
		if err != nil || strings.Contains(p.Text, "ok host") || strings.Contains(p.Text, "ignored failure") || strings.Contains(p.Text, "recap") || !strings.Contains(p.Text, "changed and failed host") {
			t.Fatal(p, err)
		}
		if (strings.Contains(p.Text, "unreachable host")) != (filter == OutputFailed) {
			t.Fatal("wrong failure classification", p)
		}
	}
}

func TestTaskOutputValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response any
		kind     ErrorKind
	}{
		{"missing results", map[string]any{}, Malformed},
		{"null results", map[string]any{"results": nil}, Malformed},
		{"missing metadata", map[string]any{"results": []any{map[string]any{"counter": 1}}}, Malformed},
		{"wrong flag", map[string]any{"results": []any{taskEvent(1, false, false, "changed\n")}}, Malformed},
		{"duplicate counter", map[string]any{"results": []any{taskEvent(1, true, false, "x"), taskEvent(1, true, false, "x")}}, Malformed},
		{"too many events", map[string]any{"results": make([]int, 10)}, Malformed},
		{"oversize text", map[string]any{"results": []any{taskEvent(1, true, false, strings.Repeat("x", OutputBytes))}}, History},
		{"too many lines", map[string]any{"results": []any{taskEvent(1, true, false, strings.Repeat("\n", 65537))}}, History},
		{"oversize response", map[string]any{"results": []any{taskEvent(1, true, false, strings.Repeat("x", maxBody))}}, Malformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewEncoder(w).Encode(tc.response); err != nil {
					t.Error(err)
				}
			})
			_, err := c.TaskOutput(context.Background(), JobRef{1, PlaybookJob}, OutputChanged, 0, 0)
			var api *APIError
			if !errors.As(err, &api) || api.Kind != tc.kind {
				t.Fatal(err)
			}
		})
	}
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input made a request") })
	for _, args := range []struct {
		ref           JobRef
		filter        OutputFilter
		before, after int
	}{
		{JobRef{1, SystemJob}, OutputChanged, 0, 0}, {JobRef{0, PlaybookJob}, OutputChanged, 0, 0},
		{JobRef{1, PlaybookJob}, OutputAll, 0, 0}, {JobRef{1, PlaybookJob}, OutputFailed, -1, 0}, {JobRef{1, PlaybookJob}, OutputFailed, 1, 2},
	} {
		if _, err := c.TaskOutput(context.Background(), args.ref, args.filter, args.before, args.after); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

func TestTaskOutputSessionPollingRetryAndCompletion(t *testing.T) {
	var phase atomic.Int32
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		p := phase.Load()
		if strings.HasSuffix(r.URL.Path, "job_events/") {
			switch p {
			case 1:
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(429)
				return
			case 4:
				w.WriteHeader(403)
				return
			}
			if p == 0 {
				writeTaskEvents(t, w, []any{})
				return
			}
			writeTaskEvents(t, w, []any{taskEvent(int(p), true, false, "live result\n")})
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "status": "successful", "event_processing_finished": p >= 3}); err != nil {
			t.Error(err)
		}
	})
	s := c.NewTaskOutputSession(context.Background(), JobRef{1, PlaybookJob}, OutputChanged)
	defer s.Close()
	var delays []time.Duration
	s.wait = func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return ctx.Err() }
	u, err := s.Next()
	if err != nil || u.Page.Text != "" || u.Complete {
		t.Fatal(u, err)
	}
	phase.Store(1)
	u, err = s.Next()
	if err != nil || !u.Retrying {
		t.Fatal(u, err)
	}
	phase.Store(2)
	u, err = s.Next()
	if err != nil || u.Complete || u.Page.Last != 2 || delays[2] != 7*time.Second {
		t.Fatal(u, err, delays)
	}
	phase.Store(3)
	u, err = s.Next()
	if err != nil || !u.Complete || u.Page.Last != 3 {
		t.Fatal(u, err)
	}
	phase.Store(4)
	u, err = s.Next()
	if err == nil || u.Retrying {
		t.Fatal("permission failure retried", u, err)
	}
	s.Stop()
	if _, err = s.Next(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = s.Read(context.Background(), 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestTaskOutputSessionStopCancelsRequest(t *testing.T) {
	started := make(chan struct{})
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "job_events/") {
			close(started)
			<-r.Context().Done()
			return
		}
		if _, err := w.Write([]byte(`{"id":1,"type":"job","status":"running"}`)); err != nil {
			t.Error(err)
		}
	})
	s := c.NewTaskOutputSession(context.Background(), JobRef{1, PlaybookJob}, OutputFailed)
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := s.Next(); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	s.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request not canceled")
	}
}

func TestTaskOutputSessionUnknownCompletionAndPermanentErrors(t *testing.T) {
	for _, code := range []int{200, 400, 401, 403, 404, 405} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "job_events/") {
					if code != 200 {
						w.WriteHeader(code)
						return
					}
					writeTaskEvents(t, w, []any{})
					return
				}
				if _, err := w.Write([]byte(`{"id":1,"type":"job","status":"successful"}`)); err != nil {
					t.Error(err)
				}
			})
			s := c.NewTaskOutputSession(context.Background(), JobRef{1, PlaybookJob}, OutputFailed)
			defer s.Close()
			u, err := s.Next()
			if code == 200 {
				if err != nil || u.CompletionKnown || u.Complete {
					t.Fatal(u, err)
				}
			} else if err == nil || u.Retrying {
				t.Fatal("permanent error retried", u, err)
			}
		})
	}
}

func TestTaskOutputStopCancelsPollingWait(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("request during polling wait") })
	s := c.NewTaskOutputSession(context.Background(), JobRef{1, PlaybookJob}, OutputChanged)
	s.delay = time.Hour
	done := make(chan error, 1)
	go func() { _, err := s.Next(); done <- err }()
	s.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("polling wait not canceled")
	}
}
