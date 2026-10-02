package aap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"aaptui/internal/config"
)

func TestOutputFormatsAndRanges(t *testing.T) {
	for _, test := range []struct {
		body  string
		valid bool
	}{{`{"range":{"start":0,"end":2,"absolute_end":2},"content":"one\npartial"}`, true}, {`{"range":{"start":0,"end":1,"absolute_end":1},"content":"<html>literal</html>"}`, true}, {`{"range":{"start":0,"end":2,"absolute_end":2},"content":"one"}`, false}, {`{"range":{"start":1,"end":2,"absolute_end":2},"content":"one\n"}`, false}, {`{"range":{"start":0,"end":1,"absolute_end":1},"content":"Standard Output too large to display (900 bytes), only download supported"}`, false}, {`{"content":"no range"}`, false}, {`{"range":{"start":0,"end":0,"absolute_end":0},"content":""}`, true}} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "stdout/") {
				if r.URL.Query().Get("format") != "json" || r.URL.Query().Get("start_line") != "0" || r.URL.Query().Get("end_line") != "2" {
					t.Error(r.URL)
				}
				if _, err := w.Write([]byte(test.body)); err != nil {
					t.Error(err)
				}
				return
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "status": "running", "related": map[string]string{"stdout": "/api/v2/jobs/1/stdout/"}}); err != nil {
				t.Error(err)
			}
		})
		_, err := c.Output(context.Background(), JobRef{1, PlaybookJob}, 0, 2)
		if (err == nil) != test.valid {
			t.Fatalf("body %s err %v", test.body, err)
		}
	}
}
func TestOutputReset(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		body := `{"id":1,"type":"job","related":{"stdout":"/api/v2/jobs/1/stdout/"}}`
		if strings.HasSuffix(r.URL.Path, "stdout/") {
			body = `{"range":{"start":5,"end":1,"absolute_end":1},"content":""}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	})
	_, err := c.Output(context.Background(), JobRef{1, PlaybookJob}, 5, 6)
	var api *APIError
	if !errors.As(err, &api) || api.Kind != History {
		t.Fatal(err)
	}
}
func TestSystemInlineOutput(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"id":6,"type":"system_job","status":"successful","event_processing_finished":true,"result_stdout":"one\nsecret two\nlast"}`)); err != nil {
			t.Error(err)
		}
	})
	d, err := c.Job(context.Background(), JobRef{6, SystemJob})
	if err != nil || d.Capabilities.Output.State != Available {
		t.Fatal(d, err)
	}
	chunk, err := c.Output(context.Background(), JobRef{6, SystemJob}, 1, 3)
	if err != nil || chunk.Start != 1 || chunk.End != 3 || chunk.AbsoluteEnd != 3 || chunk.Text != "secret two\nlast" {
		t.Fatal(chunk, err)
	}
}
