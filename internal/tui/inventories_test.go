package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"aaptui/internal/aap"
	"aaptui/internal/config"
)

type fakeInventories struct{ search string }

func (f *fakeInventories) ListInventories(ctx context.Context, o aap.ListOptions) (aap.Page[aap.InventorySummary], error) {
	f.search = o.Search
	return aap.Page[aap.InventorySummary]{Count: 2, Items: []aap.InventorySummary{{ID: 7, Name: "first inventory"}, {ID: 8, Name: "second inventory"}}}, nil
}

type fakeInventoryContents struct {
	err       error
	ctx       context.Context
	id        int
	operation string
}

func (f *fakeInventoryContents) InventoryGroups(ctx context.Context, id int, o aap.ListOptions) (aap.Page[aap.GroupSummary], error) {
	f.ctx, f.id, f.operation = ctx, id, "inventory groups"
	return aap.Page[aap.GroupSummary]{}, f.err
}
func (f *fakeInventoryContents) GroupChildren(ctx context.Context, id int, o aap.ListOptions) (aap.Page[aap.GroupSummary], error) {
	f.ctx, f.id, f.operation = ctx, id, "child groups"
	return aap.Page[aap.GroupSummary]{}, f.err
}
func (f *fakeInventoryContents) InventoryHosts(ctx context.Context, id int, o aap.ListOptions) (aap.Page[aap.HostSummary], error) {
	f.ctx, f.id, f.operation = ctx, id, "inventory hosts"
	return aap.Page[aap.HostSummary]{}, f.err
}
func (f *fakeInventoryContents) GroupHosts(ctx context.Context, id int, o aap.ListOptions) (aap.Page[aap.HostSummary], error) {
	f.ctx, f.id, f.operation = ctx, id, "group hosts"
	return aap.Page[aap.HostSummary]{}, f.err
}

func TestInventoryContentsErrorsRecoveryAndCancellation(t *testing.T) {
	for _, groupID := range []int{0, 9} {
		for _, selection := range []int{0, 1} {
			for _, kind := range []aap.ErrorKind{aap.Permission, aap.Missing} {
				t.Run(fmt.Sprintf("group%d/selection%d/%s", groupID, selection, kind), func(t *testing.T) {
					f := &fakeInventoryContents{err: &aap.APIError{Kind: kind, Operation: "read page"}}
					m := New(context.Background(), "gateway").WithInventoryContents(f)
					cleanupModel(t, m)
					m.screen, m.selected = inventoryScreen, selection
					m.inventory = aap.InventorySummary{ID: 7, Name: "inventory"}
					m.group = aap.GroupSummary{ID: groupID, Name: "group"}
					if groupID > 0 {
						m.screen = groupScreen
					}
					parent := m.screen
					_, cmd := m.Update(key("enter"))
					resource := "groups"
					if selection == 1 {
						resource = "hosts"
					}
					if !strings.Contains(m.View().Content, "Loading "+resource) {
						t.Fatal("loading state", m.View().Content)
					}
					// Enter during a pending list request must not open old items.
					m.Update(key("enter"))
					m.Update(cmd())
					if !strings.Contains(m.View().Content, "Unable to load "+resource) || !strings.Contains(m.View().Content, string(kind)) {
						t.Fatal("error state", m.View().Content)
					}
					wantID, wantOperation := 7, "inventory "+resource
					if groupID > 0 {
						wantID = 9
						wantOperation = "child groups"
						if selection == 1 {
							wantOperation = "group hosts"
						}
					}
					if f.id != wantID || f.operation != wantOperation {
						t.Fatal("wrong inventory/group context", f.id, f.operation)
					}
					f.err = nil
					_, cmd = m.Update(key("r"))
					m.Update(cmd())
					if m.err != nil || !strings.Contains(m.View().Content, "No "+resource+".") {
						t.Fatal("refresh recovery", m.View().Content)
					}
					m.Update(key("/"))
					m.Update(key("esc"))
					if m.editing || m.screen == parent {
						t.Fatal("Esc failed to exit search first")
					}
					_, cmd = m.Update(key("r"))
					m.Update(key("esc"))
					m.Update(cmd())
					if f.ctx.Err() != context.Canceled || m.screen != parent || m.selected != selection || m.inventory.ID != 7 || m.group.ID != groupID || m.loading || m.err != nil {
						t.Fatal("request cancellation/back restoration")
					}
				})
			}
		}
	}
}

