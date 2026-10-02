package aap

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aaptui/internal/config"
)

func testClient(t *testing.T, mode config.ConnectionMode, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	c, err := NewClient(config.Settings{URL: s.URL, Mode: mode, TLSVerify: true}, "secret", s.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}
func TestTransport(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				prefix := "/api/controller/v2/"
				if mode == config.Direct {
					prefix = "/api/v2/"
				}
				if r.URL.Path != prefix+"unified_jobs/" || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("search") != "hello world" {
					t.Error("unexpected request", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(`{"count":0,"results":[],"next":null,"previous":null}`)); err != nil {
					t.Error(err)
				}
			})
			p, err := readPage[struct{}](context.Background(), c, "unified_jobs/", ListOptions{Search: "hello world"})
			if err != nil || p.Count != 0 {
				t.Fatal(p, err)
			}
		})
	}
}
func TestErrorsAndBounds(t *testing.T) {
	for _, test := range []struct {
		code int
		body string
		kind ErrorKind
	}{{401, "secret", Authentication}, {403, "secret", Permission}, {404, "", Missing}, {405, "", Unsupported}, {429, "", Temporary}, {503, "", Temporary}, {302, "", Malformed}, {200, "no secret json", Malformed}, {200, "null", Malformed}, {200, strings.Repeat("x", maxBody+1), Malformed}, {200, `{"count":0}`, Malformed}, {200, `{"results":[],"next":"https://evil/api/v2/unified_jobs/"}`, Malformed}} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.code)
			if _, err := w.Write([]byte(test.body)); err != nil {
				t.Error(err)
			}
		})
		_, err := readPage[struct{}](context.Background(), c, "unified_jobs/", ListOptions{})
		var e *APIError
		if !errors.As(err, &e) || e.Kind != test.kind || strings.Contains(err.Error(), "secret") {
			t.Fatalf("code %d error %v", test.code, err)
		}
	}
}
func TestLinksRedirects(t *testing.T) {
	hits := 0
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer evil.Close()
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, evil.URL+"/api/v2/jobs/", 302) })
	if err := c.request(context.Background(), "GET", c.endpoint("jobs/"), "test", nil); err == nil || hits != 0 {
		t.Fatal("redirect followed", err)
	}
	for _, l := range []string{evil.URL + "/api/v2/jobs/", "/api/gateway/v1/tokens/", "/api/v2/../jobs/", "/api/v2/jobs/%2f/", "/api/v2/jobs/#x", "https://u:p@host/api/v2/jobs/"} {
		if _, err := c.safeLink(l); err == nil {
			t.Errorf("accepted %s", l)
		}
	}
}
func TestDeadlineAndTLS(t *testing.T) {
	c, s := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c.timeout = time.Millisecond
	err := c.request(context.Background(), "GET", c.endpoint("jobs/"), "test", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	secure, err := NewClient(config.Settings{URL: s.URL, Mode: config.Direct, TLSVerify: true}, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := secure.http.Transport.(*http.Transport)
	if tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("unsafe TLS defaults")
	}
	if err = secure.request(context.Background(), "GET", secure.endpoint("jobs/"), "test", nil); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}
func TestCleanText(t *testing.T) {
	c := &Client{token: "secret"}
	got := c.CleanText("secret\x1b[31mred\x1b[0m\x1b]52;c;clipboard\a\x00\r\n")
	if got != "[redacted]red\n" {
		t.Fatalf("%q", got)
	}
}
func TestCursorPageBound(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_size") != "50" {
			t.Error("cursor lost page bound", r.URL)
		}
		if _, err := w.Write([]byte(`{"count":0,"results":[]}`)); err != nil {
			t.Error(err)
		}
	})
	if _, err := readPage[struct{}](context.Background(), c, "unified_jobs/", ListOptions{Cursor: PageCursor{link: "/api/v2/unified_jobs/?page=2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := readPage[struct{}](context.Background(), c, "unified_jobs/", ListOptions{Cursor: PageCursor{link: "/api/v2/unified_jobs/?page_size=100000"}}); err == nil {
		t.Fatal("unbounded server cursor accepted")
	}
}
