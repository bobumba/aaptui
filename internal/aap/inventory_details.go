package aap

import (
	"context"
	"fmt"
	"net/http"
)

type InventoryDetails struct {
	InventorySummary
	Metadata                                                                              ResourceMetadata
	HostFilter                                                                            string
	TotalHosts, TotalGroups, TotalInventorySources                                        *int
	HostsWithActiveFailures, InventorySourcesWithFailures                                 *int
	HasActiveFailures, HasInventorySources, PendingDeletion, PreventInstanceGroupFallback *bool
}

type GroupDetails struct {
	GroupSummary
	Metadata  ResourceMetadata
	Inventory *ResourceReference
}

type HostDetails struct {
	HostSummary
	Metadata                               ResourceMetadata
	Inventory                              *ResourceReference
	InstanceID, FactsModified              string
	HasActiveFailures, HasInventorySources *bool
	LastJob, LastJobHostSummary            *int
}

func (c *Client) Inventory(ctx context.Context, id int) (InventoryDetails, error) {
	if id <= 0 {
		return InventoryDetails{}, apiError(Malformed, "read inventory")
	}
	var w wireInventory
	if err := c.request(ctx, http.MethodGet, c.endpoint(fmt.Sprintf("inventories/%d/", id)), "read inventory", &w); err != nil {
		return InventoryDetails{}, err
	}
	if w.ID != id || w.Type != "inventory" {
		return InventoryDetails{}, apiError(Malformed, "read inventory")
	}
	return InventoryDetails{
		InventorySummary: InventorySummary{ID: w.ID, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Kind: c.cleanLabel(w.Kind)},
		Metadata:         c.metadata(w.wireDetails), HostFilter: c.cleanLabel(w.HostFilter),
		TotalHosts: w.TotalHosts, TotalGroups: w.TotalGroups, TotalInventorySources: w.TotalInventorySources,
		HostsWithActiveFailures: w.HostsWithActiveFailures, InventorySourcesWithFailures: w.InventorySourcesWithFailures,
		HasActiveFailures: w.HasActiveFailures, HasInventorySources: w.HasInventorySources, PendingDeletion: w.PendingDeletion,
		PreventInstanceGroupFallback: w.PreventInstanceGroupFallback,
	}, nil
}

func (c *Client) Group(ctx context.Context, id int) (GroupDetails, error) {
	if id <= 0 {
		return GroupDetails{}, apiError(Malformed, "read group")
	}
	var w wireInventoryMember
	if err := c.request(ctx, http.MethodGet, c.endpoint(fmt.Sprintf("groups/%d/", id)), "read group", &w); err != nil {
		return GroupDetails{}, err
	}
	if w.ID != id || w.Type != "group" || w.Inventory <= 0 {
		return GroupDetails{}, apiError(Malformed, "read group")
	}
	return GroupDetails{
		GroupSummary: GroupSummary{ID: w.ID, InventoryID: w.Inventory, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description)},
		Metadata:     c.metadata(w.wireDetails), Inventory: c.reference(&w.Inventory, w.Summary.Inventory),
	}, nil
}

func (c *Client) Host(ctx context.Context, id int) (HostDetails, error) {
	if id <= 0 {
		return HostDetails{}, apiError(Malformed, "read host")
	}
	var w wireInventoryMember
	if err := c.request(ctx, http.MethodGet, c.endpoint(fmt.Sprintf("hosts/%d/", id)), "read host", &w); err != nil {
		return HostDetails{}, err
	}
	if w.ID != id || w.Type != "host" || w.Inventory <= 0 {
		return HostDetails{}, apiError(Malformed, "read host")
	}
	return HostDetails{
		HostSummary: HostSummary{ID: w.ID, InventoryID: w.Inventory, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Enabled: w.Enabled},
		Metadata:    c.metadata(w.wireDetails), Inventory: c.reference(&w.Inventory, w.Summary.Inventory),
		InstanceID: c.cleanLabel(w.InstanceID), FactsModified: c.cleanLabel(w.FactsModified),
		HasActiveFailures: w.HasActiveFailures, HasInventorySources: w.HasInventorySources, LastJob: w.LastJob, LastJobHostSummary: w.LastJobHostSummary,
	}, nil
}