func TestInventorySelectionAndHostStatus(t *testing.T) {
	m := New(context.Background(), "gateway").WithInventoryContents(&fakeInventoryContents{})
	cleanupModel(t, m)
	enabled, disabled := true, false
	m.screen = hostsScreen
	m.hostPage.Items = []aap.HostSummary{{Name: "enabled host", Enabled: &enabled}, {Name: "disabled host", Enabled: &disabled}, {Name: "unknown host"}}
	view := ansi.Strip(m.View().Content)
	for _, status := range []string{"enabled", "disabled", "unknown"} {
		if !strings.Contains(view, status) {
			t.Fatal("host state missing", view)
		}
	}
	for i := 0; i < 5; i++ {
		m.Update(key("j"))
	}
	if m.selected != 2 {
		t.Fatal("unbounded host selection")
	}
	if _, cmd := m.Update(key("enter")); cmd != nil || m.screen != hostsScreen {
		t.Fatal("host list opens an unsupported detail")
	}
	for _, s := range []screen{inventoryScreen, groupScreen} {
		m.screen, m.selected = s, 0
		for i := 0; i < 5; i++ {
			m.Update(key("j"))
		}
		if m.selected != 1 {
			t.Fatal("unbounded submenu selection")
		}
	}
	m.screen, m.selected = groupsScreen, 0
	m.groupPage.Items = []aap.GroupSummary{{ID: 9}, {ID: 10}}
	for i := 0; i < 5; i++ {
		m.Update(key("j"))
	}
	if m.selected != 1 {
		t.Fatal("unbounded group selection")
	}
}

func TestInventoryListRepaintAfterSearch(t *testing.T) {
	for _, s := range []screen{inventoriesScreen, groupsScreen, hostsScreen} {
		m := New(context.Background(), "gateway").WithInventories(&fakeInventories{}).WithInventoryContents(&fakeInventoryContents{})
		cleanupModel(t, m)
		m.screen = s
		m.inventory.ID = 7
		m.Update(key("/"))
		m.Update(key("q"))
		_, cmd := m.Update(key("enter"))
		_, repaint := m.Update(cmd())
		if repaint == nil || repaint() != tea.ClearScreen() || m.editing || m.search != "q" || m.quitting {
			t.Fatal("search replacement did not repaint")
		}
	}
}

