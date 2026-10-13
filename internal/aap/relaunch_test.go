package aap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"aaptui/internal/config"
)

// Fixtures model the pinned upstream contract; none were captured from AAP.
func TestRelaunchBothPrefixesAndTypes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Direct, config.Gateway} {
		for _, kind := range []JobType{PlaybookJob, WorkflowJob, AdHocCommand} {
			t.Run(string(mode)+"/"+string(kind), func(t *testing.T) {
				prefix := "/api/v2/"
				if mode == config.Gateway {
					prefix = "/api/controller/v2/"
				}
				route := prefix + jobCollection(kind) + "/1/"
				var posts, checks atomic.Int32
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("missing authentication")
					}
					var response any
					switch r.URL.Path {
					case route:
						response = map[string]any{"id": 1, "type": kind, "status": "successful", "name": "secret\x1b]52;c;bad\a deployment", "related": map[string]string{"relaunch": route + "relaunch/"}, "summary_fields": map[string]any{"user_capabilities": map[string]bool{"start": true}}}
					case route + "relaunch/":
						if r.Method == http.MethodPost {
							posts.Add(1)
							body, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error(err)
							}
							if string(body) != "{}" || r.Header.Get("Content-Type") != "application/json" {
								t.Errorf("unexpected POST: %s", body)
							}
							w.Header().Set("Location", "https://untrusted.example/api/v2/jobs/9/")
							w.WriteHeader(201)
							response = map[string]any{"id": 2, "type": kind}
						} else {
							checks.Add(1)
							response = map[string]any{}
							if kind != WorkflowJob {
								response = map[string]any{"passwords_needed_to_start": []string{}}
							}
						}
					default:
						t.Errorf("unexpected request: %s", r.URL.Path)
						w.WriteHeader(404)
						return
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				})
				ref := JobRef{ID: 1, Type: kind}
				d, err := c.Job(context.Background(), ref)
				if err != nil || d.Capabilities.Relaunch.State != Available || d.Name != "[redacted] deployment" {
					t.Fatal(d, err)
				}
				result, err := c.Relaunch(context.Background(), ref)
				if err != nil || !result.Requested || result.Ambiguous || result.Job.Ref != (JobRef{ID: 2, Type: kind}) || result.Source.Ref != ref || posts.Load() != 1 || checks.Load() != 2 {
					t.Fatal(result, err, posts.Load(), checks.Load())
				}
			})
		}
	}
}

func TestRelaunchUnavailableAndUnknownCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       JobType
		permission any
		link       bool
		code       int
		body       string
		state      CapabilityState
		reason     string
	}{
		{"denied", PlaybookJob, false, true, 200, `{"passwords_needed_to_start":[]}`, Unavailable, "permission"},
		{"permission absent", PlaybookJob, nil, true, 200, `{"passwords_needed_to_start":[]}`, Unknown, "permission"},
		{"no endpoint", PlaybookJob, true, false, 200, `{}`, Unavailable, "not exposed"},
		{"passwords", PlaybookJob, true, true, 200, `{"passwords_needed_to_start":["secret"]}`, Unavailable, "credential passwords"},
		{"ad hoc passwords", AdHocCommand, true, true, 200, `{"passwords_needed_to_start":["secret"]}`, Unavailable, "credential passwords"},
		{"requirements absent", PlaybookJob, true, true, 200, `{}`, Unknown, "requirements"},
		{"requirements null", AdHocCommand, true, true, 200, `{"passwords_needed_to_start":null}`, Unknown, "requirements"},
		{"malformed requirements", PlaybookJob, true, true, 200, `{"passwords_needed_to_start":"secret"}`, Unknown, "malformed"},
		{"endpoint denied", WorkflowJob, true, true, 403, `secret`, Unavailable, "permission"},
		{"endpoint missing", PlaybookJob, true, true, 404, `secret`, Unavailable, "missing"},
		{"endpoint unsupported", PlaybookJob, true, true, 405, `secret`, Unavailable, "unsupported"},
		{"endpoint down", PlaybookJob, true, true, 503, `secret`, Unknown, "temporary"},
		{"project update", ProjectUpdate, true, true, 200, `{}`, Unavailable, "job type"},
		{"inventory update", InventoryUpdate, true, true, 200, `{}`, Unavailable, "job type"},
		{"system job", SystemJob, true, true, 200, `{}`, Unavailable, "job type"},
		{"approval", WorkflowApproval, true, true, 200, `{}`, Unavailable, "job type"},
		{"unknown", "future_job", true, true, 200, `{}`, Unavailable, "job type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			route := "/api/v2/" + jobCollection(tc.kind) + "/1/"
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == route+"relaunch/" {
					w.WriteHeader(tc.code)
					if _, err := w.Write([]byte(tc.body)); err != nil {
						t.Error(err)
					}
					return
				}
				related := map[string]string{}
				if tc.link {
					related["relaunch"] = route + "relaunch/"
				}
				response := map[string]any{"id": 1, "type": tc.kind, "related": related, "summary_fields": map[string]any{"user_capabilities": map[string]any{"start": tc.permission}}}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			})
			ref := JobRef{ID: 1, Type: tc.kind}
			d, err := c.Job(context.Background(), ref)
			if err != nil || d.Capabilities.Relaunch.State != tc.state || !strings.Contains(d.Capabilities.Relaunch.Reason, tc.reason) || strings.Contains(d.Capabilities.Relaunch.Reason, "secret") {
				t.Fatal(d.Capabilities.Relaunch, err)
			}
			result, err := c.Relaunch(context.Background(), ref)
			if err == nil || result.Requested || result.Ambiguous || posts.Load() != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatal(result, err, posts.Load())
			}
		})
	}
}

