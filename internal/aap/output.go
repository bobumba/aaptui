package aap

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const OutputLines = 256
const OutputBytes = 256 * 1024

type OutputCursor struct{ Line int }
type OutputChunk struct {
	Start, End, AbsoluteEnd int
	Text                    string
}
type OutputUpdate struct {
	Chunk           OutputChunk
	Cursor          OutputCursor
	Status          JobStatus
	Complete        bool
	CompletionKnown bool
	Err             error
	Retrying        bool
}
type wireOutput struct {
	Range *struct {
		Start       *int `json:"start"`
		End         *int `json:"end"`
		AbsoluteEnd *int `json:"absolute_end"`
	} `json:"range"`
	Content *string `json:"content"`
}

func (c *Client) Output(ctx context.Context, ref JobRef, start, end int) (OutputChunk, error) {
	if start < 0 || end <= start || end-start > OutputLines {
		return OutputChunk{}, apiError(Malformed, "read output range")
	}
	d, err := c.Job(ctx, ref)
	if err != nil {
		return OutputChunk{}, err
	}
	if d.Capabilities.Output.State != Available {
		return OutputChunk{}, apiError(Unsupported, "read output")
	}
	return c.output(ctx, d, start, end)
}
func (c *Client) output(ctx context.Context, d JobDetails, start, end int) (OutputChunk, error) {
	if d.Ref.Type == SystemJob && d.systemText != nil {
		text := *d.systemText
		if strings.HasPrefix(text, "Standard Output too large to display (") {
			return OutputChunk{}, apiError(History, "Server stdout display limit exceeded")
		}
		if len(text) > OutputBytes {
			return OutputChunk{}, apiError(History, "Inline system stdout exceeds client byte budget")
		}
		lines := outputLines(text)
		if start > len(lines) {
			return OutputChunk{}, apiError(History, "Output reset or requested history expired")
		}
		end = min(end, len(lines))
		return OutputChunk{start, end, len(lines), strings.Join(lines[start:end], "")}, nil
	}
	q := url.Values{"format": {"json"}, "start_line": {strconv.Itoa(start)}, "end_line": {strconv.Itoa(end)}}
	u, err := c.safeLink(d.stdout)
	if err != nil {
		return OutputChunk{}, err
	}
	u.RawQuery = q.Encode()
	var w wireOutput
	if err := c.request(ctx, http.MethodGet, u.String(), "read output", &w); err != nil {
		return OutputChunk{}, err
	}
	if w.Content == nil || w.Range == nil || w.Range.Start == nil || w.Range.End == nil || w.Range.AbsoluteEnd == nil {
		return OutputChunk{}, apiError(Malformed, "read output")
	}
	if strings.HasPrefix(*w.Content, "Standard Output too large to display (") {
		return OutputChunk{}, &APIError{Kind: History, Operation: "Server stdout display limit exceeded; download is required"}
	}
	chunk := OutputChunk{Start: *w.Range.Start, End: *w.Range.End, AbsoluteEnd: *w.Range.AbsoluteEnd, Text: *w.Content}
	// When querying past the retained end, upstream can return end < start.
	if chunk.AbsoluteEnd < start {
		return OutputChunk{}, apiError(History, "Output reset or requested history expired")
	}
	if chunk.Start != start || chunk.End > end || chunk.End < start || chunk.End > chunk.AbsoluteEnd || len(chunk.Text) > OutputBytes {
		return OutputChunk{}, apiError(Malformed, "read output range")
	}
	if err := validateChunk(chunk); err != nil {
		return OutputChunk{}, err
	}
	return chunk, nil
}
func validateChunk(c OutputChunk) error {
	if c.Start < 0 || c.End < c.Start || c.AbsoluteEnd < c.End || c.End-c.Start > OutputLines || len(c.Text) > OutputBytes {
		return apiError(Malformed, "validate output chunk")
	}
	if len(outputLines(c.Text)) != c.End-c.Start {
		return apiError(Malformed, "output range does not match content")
	}
	return nil
}
func outputLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
func (c OutputChunk) String() string { return fmt.Sprintf("output lines %d–%d", c.Start, c.End) }
