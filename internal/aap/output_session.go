package aap

import (
	"context"
	"errors"
	"sync"
	"time"
)

// OutputSession owns cache and cursor for one job; constructing it performs no IO.
// Read and Close serialize storage operations; Stop cancels requests immediately.
type OutputSession struct {
	delay, backoff time.Duration
	wait           func(context.Context, time.Duration) error
	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	client         *Client
	ref            JobRef
	store          *OutputStore
	closed         bool
	factory        func() (*OutputStore, error)
}

func (c *Client) NewOutputSession(ctx context.Context, ref JobRef) *OutputSession {
	ctx, cancel := context.WithCancel(ctx)
	return &OutputSession{ctx: ctx, cancel: cancel, client: c, ref: ref, wait: waitContext, factory: func() (*OutputStore, error) { return NewOutputStore(c.token, DefaultOutputLimits()) }}
}
func (s *OutputSession) Stop() { s.cancel() }
func (s *OutputSession) ensureStore() error {
	if s.closed {
		return apiError(Storage, "output session closed")
	}
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if s.store == nil {
		store, err := s.factory()
		if err != nil {
			return err
		}
		s.store = store
	}
	return nil
}
func (s *OutputSession) Read(ctx context.Context, start, end int) (OutputUpdate, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return OutputUpdate{}, ctx.Err()
	}
	if err := s.ensureStore(); err != nil {
		return OutputUpdate{}, err
	}
	if start < 0 || end <= start || end-start > OutputLines {
		return OutputUpdate{}, apiError(History, "invalid output viewport")
	}
	if text, err := s.store.Read(start, end); err == nil {
		return OutputUpdate{Chunk: OutputChunk{start, end, max(s.store.Cursor().Line, end), text}, Cursor: s.store.Cursor()}, nil
	} else {
		var api *APIError
		if !errors.As(err, &api) || api.Kind != History {
			return OutputUpdate{}, err
		}
	}
	chunk, err := s.client.Output(ctx, s.ref, start, end)
	if err != nil {
		return OutputUpdate{}, err
	}
	if err = s.store.Accept(chunk); err != nil {
		return OutputUpdate{}, err
	}
	text, err := s.store.Read(chunk.Start, chunk.End)
	if err != nil {
		return OutputUpdate{}, err
	}
	chunk.Text = text
	return OutputUpdate{Chunk: chunk, Cursor: s.store.Cursor()}, nil
}
func (s *OutputSession) Close() error {
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.store != nil {
		if err := s.store.Close(); err != nil {
			return err
		}
	}
	s.closed = true
	return nil
}

func waitContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Next waits in the API layer, then retrieves one bounded increment. Calling it
// sequentially creates backpressure: at most one update is pending, with no producer
// goroutine or unbounded message queue. History reads share the cache mutex.
func (s *OutputSession) Next(viewLines int) (OutputUpdate, error) {
	s.mu.Lock()
	delay := s.delay
	wait := s.wait
	s.mu.Unlock()
	if err := wait(s.ctx, delay); err != nil {
		return OutputUpdate{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureStore(); err != nil {
		return OutputUpdate{}, err
	}
	d, err := s.client.Job(s.ctx, s.ref)
	if err != nil {
		return s.failed(err)
	}
	if d.Capabilities.Output.State != Available {
		return OutputUpdate{}, apiError(Unsupported, "follow output")
	}
	cursor := s.store.Cursor()
	start := max(0, cursor.Line-1)
	chunk, err := s.client.output(s.ctx, d, start, start+OutputLines)
	if err != nil {
		return s.failed(err)
	}
	if chunk.AbsoluteEnd < cursor.Line {
		return OutputUpdate{}, apiError(History, "Output reset; previous output is retained")
	}
	if err = s.store.Accept(chunk); err != nil {
		return OutputUpdate{}, err
	}
	cursor = s.store.Cursor()
	viewStart := max(0, cursor.Line-min(max(viewLines, 1), OutputLines))
	text, err := s.store.Read(viewStart, cursor.Line)
	if err != nil {
		return OutputUpdate{}, err
	}
	s.backoff = 0
	s.delay = 2 * time.Second
	known := d.EventProcessingFinished != nil
	complete := d.Status.Terminal() && known && *d.EventProcessingFinished && cursor.Line >= chunk.AbsoluteEnd
	// Catch up immediately when the bounded range did not reach the retained end.
	if cursor.Line < chunk.AbsoluteEnd {
		s.delay = 0
	}
	return OutputUpdate{Chunk: OutputChunk{viewStart, cursor.Line, chunk.AbsoluteEnd, text}, Cursor: cursor, Status: d.Status, Complete: complete, CompletionKnown: known}, nil
}
func (s *OutputSession) failed(err error) (OutputUpdate, error) {
	if s.ctx.Err() != nil {
		return OutputUpdate{}, s.ctx.Err()
	}
	var api *APIError
	recoverable := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &api) && (api.Kind == Connection || api.Kind == Temporary))
	if !recoverable {
		return OutputUpdate{}, err
	}
	if s.backoff == 0 {
		s.backoff = 2 * time.Second
	} else {
		s.backoff = min(s.backoff*2, 30*time.Second)
	}
	s.delay = s.backoff
	if api != nil {
		s.delay = max(s.delay, api.RetryAfter)
	}
	return OutputUpdate{Cursor: s.store.Cursor(), Err: err, Retrying: true}, nil
}