func TestRelaunchPostFailuresNeverRetry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		body      string
		kind      ErrorKind
		ambiguous bool
	}{
		{"denied", 403, `secret`, Permission, false},
		{"input changed", 400, `secret`, Rejected, false},
		{"missing", 404, `secret`, Missing, false},
		{"unsupported", 405, `secret`, Unsupported, false},
		{"authentication", 401, `secret`, Authentication, false},
		{"server failure", 503, `secret`, Connection, true},
		{"rate limited", 429, `secret`, Connection, true},
		{"malformed JSON", 201, `secret`, Connection, true},
		{"missing identity", 201, `{}`, Malformed, true},
		{"source returned", 201, `{"id":1,"type":"job"}`, Malformed, true},
		{"wrong type", 201, `{"id":2,"type":"workflow_job"}`, Malformed, true},
		{"legacy mismatch", 201, `{"id":2,"type":"job","job":3}`, Malformed, true},
		{"negative ID", 201, `{"id":-2,"type":"job"}`, Malformed, true},
		{"transport loss", 0, ``, Connection, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					if tc.code == 0 {
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
					w.WriteHeader(tc.code)
					if _, err := w.Write([]byte(tc.body)); err != nil {
						t.Error(err)
					}
					return
				}
				body := `{"id":1,"type":"job","related":{"relaunch":"/api/v2/jobs/1/relaunch/"},"summary_fields":{"user_capabilities":{"start":true}}}`
				if strings.HasSuffix(r.URL.Path, "relaunch/") {
					body = `{"passwords_needed_to_start":[]}`
				}
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			})
			result, err := c.Relaunch(context.Background(), JobRef{ID: 1, Type: PlaybookJob})
			var api *APIError
			if !errors.As(err, &api) || api.Kind != tc.kind || result.Ambiguous != tc.ambiguous || posts.Load() != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal(result, err, posts.Load())
			}
			if tc.ambiguous && !strings.Contains(err.Error(), "refresh Jobs before retrying") {
				t.Fatal("missing reconciliation guidance", err)
			}
		})
	}
}

func TestRelaunchRechecksPermissionsAndInputs(t *testing.T) {
	for _, change := range []string{"permission", "passwords"} {
		t.Run(change, func(t *testing.T) {
			var reads, posts atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					w.WriteHeader(500)
					return
				}
				var response any
				if strings.HasSuffix(r.URL.Path, "relaunch/") {
					passwords := []string{}
					if change == "passwords" && reads.Load() > 1 {
						passwords = []string{"secret"}
					}
					response = map[string]any{"passwords_needed_to_start": passwords}
				} else {
					reads.Add(1)
					response = map[string]any{"id": 1, "type": "job", "related": map[string]string{"relaunch": "/api/v2/jobs/1/relaunch/"}, "summary_fields": map[string]any{"user_capabilities": map[string]bool{"start": !(change == "permission" && reads.Load() > 1)}}}
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			})
			ref := JobRef{ID: 1, Type: PlaybookJob}
			d, err := c.Job(context.Background(), ref)
			if err != nil || d.Capabilities.Relaunch.State != Available {
				t.Fatal(d, err)
			}
			result, err := c.Relaunch(context.Background(), ref)
			if err == nil || result.Source.Capabilities.Relaunch.State != Unavailable || posts.Load() != 0 {
				t.Fatal(result, err, posts.Load())
			}
		})
	}
}

func TestRelaunchValidatesLinksAndCancellation(t *testing.T) {
	for _, link := range []string{"https://evil.example/api/v2/jobs/1/relaunch/", "/api/v2/jobs/2/relaunch/", "/api/controller/v2/jobs/1/relaunch/", "/api/v2/jobs/1/relaunch/?hosts=failed", "/api/v2/jobs/1/relaunch/?", "/api/v2/jobs/1/relaunch/#x"} {
		t.Run(link, func(t *testing.T) {
			var requests atomic.Int32
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if err := json.NewEncoder(w).Encode(map[string]any{"id": 1, "type": "job", "related": map[string]string{"relaunch": link}}); err != nil {
					t.Error(err)
				}
			})
			_, err := c.Relaunch(context.Background(), JobRef{ID: 1, Type: PlaybookJob})
			if err == nil || requests.Load() != 1 {
				t.Fatal("invalid link followed", err, requests.Load())
			}
		})
	}
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled request reached server") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Relaunch(ctx, JobRef{ID: 1, Type: PlaybookJob}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
