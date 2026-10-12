package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

// ObjectDetailsReader adds detail requests without changing collection readers.
type ObjectDetailsReader interface {
	Inventory(context.Context, int) (aap.InventoryDetails, error)
	Group(context.Context, int) (aap.GroupDetails, error)
	Host(context.Context, int) (aap.HostDetails, error)
}

func (m *Model) WithObjectDetails(reader ObjectDetailsReader) *Model {
	m.objectDetails = reader
	return m
}

func (m *Model) loadObjectDetails() tea.Cmd {
	reader := m.objectDetails
	if reader == nil {
		return nil
	}
	s, inventoryID, groupID, hostID := m.screen, m.inventory.ID, m.group.ID, m.hostDetail.ID
	return m.begin(func(ctx context.Context) (any, error) {
		switch s {
		case inventoryScreen:
			return reader.Inventory(ctx, inventoryID)
		case groupScreen:
			return reader.Group(ctx, groupID)
		default:
			return reader.Host(ctx, hostID)
		}
	})
}

func (m *Model) scrollingDetails() bool {
	return m.screen == jobScreen || m.screen == hostScreen || m.screen == nodeScreen ||
		(m.screen == templateScreen && (!m.hasTemplateJobs() || !m.templateJobsFocus)) ||
		((m.screen == inventoryScreen || m.screen == groupScreen) && m.objectDetailsFocus)
}

func (m *Model) inventoryDetailsBody(width, height int) []string {
	labels := []string{"Groups", "Hosts"}
	if m.screen == groupScreen {
		labels = []string{"Child groups", "Hosts (including descendants)"}
	}
	var lines []string
	start := max(0, m.selected-height+1)
	for i := start; i < len(labels) && len(lines) < height; i++ {
		lines = append(lines, m.selectRow(labels[i], !m.objectDetailsFocus && i == m.selected, width))
	}
	if len(lines) < height {
		focus := "menu"
		if m.objectDetailsFocus {
			focus = "details"
		}
		lines = append(lines, m.styles.muted.Render("Details · Tab focus: "+focus))
	}
	available := height - len(lines)
	if available <= 0 {
		return lines
	}
	details := m.details(width)
	top := min(m.detailTop, max(0, len(details)-available))
	return append(lines, details[top:]...)
}
