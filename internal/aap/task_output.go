package aap

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// OutputFilter selects structured task results, never words or colors in stdout.
type OutputFilter string

const (
	OutputAll        OutputFilter = "All"
	OutputChanged    OutputFilter = "Changed"
	OutputFailed     OutputFilter = "Failed"
	taskOutputEvents              = 8
)

// TaskOutputPage uses event counters independently of absolute stdout lines.
// Text is sanitized and bounded by OutputBytes; the caller scrolls within it.
type TaskOutputPage struct {
	Text         string
	First, Last  int
	Older, Newer bool
}

type wireTaskEvent struct {
	Counter int     `json:"counter"`
	Event   string  `json:"event"`
	Changed *bool   `json:"changed"`
	Failed  *bool   `json:"failed"`
	Task    string  `json:"task"`
	Host    string  `json:"host_name"`
	Stdout  *string `json:"stdout"`
	Data    struct {
		Task string `json:"task"`
	} `json:"event_data"`
}

// TaskOutput reads one bounded keyset page. Both cursors zero selects the tail.
func (c *Client) TaskOutput(ctx context.Context, ref JobRef, filter OutputFilter, before, after int) (TaskOutputPage, error) {
	route := ""
	switch ref.Type {
	case PlaybookJob:
		route = fmt.Sprintf("jobs/%d/job_events/", ref.ID)
	case AdHocCommand:
		route = fmt.Sprintf("ad_hoc_commands/%d/ad_hoc_command_events/", ref.ID)
	default:
		return TaskOutputPage{}, apiError(Unsupported, "Task filters are unavailable for this job type; press A for All")
	}
	if ref.ID <= 0 || before < 0 || after < 0 || (before != 0 && after != 0) {
		return TaskOutputPage{}, apiError(Malformed, "read task output")
	}
	q := url.Values{"limit": {strconv.Itoa(taskOutputEvents + 1)}, "order_by": {"-counter"}, "event__startswith": {"runner_"}, "no_truncate": {"1"}}
	switch filter {
	case OutputChanged:
		q.Set("changed", "true")
	case OutputFailed:
		q.Set("failed", "true")
	default:
		return TaskOutputPage{}, apiError(Malformed, "select task filter")
	}
	if before > 0 {
		q.Set("counter__lt", strconv.Itoa(before))
	}
	if after > 0 {
		q.Set("counter__gt", strconv.Itoa(after))
		q.Set("order_by", "counter")
	}
	var wire struct {
		Results *[]wireTaskEvent `json:"results"`
	}
	if err := c.request(ctx, http.MethodGet, c.endpoint(route)+"?"+q.Encode(), "Load task filter (press A for All)", &wire); err != nil {
		return TaskOutputPage{}, err
	}
	if wire.Results == nil || len(*wire.Results) > taskOutputEvents+1 {
		return TaskOutputPage{}, apiError(Malformed, "read task events")
	}
	events := *wire.Results
	previous := 0
	for _, event := range events {
		if event.Counter <= 0 || event.Stdout == nil || event.Changed == nil || event.Failed == nil || !strings.HasPrefix(event.Event, "runner_") ||
			(filter == OutputChanged && !*event.Changed) || (filter == OutputFailed && !*event.Failed) ||
			(before > 0 && event.Counter >= before) || (after > 0 && event.Counter <= after) ||
			(previous != 0 && ((after > 0 && event.Counter <= previous) || (after == 0 && event.Counter >= previous))) {
			return TaskOutputPage{}, apiError(Malformed, "validate filtered task events")
		}
		previous = event.Counter
	}
	more := len(events) > taskOutputEvents
	if more {
		events = events[:taskOutputEvents]
	}
	if after == 0 {
		slices.Reverse(events)
	}
	page := TaskOutputPage{}
	if len(events) == 0 {
		return page, nil
	}
	page.First, page.Last = events[0].Counter, events[len(events)-1].Counter
	page.Older, page.Newer = more, before > 0
	if after > 0 {
		page.Older, page.Newer = true, more
	}
	var text strings.Builder
	for _, event := range events {
		task := event.Task
		if task == "" {
			task = event.Data.Task
		}
		if task == "" {
			task = "Task result"
		}
		header := fmt.Sprintf("TASK [%s] · %s · event %d\n", c.cleanLabel(task), c.cleanLabel(event.Host), event.Counter)
		stdout := c.CleanText(*event.Stdout)
		if len(header)+len(stdout)+1 > OutputBytes-text.Len() {
			return TaskOutputPage{}, apiError(History, "Filtered task page exceeds the output byte budget; press A for All")
		}
		text.WriteString(header)
		text.WriteString(stdout)
		if !strings.HasSuffix(stdout, "\n") {
			text.WriteByte('\n')
		}
	}
	page.Text = text.String()
	if strings.Count(page.Text, "\n") > DefaultOutputLimits().MaxLines {
		return TaskOutputPage{}, apiError(History, "Filtered task page exceeds the output line budget; press A for All")
	}
	return page, nil
}
