package aap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"aaptui/internal/config"
)

// Synthetic fixtures based on the pinned upstream Me collection contract.
func TestMe(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != prefix+"me/" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("unexpected identity request", r.URL.Path)
				}
				if _, err := fmt.Fprint(w, `{"count":1,"results":[{"id":4,"username":"admin\u001b[31msecret\u001b[0m\n"}]}`); err != nil {
					t.Error(err)
				}
			})
			user, err := c.Me(context.Background())
			if err != nil || user.ID != 4 || user.Username != "admin[redacted] " {
				t.Fatal(user, err)
			}
		})
	}
}

func TestMeFailures(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
		kind ErrorKind
	}{
		{`{"count":0,"results":[]}`, 200, Malformed},
		{`{"count":2,"results":[{"id":1,"username":"a"}]}`, 200, Malformed},
		{`{"count":1,"results":[{"id":0,"username":"a"}]}`, 200, Malformed},
		{`{"count":1,"results":[{"id":1,"username":"\u001b[31m"}]}`, 200, Malformed},
		{`{"count":1,"results":[{"id":1}]}`, 200, Malformed},
		{`secret`, 401, Authentication}, {`secret`, 403, Permission}, {`secret`, 503, Temporary},
	} {
		c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			if _, err := fmt.Fprint(w, tc.body); err != nil {
				t.Error(err)
			}
		})
		_, err := c.Me(context.Background())
		var api *APIError
		if !errors.As(err, &api) || api.Kind != tc.kind || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %v", tc.body, err)
		}
	}
}
