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

// Fixtures are synthetic models of the pinned upstream inventory contract.
func TestInventoryPagination(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != prefix+"inventories/" || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("search") != "demo inventory" || r.URL.Query().Get("page_size") != "5" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				response := map[string]any{"count": 2, "results": []any{map[string]any{"id": 7, "type": "inventory", "name": "secret\x1b[31mdemo", "kind": "smart"}}, "next": prefix + "inventories/?page=2&search=demo+inventory&page_size=5"}
				if r.URL.Query().Get("page") == "2" {
					response = map[string]any{"count": 2, "results": []any{map[string]any{"id": 8, "type": "inventory", "name": "second"}}, "previous": prefix + "inventories/?search=demo+inventory&page_size=5"}
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			})
			p, err := c.ListInventories(context.Background(), ListOptions{Search: "demo inventory", PageSize: 5})
			if err != nil || len(p.Items) != 1 || !p.Next.Present() || p.Items[0].ID != 7 || p.Items[0].Name != "[redacted]demo" || p.Items[0].Kind != "smart" {
				t.Fatal(p, err)
			}
			p, err = c.ListInventories(context.Background(), ListOptions{Cursor: p.Next})
			if err != nil || len(p.Items) != 1 || p.Items[0].ID != 8 || !p.Previous.Present() {
				t.Fatal(p, err)
			}
			p, err = c.ListInventories(context.Background(), ListOptions{Cursor: p.Previous})
			if err != nil || len(p.Items) != 1 || p.Items[0].ID != 7 {
				t.Fatal(p, err)
			}
		})
	}
}

func TestInventoryMembersBothPrefixes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		for _, route := range []string{"inventories/7/groups/", "inventories/7/hosts/", "groups/9/children/", "groups/9/all_hosts/"} {
			t.Run(string(mode)+"/"+route, func(t *testing.T) {
				prefix := "/api/controller/v2/"
				if mode == config.Direct {
					prefix = "/api/v2/"
				}
				isHost := strings.Contains(route, "hosts/")
				c, _ := testClient(t, mode, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != prefix+route || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("search") != "member search" || r.URL.Query().Get("page_size") != "1" {
						t.Errorf("unexpected request: %s", r.URL)
					}
					kind := "group"
					if isHost {
						kind = "host"
					}
					member := map[string]any{"id": 11, "type": kind, "inventory": 7, "name": "secret\x1b[31mmember\nname", "description": "secret\x1b[0mdescription", "enabled": false}
					response := map[string]any{"count": 2, "results": []any{member}, "next": prefix + route + "?page=2&search=member+search&page_size=1"}
					if r.URL.Query().Get("page") == "2" {
						member["id"] = 12
						// Smart inventory hosts can belong to another inventory.
						if isHost {
							member["inventory"] = 8
						}
						response["next"] = nil
						response["previous"] = prefix + route + "?search=member+search&page_size=1"
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				})
				ctx := context.Background()
				o := ListOptions{Search: "member search", PageSize: 1}
				if isHost {
					read := c.InventoryHosts
					id := 7
					if strings.HasPrefix(route, "groups/") {
						read, id = c.GroupHosts, 9
					}
					p, err := read(ctx, id, o)
					if err != nil || len(p.Items) != 1 || p.Items[0].Name != "[redacted]member name" || p.Items[0].Description != "[redacted]description" || p.Items[0].Enabled == nil || *p.Items[0].Enabled || !p.Next.Present() {
						t.Fatal(p, err)
					}
					p, err = read(ctx, id, ListOptions{Cursor: p.Next})
					if err != nil || len(p.Items) != 1 || p.Items[0].ID != 12 || p.Items[0].InventoryID != 8 || !p.Previous.Present() {
						t.Fatal(p, err)
					}
					p, err = read(ctx, id, ListOptions{Cursor: p.Previous})
					if err != nil || len(p.Items) != 1 || p.Items[0].ID != 11 {
						t.Fatal(p, err)
					}
				} else {
					read := c.InventoryGroups
					id := 7
					if strings.HasPrefix(route, "groups/") {
						read, id = c.GroupChildren, 9
					}
					p, err := read(ctx, id, o)
					if err != nil || len(p.Items) != 1 || p.Items[0].Name != "[redacted]member name" || p.Items[0].Description != "[redacted]description" || p.Items[0].InventoryID != 7 || !p.Next.Present() {
						t.Fatal(p, err)
					}
					p, err = read(ctx, id, ListOptions{Cursor: p.Next})
					if err != nil || len(p.Items) != 1 || p.Items[0].ID != 12 || !p.Previous.Present() {
						t.Fatal(p, err)
					}
					p, err = read(ctx, id, ListOptions{Cursor: p.Previous})
					if err != nil || len(p.Items) != 1 || p.Items[0].ID != 11 {
						t.Fatal(p, err)
					}
				}
			})
		}
	}
}

