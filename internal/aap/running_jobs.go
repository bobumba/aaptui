package aap

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// RunningJobsCount reads the server's filtered total across visible job types.
func (c *Client) RunningJobsCount(ctx context.Context) (int, error) {
	var page wirePage[wireJob]
	if err := c.request(ctx, http.MethodGet, c.endpoint("unified_jobs/")+"?status=running&page_size=1", "count running jobs", &page); err != nil {
		return 0, err
	}
	if page.Count == nil || page.Results == nil || *page.Count < len(*page.Results) || len(*page.Results) > 1 || (*page.Count > 0 && len(*page.Results) == 0) {
		return 0, apiError(Malformed, "count running jobs")
	}
	for _, job := range *page.Results {
		if job.ID <= 0 || !validType(job.Type) || job.Status != "running" {
			return 0, apiError(Malformed, "count running jobs")
		}
	}
	return *page.Count, nil
}

type RunningJobsUpdate struct {
	Count int
	Err   error
}

// RunningJobsPoller owns cancellable refresh/backoff timing. Call Next serially;
// cancelling its context stops waits and requests. Construction performs no IO.
type RunningJobsPoller struct {
	ctx            context.Context
	client         *Client
	delay, backoff time.Duration
	wait           func(context.Context, time.Duration) error
}

func (c *Client) NewRunningJobsPoller(ctx context.Context) *RunningJobsPoller {
	return &RunningJobsPoller{ctx: ctx, client: c, wait: waitContext}
}

// Next returns recoverable failures in update.Err. Other errors stop polling.
func (p *RunningJobsPoller) Next() (RunningJobsUpdate, error) {
	if err := p.wait(p.ctx, p.delay); err != nil {
		return RunningJobsUpdate{}, err
	}
	count, err := p.client.RunningJobsCount(p.ctx)
	if err != nil {
		var api *APIError
		if (errors.As(err, &api) && (api.Kind == Connection || api.Kind == Temporary)) || (errors.Is(err, context.DeadlineExceeded) && p.ctx.Err() == nil) {
			p.backoff = min(max(5*time.Second, p.backoff*2), 30*time.Second)
			p.delay = p.backoff
			if api != nil {
				p.delay = max(p.delay, api.RetryAfter)
			}
			return RunningJobsUpdate{Err: err}, nil
		}
		return RunningJobsUpdate{}, err
	}
	p.backoff = 0
	p.delay = 5 * time.Second
	return RunningJobsUpdate{Count: count}, nil
}
