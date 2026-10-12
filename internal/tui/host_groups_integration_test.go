package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

// Synthetic API verifies both prefixes, parent groups, and on-demand paging.
func TestHostGroupsIntegrationBothModes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			type request struct{ path, page string }
			requests := make(chan request, 10)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("wrong method or authentication")
				}
				requests <- request{r.URL.Path, r.URL.Query().Get("page")}
				var response any
				switch r.URL.Path {
				case prefix + "hosts/3/":
					response = map[string]any{"id": 3, "type": "host", "name": "web-1", "inventory": 1, "enabled": false}
				case prefix + "hosts/3/all_groups/":
					if r.URL.Query().Get("search") != "" {
						t.Error("host list search leaked into memberships")
					}
					if r.URL.Query().Get("page") == "2" {
						response = map[string]any{"count": 2, "results": []any{map[string]any{"id": 8, "type": "group", "inventory": 1, "name": "secret\x1b[31mparent"}}, "previous": prefix + "hosts/3/all_groups/?page=1"}
					} else {
						response = map[string]any{"count": 2, "results": []any{map[string]any{"id": 7, "type": "group", "inventory": 1, "name": "direct"}}, "next": prefix + "hosts/3/all_groups/?page=2"}
					}
				case prefix + "hosts/4/":
					response = map[string]any{"id": 4, "type": "host", "name": "ungrouped", "inventory": 1}
				case prefix + "hosts/4/all_groups/":
					response = map[string]any{"count": 0, "results": []any{}}
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := aap.NewClient(config.Settings{URL: server.URL, Mode: mode, TLSVerify: true}, "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			m := New(context.Background(), "").WithObjectDetails(client).WithHostGroups(client)
			cleanupModel(t, m)
			m.width, m.height = 110, 50
			m.screen, m.search = hostsScreen, "web"
			m.hostPage.Items = []aap.HostSummary{{ID: 3, Name: "web-1"}, {ID: 4, Name: "ungrouped"}}
			step := func(k string) {
				t.Helper()
				_, cmd := m.Update(key(k))
				runHostCommand(m, cmd)
				if m.err != nil || m.hostGroupsErr != nil {
					t.Fatal(m.err, m.hostGroupsErr)
				}
				assertFrameSize(t, m)
			}
			expect := func(path, page string) {
				t.Helper()
				if got := <-requests; got != (request{prefix + path, page}) {
					t.Fatalf("request = %+v", got)
				}
			}
			step("enter")
			expect("hosts/3/", "")
			expect("hosts/3/all_groups/", "")
			if len(requests) != 0 || !m.hostGroupPage.Next.Present() || !strings.Contains(hostGroupText(m), "7 · direct") {
				t.Fatal("membership missing or fetched all pages")
			}
			step("n")
			expect("hosts/3/all_groups/", "2")
			text := normalizedDetailText(m.View().Content)
			if !strings.Contains(text, "8 · [redacted]parent") || strings.Contains(text, "secret") || strings.Contains(text, "7 · direct") || !strings.Contains(text, "Enabled: false") {
				t.Fatal("next group page or host details incorrect", text)
			}
			step("p")
			expect("hosts/3/all_groups/", "1")
			step("r")
			expect("hosts/3/", "")
			expect("hosts/3/all_groups/", "")
			step("esc")
			if m.screen != hostsScreen || m.search != "web" || m.selected != 0 || len(m.hostPage.Items) != 2 {
				t.Fatal("back context lost")
			}
			step("j")
			step("enter")
			expect("hosts/4/", "")
			expect("hosts/4/all_groups/", "")
			if !strings.Contains(hostGroupText(m), "No visible groups.") || strings.Contains(hostGroupText(m), "direct") {
				t.Fatal("old host memberships leaked")
			}
		})
	}
}
