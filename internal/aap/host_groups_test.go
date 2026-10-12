package aap

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"aaptui/internal/config"
)

// Synthetic fixtures model HostAllGroupsList at the revision in API_CONTRACT.md.
func TestHostGroupsEmptyAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		kind   ErrorKind
	}{
		{"empty", 200, `{"count":0,"results":[]}`, ""},
		{"permission", 403, "secret", Permission},
		{"missing", 404, "secret", Missing},
		{"unsupported", 405, "secret", Unsupported},
		{"authentication", 401, "secret", Authentication},
		{"temporary", 503, "secret", Temporary},
		{"malformed", 200, `{"count":1,"results":[{"id":1,"type":"host","inventory":7}]}`, Malformed},
		{"invalid id", 200, `{"count":1,"results":[{"id":0,"type":"group","inventory":7}]}`, Malformed},
		{"invalid inventory", 200, `{"count":1,"results":[{"id":1,"type":"group"}]}`, Malformed},
		{"foreign next", 200, `{"count":0,"results":[],"next":"https://evil.example/api/v2/hosts/11/all_groups/"}`, Malformed},
		{"wrong host", 200, `{"count":0,"results":[],"next":"/api/v2/hosts/12/all_groups/?page=2"}`, Malformed},
		{"direct groups cursor", 200, `{"count":0,"results":[],"previous":"/api/v2/hosts/11/groups/"}`, Malformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v2/hosts/11/all_groups/" {
					t.Error("wrong membership endpoint")
				}
				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Error(err)
				}
			})
			page, err := c.HostGroups(context.Background(), 11, ListOptions{})
			if tc.kind == "" {
				if err != nil || page.Count != 0 || len(page.Items) != 0 {
					t.Fatal(page, err)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Kind != tc.kind || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
}

func TestHostGroupsCanceled(t *testing.T) {
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled read reached server") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.HostGroups(ctx, 11, ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
