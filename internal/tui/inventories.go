package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

type InventoryReader interface {
	ListInventories(context.Context, aap.ListOptions) (aap.Page[aap.InventorySummary], error)
}

type InventoryContentsReader interface {
	InventoryGroups(context.Context, int, aap.ListOptions) (aap.Page[aap.GroupSummary], error)
	InventoryHosts(context.Context, int, aap.ListOptions) (aap.Page[aap.HostSummary], error)
	GroupChildren(context.Context, int, aap.ListOptions) (aap.Page[aap.GroupSummary], error)
	GroupHosts(context.Context, int, aap.ListOptions) (aap.Page[aap.HostSummary], error)
}

func (m *Model) WithInventories(reader InventoryReader) *Model {
	m.inventories = reader
	return m
}

func (m *Model) loadInventories(cursor aap.PageCursor) tea.Cmd {
	if m.inventories == nil {
		return nil
	}
	reader := m.inventories
	o := aap.ListOptions{Search: m.search, Cursor: cursor}
	return m.begin(func(ctx context.Context) (any, error) {
		return reader.ListInventories(ctx, o)
	})
}

func (m *Model) WithInventoryContents(reader InventoryContentsReader) *Model {
	m.inventoryContents = reader
	return m
}

func (m *Model) inventoryList() bool {
	return m.screen == inventoriesScreen || m.screen == groupsScreen || m.screen == hostsScreen
}

func (m *Model) loadInventoryList(cursor aap.PageCursor) tea.Cmd {
	if m.screen == inventoriesScreen {
		return m.loadInventories(cursor)
	}
	if m.inventoryContents == nil {
		return nil
	}
	reader := m.inventoryContents
	inventoryID, groupID, s := m.inventory.ID, m.group.ID, m.screen
	o := aap.ListOptions{Search: m.search, Cursor: cursor}
	return m.begin(func(ctx context.Context) (any, error) {
		if s == groupsScreen {
			if groupID > 0 {
				return reader.GroupChildren(ctx, groupID, o)
			}
			return reader.InventoryGroups(ctx, inventoryID, o)
		}
		if groupID > 0 {
			return reader.GroupHosts(ctx, groupID, o)
		}
		return reader.InventoryHosts(ctx, inventoryID, o)
	})
}

func (m *Model) inventoryCursor(next bool) aap.PageCursor {
	switch m.screen {
	case inventoriesScreen:
		if next {
			return m.inventoryPage.Next
		}
		return m.inventoryPage.Previous
	case groupsScreen:
		if next {
			return m.groupPage.Next
		}
		return m.groupPage.Previous
	case hostsScreen:
		if next {
			return m.hostPage.Next
		}
		return m.hostPage.Previous
	}
	return aap.PageCursor{}
}
