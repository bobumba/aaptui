package aap

import (
	"context"
	"errors"
	"sync"
	"time"
)

type TaskOutputUpdate struct {
	Page                                TaskOutputPage
	Status                              JobStatus
	Complete, CompletionKnown, Retrying bool
	Err                                 error
}

// TaskOutputSession polls matching results without accumulating job history.
// It owns no disk cache; stopping cancels reads and retry waits immediately.
type TaskOutputSession struct {
	client         *Client
	ref            JobRef
	filter         OutputFilter
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	delay, backoff time.Duration
	wait           func(context.Context, time.Duration) error
}

func (c *Client) NewTaskOutputSession(ctx context.Context, ref JobRef, filter OutputFilter) *TaskOutputSession {
	ctx, cancel := context.WithCancel(ctx)
	return &TaskOutputSession{client: c, ref: ref, filter: filter, ctx: ctx, cancel: cancel, wait: waitContext}
}
func (s *TaskOutputSession) Stop()        { s.cancel() }
func (s *TaskOutputSession) Close() error { s.Stop(); return nil }

func (s *TaskOutputSession) Read(ctx context.Context, before, after int) (TaskOutputPage, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	if s.ctx.Err() != nil {
		return TaskOutputPage{}, s.ctx.Err()
	}
	return s.client.TaskOutput(ctx, s.ref, s.filter, before, after)
}

func (s *TaskOutputSession) Next() (TaskOutputUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.wait(s.ctx, s.delay); err != nil {
		return TaskOutputUpdate{}, err
	}
	d, err := s.client.Job(s.ctx, s.ref)
	if err != nil {
		return s.failed(err)
	}
	page, err := s.Read(s.ctx, 0, 0)
	if err != nil {
		return s.failed(err)
	}
	s.backoff, s.delay = 0, 2*time.Second
	known := d.EventProcessingFinished != nil
	complete := d.Status.Terminal() && known && *d.EventProcessingFinished
	return TaskOutputUpdate{Page: page, Status: d.Status, Complete: complete, CompletionKnown: known}, nil
}

func (s *TaskOutputSession) failed(err error) (TaskOutputUpdate, error) {
	if s.ctx.Err() != nil {
		return TaskOutputUpdate{}, s.ctx.Err()
	}
	var api *APIError
	recoverable := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &api) && (api.Kind == Connection || api.Kind == Temporary))
	if !recoverable {
		return TaskOutputUpdate{}, err
	}
	if s.backoff == 0 {
		s.backoff = 2 * time.Second
	} else {
		s.backoff = min(2*s.backoff, 30*time.Second)
	}
	s.delay = s.backoff
	if api != nil {
		s.delay = max(s.delay, api.RetryAfter)
	}
	return TaskOutputUpdate{Err: err, Retrying: true}, nil
}
