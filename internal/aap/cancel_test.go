package aap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"aaptui/internal/config"
)

func TestCancellation(t *testing.T) {
	for _, scenario := range []string{"success", "denied", "unsupported", "state changed", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			var posts atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "cancel/") {
					if scenario == "denied" {
						w.WriteHeader(403)
						return
					}
					if r.Method == http.MethodPost {
						posts.Add(1)
						if scenario == "ambiguous" {
							w.WriteHeader(503)
						} else {
							w.WriteHeader(202)
						}
						return
					}
					if err := json.NewEncoder(w).Encode(map[string]bool{"can_cancel": true}); err != nil {
						t.Error(err)
					}
					return
				}
				status := "running"
				if scenario == "state changed" || posts.Load() > 0 {
					status = "canceled"
				}
				related := map[string]string{"cancel": "/api/v2/jobs/1/cancel/"}
				if scenario == "unsupported" {
					related = nil
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "status": status, "related": related}); err != nil {
					t.Error(err)
				}
			})
			result, err := c.Cancel(context.Background(), JobRef{1, PlaybookJob})
			switch scenario {
			case "success":
				if err != nil || !result.Requested || result.Job.Status != "canceled" || posts.Load() != 1 {
					t.Fatal(result, err)
				}
			case "ambiguous":
				if err == nil || !result.Ambiguous || posts.Load() != 1 || result.Job.Status != "canceled" {
					t.Fatal(result, err)
				}
			default:
				if err == nil || posts.Load() != 0 {
					t.Fatal("unauthorized mutation", result, err)
				}
			}
		})
	}
}
func TestCancellationTransportLoss(t *testing.T) {
	var posts atomic.Int32
	c, _ := testClient(t, config.Gateway, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			if err := conn.Close(); err != nil {
				t.Error(err)
			}
			return
		}
		body := `{"id":1,"type":"job","status":"running","related":{"cancel":"/api/controller/v2/jobs/1/cancel/"}}`
		if strings.HasSuffix(r.URL.Path, "cancel/") {
			body = `{"can_cancel":true}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	})
	result, err := c.Cancel(context.Background(), JobRef{1, PlaybookJob})
	var api *APIError
	if !errors.As(err, &api) || !result.Ambiguous || posts.Load() != 1 {
		t.Fatal(result, err, posts.Load())
	}
}
func TestCancellationStateChangesBetweenCheckAndPost(t *testing.T) {
	var posts atomic.Int32
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "cancel/") {
			if r.Method == "POST" {
				posts.Add(1)
				w.WriteHeader(405)
				return
			}
			if _, err := w.Write([]byte(`{"can_cancel":true}`)); err != nil {
				t.Error(err)
			}
			return
		}
		status := "running"
		if posts.Load() > 0 {
			status = "successful"
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "status": status, "related": map[string]string{"cancel": "/api/v2/jobs/1/cancel/"}}); err != nil {
			t.Error(err)
		}
	})
	result, err := c.Cancel(context.Background(), JobRef{1, PlaybookJob})
	if err == nil || result.Requested || result.Job.Status != "successful" || posts.Load() != 1 {
		t.Fatal(result, err)
	}
}
