package aap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"aaptui/internal/config"
)

// Synthetic filtered page envelopes, not captured AAP responses.
func TestRunningJobsCountBothPrefixes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			for _, jobType := range []JobType{PlaybookJob, WorkflowJob, ProjectUpdate, InventoryUpdate, AdHocCommand, SystemJob, WorkflowApproval, "future_job"} {
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != prefix+"unified_jobs/" || r.URL.Query().Get("status") != "running" || r.URL.Query().Get("page_size") != "1" || r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("incorrect count request", r.URL)
					}
					if _, err := fmt.Fprintf(w, `{"count":237,"results":[{"id":1,"type":%q,"status":"running"}]}`, jobType); err != nil {
						t.Error(err)
					}
				})
				count, err := c.RunningJobsCount(context.Background())
				if err != nil || count != 237 {
					t.Fatal(count, err)
				}
			}
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				if _, err := fmt.Fprint(w, `{"count":0,"results":[]}`); err != nil {
					t.Error(err)
				}
			})
			count, err := c.RunningJobsCount(context.Background())
			if err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestRunningJobsCountRejectsInvalidTotalsAndStatuses(t *testing.T) {
	for _, body := range []string{
		`{"results":[]}`, `{"count":0}`, `{"count":-1,"results":[]}`, `{"count":1,"results":[]}`,
		`{"count":1,"results":[{"id":1,"type":"job","status":"waiting"}]}`,
		`{"count":1,"results":[{"id":1,"type":"job","status":"pending"}]}`,
		`{"count":1,"results":[{"id":1,"type":"job","status":"successful"}]}`,
		`{"count":1,"results":[{"id":0,"type":"job","status":"running"}]}`,
		`{"count":1,"results":[{"id":1,"type":"","status":"running"}]}`,
		`{"count":2,"results":[{},{}]}`,
	} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			if _, err := fmt.Fprint(w, body); err != nil {
				t.Error(err)
			}
		})
		_, err := c.RunningJobsCount(context.Background())
		var api *APIError
		if !errors.As(err, &api) || api.Kind != Malformed {
			t.Fatal("accepted malformed count", body, err)
		}
	}
}

func TestRunningJobsPollingTimingAndRecovery(t *testing.T) {
	step := 0
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		step++
		if step >= 2 && step <= 5 {
			w.WriteHeader(503)
			return
		}
		if step == 7 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			return
		}
		if _, err := fmt.Fprint(w, `{"count":2,"results":[{"id":1,"type":"job","status":"running"}]}`); err != nil {
			t.Error(err)
		}
	})
	p := c.NewRunningJobsPoller(context.Background())
	var waits []time.Duration
	p.wait = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return ctx.Err() }
	for i := 0; i < 8; i++ {
		update, err := p.Next()
		if err != nil {
			t.Fatal(err)
		}
		if i >= 1 && i <= 4 || i == 6 {
			if update.Err == nil {
				t.Fatal("missing recoverable error")
			}
		} else if update.Err != nil || update.Count != 2 {
			t.Fatal(update)
		}
	}
	want := []time.Duration{0, 5 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, 5 * time.Second, 60 * time.Second}
	if !reflect.DeepEqual(waits, want) {
		t.Fatal("incorrect polling/backoff", waits)
	}
}

func TestRunningJobsPollingStopsOnPermanentFailure(t *testing.T) {
	for _, code := range []int{401, 403, 404, 200} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			if _, err := fmt.Fprint(w, "secret"); err != nil {
				t.Error(err)
			}
		})
		_, err := c.NewRunningJobsPoller(context.Background()).Next()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("permanent failure hidden", err)
		}
	}
}

func TestRunningJobsPollingRetriesRequestTimeout(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("expired request reached server") })
	c.timeout = 0
	p := c.NewRunningJobsPoller(context.Background())
	update, err := p.Next()
	if err != nil || !errors.Is(update.Err, context.DeadlineExceeded) || p.delay != 5*time.Second {
		t.Fatal("request timeout stopped polling", update, err)
	}
}

func TestRunningJobsPollingCancellation(t *testing.T) {
	t.Run("wait", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		p := (&Client{}).NewRunningJobsPoller(ctx)
		p.delay = 5 * time.Second
		entered := make(chan struct{})
		p.wait = func(ctx context.Context, _ time.Duration) error { close(entered); <-ctx.Done(); return ctx.Err() }
		done := make(chan error, 1)
		go func() { _, err := p.Next(); done <- err }()
		<-entered
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entered := make(chan struct{})
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() })
		done := make(chan error, 1)
		go func() { _, err := c.NewRunningJobsPoller(ctx).Next(); done <- err }()
		<-entered
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
