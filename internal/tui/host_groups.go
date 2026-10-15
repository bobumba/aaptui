package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"aaptui/internal/aap"
)

type HostGroupsReader interface {
	HostGroups(context.Context, int, aap.ListOptions) (aap.Page[aap.GroupSummary], error)
}

type hostGroupsMsg struct {
	id     uint64
	hostID int
	page   aap.Page[aap.GroupSummary]
	err    error
}

func (m *Model) WithHostGroups(reader HostGroupsReader) *Model {
	m.hostGroups = reader
	return m
}

func (m *Model) invalidateHostGroups() {
	if m.hostGroupsCancel != nil {
		m.hostGroupsCancel()
	}
	m.hostGroupsID++
	m.hostGroupsLoading, m.hostGroupsKnown = false, false
	m.hostGroupsErr = nil
	m.hostGroupPage = aap.Page[aap.GroupSummary]{}
}

func (m *Model) loadHostGroups(cursor aap.PageCursor) tea.Cmd {
	m.invalidateHostGroups()
	reader, hostID, id := m.hostGroups, m.hostDetail.ID, m.hostGroupsID
	if reader == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.hostGroupsCancel = cancel
	m.hostGroupsLoading = true
	return func() tea.Msg {
		defer cancel()
		page, err := reader.HostGroups(ctx, hostID, aap.ListOptions{Cursor: cursor})
		return hostGroupsMsg{id: id, hostID: hostID, page: page, err: err}
	}
}

func (m *Model) hostGroupFields() detailFields {
	var fields detailFields
	fields.section("Groups (including parent groups)")
	switch {
	case m.hostGroupsLoading:
		fields.field("Membership", "Loading…")
	case m.hostGroupsErr != nil:
		message := "Unable to load groups; press r to retry."
		var apiErr *aap.APIError
		if errors.As(m.hostGroupsErr, &apiErr) {
			switch apiErr.Kind {
			case aap.Permission:
				message = "Permission denied; press r to retry."
			case aap.Missing, aap.Unsupported:
				message = "Unavailable; press r to retry."
			case aap.Authentication:
				message = "Authentication failed; press r to retry."
			}
		}
		fields.field("Membership", message)
	case !m.hostGroupsKnown:
		fields.field("Membership", "Unavailable")
	default:
		fields.field("Visible groups", fmt.Sprint(m.hostGroupPage.Count))
		if len(m.hostGroupPage.Items) == 0 {
			fields.field("Membership", "No visible groups.")
		}
		var groups []string
		for _, group := range m.hostGroupPage.Items {
			entry := fmt.Sprintf("- %d", group.ID)
			if group.Name != "" {
				entry += " · " + group.Name
			}
			groups = append(groups, entry)
		}
		if len(groups) > 0 {
			fields = append(fields, detailField{label: "Groups", value: strings.Join(groups, "\n"), preformatted: true})
		}
		if m.hostGroupPage.Previous.Present() || m.hostGroupPage.Next.Present() {
			paging := ""
			if m.hostGroupPage.Previous.Present() {
				paging = "p previous"
			}
			if m.hostGroupPage.Next.Present() {
				if paging != "" {
					paging += " · "
				}
				paging += "n next"
			}
			fields.field("Pages", paging)
		}
	}
	return fields
}
