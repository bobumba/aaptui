package aap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aaptui/internal/config"
)

func TestFollowRetryOverlapDelayedFinalOutput(t *testing.T) {
	var phase atomic.Int32
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		p := phase.Load()
		if p == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			return
		}
		if strings.HasSuffix(r.URL.Path, "stdout/") {
			all := []string{"one\n", "part"}
			if p >= 2 {
				all = []string{"one\n", "partial\n", "final\n"}
			}
			start, err := strconv.Atoi(r.URL.Query().Get("start_line"))
			if err != nil {
				t.Error(err)
			}
			end, err := strconv.Atoi(r.URL.Query().Get("end_line"))
			if err != nil {
				t.Error(err)
			}
			end = min(end, len(all))
			if err := json.NewEncoder(w).Encode(map[string]any{"range": map[string]int{"start": start, "end": end, "absolute_end": len(all)}, "content": strings.Join(all[start:end], "")}); err != nil {
				t.Error(err)
			}
			return
		}
		status := "running"
		if p >= 2 {
			status = "successful"
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "status": status, "event_processing_finished": p >= 3, "related": map[string]string{"stdout": "/api/v2/jobs/1/stdout/"}}); err != nil {
			t.Error(err)
		}
	})
	session := c.NewOutputSession(context.Background(), JobRef{1, PlaybookJob})
	store := testStore(t, DefaultOutputLimits())
	session.factory = func() (*OutputStore, error) { return store, nil }
	defer session.Close()
	var waits []time.Duration
	session.wait = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return ctx.Err() }
	u, err := session.Next(10)
	if err != nil || u.Cursor.Line != 2 || u.Chunk.Text != "one\npart" || u.Complete {
		t.Fatal(u, err)
	}
	phase.Store(1)
	u, err = session.Next(10)
	if err != nil || !u.Retrying || u.Cursor.Line != 2 {
		t.Fatal(u, err)
	}
	phase.Store(2)
	u, err = session.Next(10)
	if err != nil || u.Chunk.Text != "one\npartial\nfinal\n" || u.Complete || waits[2] != 7*time.Second {
		t.Fatal(u, err, waits)
	}
	phase.Store(3)
	u, err = session.Next(10)
	if err != nil || !u.Complete || u.Cursor.Line != 3 {
		t.Fatal(u, err)
	}
	files := len(store.files)
	u, err = session.Next(10)
	if err != nil || len(store.files) != files {
		t.Fatal("unchanged output rewrote cache", u, err)
	}
}
func TestFollowUnknownCompletionAndAuthStop(t *testing.T) {
	var code atomic.Int32
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if n := code.Load(); n != 0 {
			w.WriteHeader(int(n))
			return
		}
		body := `{"id":1,"type":"job","status":"successful","related":{"stdout":"/api/v2/jobs/1/stdout/"}}`
		if strings.HasSuffix(r.URL.Path, "stdout/") {
			body = `{"range":{"start":0,"end":0,"absolute_end":0},"content":""}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	})
	s := c.NewOutputSession(context.Background(), JobRef{1, PlaybookJob})
	s.factory = func() (*OutputStore, error) { return testStore(t, DefaultOutputLimits()), nil }
	s.wait = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	defer s.Close()
	u, err := s.Next(10)
	if err != nil || u.Complete || u.CompletionKnown {
		t.Fatal(u, err)
	}
	code.Store(401)
	u, err = s.Next(10)
	if err == nil || u.Retrying {
		t.Fatal("authentication retried", u, err)
	}
}
func TestFollowCancellationDuringRetryWait(t *testing.T) {
	c := &Client{token: "secret"}
	s := c.NewOutputSession(context.Background(), JobRef{1, PlaybookJob})
	entered := make(chan struct{})
	s.delay = time.Hour
	s.wait = func(ctx context.Context, d time.Duration) error { close(entered); <-ctx.Done(); return ctx.Err() }
	done := make(chan error, 1)
	go func() { _, err := s.Next(10); done <- err }()
	<-entered
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestFollowBackoffCap(t *testing.T) {
	s := &OutputSession{ctx: context.Background(), store: testStore(t, DefaultOutputLimits())}
	for i := 0; i < 12; i++ {
		u, err := s.failed(apiError(Connection, "follow"))
		if err != nil || !u.Retrying || s.delay > 30*time.Second {
			t.Fatal(u, err, s.delay)
		}
	}
}
func TestFinalOutputCatchUpAcrossBoundedChunks(t *testing.T) {
	var fetches atomic.Int32
	c, _ := testClient(t, config.Gateway, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "stdout/") {
			fetches.Add(1)
			start, err := strconv.Atoi(r.URL.Query().Get("start_line"))
			if err != nil {
				t.Error(err)
			}
			end, err := strconv.Atoi(r.URL.Query().Get("end_line"))
			if err != nil {
				t.Error(err)
			}
			end = min(end, 600)
			if err := json.NewEncoder(w).Encode(map[string]any{"range": map[string]int{"start": start, "end": end, "absolute_end": 600}, "content": strings.Repeat("line\n", end-start)}); err != nil {
				t.Error(err)
			}
			return
		}
		if _, err := w.Write([]byte(`{"id":1,"type":"job","status":"successful","event_processing_finished":true,"related":{"stdout":"/api/controller/v2/jobs/1/stdout/"}}`)); err != nil {
			t.Error(err)
		}
	})
	s := c.NewOutputSession(context.Background(), JobRef{1, PlaybookJob})
	s.factory = func() (*OutputStore, error) { return testStore(t, DefaultOutputLimits()), nil }
	s.wait = func(ctx context.Context, d time.Duration) error {
		if d != 0 {
			t.Error("catch-up waited", d)
		}
		return ctx.Err()
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		u, err := s.Next(10)
		if err != nil || u.Complete != (i == 2) {
			t.Fatal(u, err)
		}
	}
	if s.store.Cursor().Line != 600 || fetches.Load() != 3 {
		t.Fatal("output truncated")
	}
}
func TestSessionReloadsEvictedHistoryWithoutMovingCursor(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		body := `{"id":1,"type":"job","status":"running","related":{"stdout":"/api/v2/jobs/1/stdout/"}}`
		if strings.HasSuffix(r.URL.Path, "stdout/") {
			body = `{"range":{"start":0,"end":1,"absolute_end":3},"content":"secret older\n"}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	})
	store := testStore(t, OutputLimits{MemoryBytes: 1, DiskBytes: 1024, MaxLines: 2, MaxFiles: 2})
	for i := 0; i < 3; i++ {
		if err := store.Accept(OutputChunk{i, i + 1, i + 1, "line\n"}); err != nil {
			t.Fatal(err)
		}
	}
	s := c.NewOutputSession(context.Background(), JobRef{1, PlaybookJob})
	s.factory = func() (*OutputStore, error) { return store, nil }
	defer s.Close()
	u, err := s.Read(context.Background(), 0, 1)
	if err != nil || u.Cursor.Line != 3 || u.Chunk.Text != "[redacted] older\n" {
		t.Fatal(u, err)
	}
}