// Synthetic integration: no deployed AAP instance or captured inventory data.
func TestInventoryTraversalBothPrefixes(t *testing.T) {
	for _, mode := range []config.ConnectionMode{config.Gateway, config.Direct} {
		t.Run(string(mode), func(t *testing.T) {
			prefix := "/api/controller/v2/"
			if mode == config.Direct {
				prefix = "/api/v2/"
			}
			type request struct{ route, search, page string }
			requests := make(chan request, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("unexpected mutation or missing authentication")
				}
				route := strings.TrimPrefix(r.URL.Path, prefix)
				search, page := r.URL.Query().Get("search"), r.URL.Query().Get("page")
				requests <- request{route, search, page}
				var items []any
				member := func(id int, kind, name string) any {
					return map[string]any{"id": id, "type": kind, "inventory": 8, "name": name, "enabled": true}
				}
				switch route {
				case "inventories/":
					items = []any{member(7, "inventory", "first inventory")}
					if page == "2" {
						items = []any{member(8, "inventory", "selected inventory")}
					}
				case "inventories/8/hosts/":
					items = []any{member(41, "host", "ungrouped host"), member(42, "host", "grouped host")}
					if page == "2" {
						items = []any{member(43, "host", "last inventory host")}
					}
				case "inventories/8/groups/":
					items = []any{member(10, "group", "first group"), member(11, "group", "parent group")}
					if page == "2" {
						items = []any{member(12, "group", "last inventory group")}
					}
				case "groups/11/children/":
					items = []any{member(21, "group", "first child"), member(22, "group", "second child")}
					if page == "2" {
						items = []any{member(23, "group", "nested group")}
					}
				case "groups/11/all_hosts/", "groups/23/all_hosts/":
					items = []any{member(51, "host", "direct host"), member(52, "host", "descendant host")}
					if page == "2" {
						items = []any{member(53, "host", "last group host")}
					}
				case "groups/23/children/":
					if err := json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []any{}}); err != nil {
						t.Error(err)
					}
					return
				default:
					http.NotFound(w, r)
					return
				}
				count := 3
				if route == "inventories/" {
					count = 2
				}
				response := map[string]any{"count": count, "results": items}
				q := r.URL.Query()
				if page == "2" {
					q.Del("page")
					response["previous"] = prefix + route + "?" + q.Encode()
				} else {
					q.Set("page", "2")
					response["next"] = prefix + route + "?" + q.Encode()
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
			m := New(context.Background(), string(mode)).WithInventories(client).WithInventoryContents(client)
			cleanupModel(t, m)
			load := func(k, route, search, page string) {
				t.Helper()
				_, cmd := m.Update(key(k))
				if cmd == nil || !m.loading {
					t.Fatalf("%q did not start a request", k)
				}
				_, repaint := m.Update(cmd())
				if m.err != nil {
					t.Fatal(m.err)
				}
				if repaint == nil {
					t.Fatal("replacement list did not request repaint")
				}
				if got, want := <-requests, (request{route, search, page}); got != want {
					t.Fatalf("request = %+v, want %+v", got, want)
				}
			}
			search := func(route string) {
				t.Helper()
				m.Update(key("/"))
				m.Update(key("q"))
				if m.quitting {
					t.Fatal("search quit")
				}
				load("enter", route, "q", "")
			}
			m.selected = 3
			load("enter", "inventories/", "", "")
			search("inventories/")
			load("n", "inventories/", "q", "2")
			m.Update(key("enter"))
			if m.screen != inventoryScreen || m.inventory.ID != 8 || !strings.Contains(m.View().Content, "Groups") || !strings.Contains(m.View().Content, "Hosts") {
				t.Fatal("inventory submenu", m.View().Content)
			}
			m.Update(key("j"))
			load("enter", "inventories/8/hosts/", "", "")
			if !strings.Contains(m.View().Content, "ungrouped host") || !strings.Contains(m.View().Content, "grouped host") {
				t.Fatal("inventory hosts", m.View().Content)
			}
			search("inventories/8/hosts/")
			load("n", "inventories/8/hosts/", "q", "2")
			if !strings.Contains(m.View().Content, "last inventory host") {
				t.Fatal("host missing from next page")
			}
			load("p", "inventories/8/hosts/", "q", "")
			load("r", "inventories/8/hosts/", "q", "")
			m.Update(key("esc"))
			if m.screen != inventoryScreen || m.selected != 1 || m.inventory.ID != 8 {
				t.Fatal("inventory submenu not restored")
			}
			m.Update(key("k"))
			load("enter", "inventories/8/groups/", "", "")
			search("inventories/8/groups/")
			load("n", "inventories/8/groups/", "q", "2")
			load("p", "inventories/8/groups/", "q", "")
			m.Update(key("j"))
			m.Update(key("enter"))
			if m.screen != groupScreen || m.group.ID != 11 {
				t.Fatal("group submenu")
			}
			load("enter", "groups/11/children/", "", "")
			search("groups/11/children/")
			load("n", "groups/11/children/", "q", "2")
			m.Update(key("enter"))
			if m.screen != groupScreen || m.group.ID != 23 || m.inventory.ID != 8 {
				t.Fatal("nested group")
			}
			load("enter", "groups/23/children/", "", "")
			if !strings.Contains(m.View().Content, "No groups.") {
				t.Fatal("empty child list")
			}
			m.Update(key("esc"))
			m.Update(key("j"))
			load("enter", "groups/23/all_hosts/", "", "")
			if !strings.Contains(m.View().Content, "including descendant") || !strings.Contains(m.View().Content, "descendant host") {
				t.Fatal("descendant hosts", m.View().Content)
			}
			load("n", "groups/23/all_hosts/", "", "2")
			if !strings.Contains(m.View().Content, "last group host") {
				t.Fatal("group host pagination")
			}
			load("p", "groups/23/all_hosts/", "", "")
			m.Update(key("esc"))
			if m.screen != groupScreen || m.group.ID != 23 || m.selected != 1 {
				t.Fatal("nested submenu not restored")
			}
			m.Update(key("esc"))
			if m.screen != groupsScreen || m.group.ID != 11 || m.search != "q" || len(m.groupPage.Items) != 1 || m.groupPage.Items[0].ID != 23 || !m.groupPage.Previous.Present() {
				t.Fatal("child page not restored")
			}
			m.Update(key("esc"))
			if m.screen != groupScreen || m.group.ID != 11 || m.selected != 0 {
				t.Fatal("parent group not restored")
			}
			m.Update(key("j"))
			load("enter", "groups/11/all_hosts/", "", "")
			m.Update(key("esc"))
			m.Update(key("esc"))
			if m.screen != groupsScreen || m.group.ID != 0 || m.selected != 1 || m.search != "q" || len(m.groupPage.Items) != 2 {
				t.Fatal("inventory groups not restored")
			}
			m.Update(key("esc"))
			m.Update(key("esc"))
			if m.screen != inventoriesScreen || m.inventory.ID != 0 || m.search != "q" || len(m.inventoryPage.Items) != 1 || m.inventoryPage.Items[0].ID != 8 || !m.inventoryPage.Previous.Present() {
				t.Fatal("inventory page not restored")
			}
			m.Update(key("esc"))
			if m.screen != mainScreen || m.selected != 3 {
				t.Fatal("main not restored")
			}
		})
	}
}