func TestInventoryMembersErrors(t *testing.T) {
	for _, kind := range []string{"group", "host"} {
		for _, test := range []struct {
			name string
			code int
			body string
			want ErrorKind
		}{
			{"missing", 404, "secret", Missing},
			{"permission", 403, "secret", Permission},
			{"authentication", 401, "secret", Authentication},
			{"wrong type", 200, `{"count":1,"results":[{"id":1,"type":"project","inventory":7}]}`, Malformed},
			{"invalid ID", 200, `{"count":1,"results":[{"id":0,"type":"` + kind + `","inventory":7}]}`, Malformed},
			{"invalid inventory", 200, `{"count":1,"results":[{"id":1,"type":"` + kind + `"}]}`, Malformed},
			{"wrong collection", 200, `{"count":0,"results":[],"next":"/api/v2/inventories/8/hosts/?page=2"}`, Malformed},
			{"foreign link", 200, `{"count":0,"results":[],"next":"https://evil.example/api/v2/groups/9/children/"}`, Malformed},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(test.code)
					if _, err := w.Write([]byte(test.body)); err != nil {
						t.Error(err)
					}
				})
				var err error
				if kind == "group" {
					_, err = c.GroupChildren(context.Background(), 9, ListOptions{})
				} else {
					_, err = c.InventoryHosts(context.Background(), 7, ListOptions{})
				}
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Kind != test.want || strings.Contains(err.Error(), "secret") {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestInventoryMembersRejectInvalidIDsAndCursors(t *testing.T) {
	hits := 0
	c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) { hits++; http.Error(w, "unexpected request", 500) })
	ctx := context.Background()
	for _, id := range []int{0, -1} {
		if _, err := c.InventoryGroups(ctx, id, ListOptions{}); err == nil {
			t.Fatal("invalid inventory ID accepted")
		}
		if _, err := c.InventoryHosts(ctx, id, ListOptions{}); err == nil {
			t.Fatal("invalid inventory ID accepted")
		}
		if _, err := c.GroupChildren(ctx, id, ListOptions{}); err == nil {
			t.Fatal("invalid group ID accepted")
		}
		if _, err := c.GroupHosts(ctx, id, ListOptions{}); err == nil {
			t.Fatal("invalid group ID accepted")
		}
	}
	o := ListOptions{Cursor: PageCursor{link: "/api/v2/groups/10/all_hosts/?page=2"}}
	if _, err := c.GroupHosts(ctx, 9, o); err == nil {
		t.Fatal("cross-group cursor accepted")
	}
	if hits != 0 {
		t.Fatal("invalid input reached server")
	}
}

func TestInventoriesRejectMalformedResourcesAndLinks(t *testing.T) {
	for _, body := range []string{
		`{"count":1,"results":[{"id":0,"type":"inventory"}]}`,
		`{"count":1,"results":[{"id":1,"type":"inventory_source"}]}`,
		`{"count":1,"results":[{"id":1}]}`,
		`{"count":0,"results":[],"next":"/api/v2/projects/?page=2"}`,
		`{"count":0,"results":[],"next":"https://evil.example/api/v2/inventories/"}`,
	} {
		t.Run(body, func(t *testing.T) {
			c, _ := testClient(t, config.Direct, func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			})
			_, err := c.ListInventories(context.Background(), ListOptions{})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Kind != Malformed {
				t.Fatal(err)
			}
		})
	}
}