func TestInventoriesMainMenu(t *testing.T) {
	f := &fakeInventories{}
	m := New(context.Background(), "gateway").WithInventories(f)
	cleanupModel(t, m)
	if !strings.Contains(m.View().Content, "Inventories") {
		t.Fatal("main menu missing Inventories")
	}
	for i := 0; i < 5; i++ {
		m.Update(key("j"))
	}
	if m.selected != 3 {
		t.Fatal("unbounded main selection")
	}
	_, cmd := m.Update(key("enter"))
	if cmd == nil || m.screen != inventoriesScreen || !strings.Contains(m.View().Content, "Loading inventories") {
		t.Fatal("inventory load not started")
	}
	m.Update(cmd())
	if !strings.Contains(m.View().Content, "first inventory") {
		t.Fatal(m.View().Content)
	}
	m.Update(key("/"))
	m.Update(key("q"))
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if f.search != "q" || m.quitting {
		t.Fatal("search/quit")
	}
	m.Update(key("j"))
	m.Update(key("j"))
	if m.selected != 1 {
		t.Fatal("unbounded inventory selection")
	}
	_, cmd = m.Update(key("r"))
	m.Update(cmd())
	if f.search != "q" {
		t.Fatal("refresh lost search")
	}
	m.Update(key("esc"))
	if m.screen != mainScreen || m.selected != 3 {
		t.Fatal("main menu not restored")
	}
}

func TestInventoryLoadingErrorAndStaleResults(t *testing.T) {
	for _, stale := range []bool{false, true} {
		m := New(context.Background(), "gateway").WithInventories(&fakeInventories{})
		cleanupModel(t, m)
		m.selected = 3
		_, cmd := m.Update(key("enter"))
		if stale {
			m.Update(key("esc"))
			m.Update(cmd())
			if m.screen != mainScreen || m.loading || len(m.inventoryPage.Items) != 0 {
				t.Fatal("stale inventory response applied")
			}
		} else {
			m.Update(resultMsg{id: m.generation, err: &aap.APIError{Kind: aap.Permission, Operation: "read page"}})
			if m.loading || !strings.Contains(m.View().Content, "Unable to load inventories.") || !strings.Contains(m.View().Content, "permission") {
				t.Fatal(m.View().Content)
			}
		}
	}
}
