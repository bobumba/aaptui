package aap

import (
	"context"
	"fmt"
)

// InventorySummary describes an inventory, rather than an inventory source.
type InventorySummary struct {
	ID                      int
	Name, Description, Kind string
}

type wireInventory struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

func (c *Client) ListInventories(ctx context.Context, o ListOptions) (Page[InventorySummary], error) {
	p, err := readPage[wireInventory](ctx, c, "inventories/", o)
	if err != nil {
		return Page[InventorySummary]{}, err
	}
	out := Page[InventorySummary]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 || w.Type != "inventory" {
			return Page[InventorySummary]{}, apiError(Malformed, "list inventories")
		}
		out.Items = append(out.Items, InventorySummary{ID: w.ID, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Kind: c.cleanLabel(w.Kind)})
	}
	return out, nil
}

type GroupSummary struct {
	ID, InventoryID   int
	Name, Description string
}

type HostSummary struct {
	ID, InventoryID   int
	Name, Description string
	Enabled           *bool
}

type wireInventoryMember struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Inventory   int    `json:"inventory"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

func (c *Client) InventoryGroups(ctx context.Context, inventoryID int, o ListOptions) (Page[GroupSummary], error) {
	if inventoryID <= 0 {
		return Page[GroupSummary]{}, apiError(Malformed, "list inventory groups")
	}
	return c.listGroups(ctx, fmt.Sprintf("inventories/%d/groups/", inventoryID), o)
}

func (c *Client) GroupChildren(ctx context.Context, groupID int, o ListOptions) (Page[GroupSummary], error) {
	if groupID <= 0 {
		return Page[GroupSummary]{}, apiError(Malformed, "list child groups")
	}
	return c.listGroups(ctx, fmt.Sprintf("groups/%d/children/", groupID), o)
}

func (c *Client) listGroups(ctx context.Context, route string, o ListOptions) (Page[GroupSummary], error) {
	p, err := readPage[wireInventoryMember](ctx, c, route, o)
	if err != nil {
		return Page[GroupSummary]{}, err
	}
	out := Page[GroupSummary]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 || w.Inventory <= 0 || w.Type != "group" {
			return Page[GroupSummary]{}, apiError(Malformed, "list groups")
		}
		out.Items = append(out.Items, GroupSummary{ID: w.ID, InventoryID: w.Inventory, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description)})
	}
	return out, nil
}

func (c *Client) InventoryHosts(ctx context.Context, inventoryID int, o ListOptions) (Page[HostSummary], error) {
	if inventoryID <= 0 {
		return Page[HostSummary]{}, apiError(Malformed, "list inventory hosts")
	}
	return c.listHosts(ctx, fmt.Sprintf("inventories/%d/hosts/", inventoryID), o)
}

// GroupHosts includes hosts in descendant groups using the all_hosts collection.
func (c *Client) GroupHosts(ctx context.Context, groupID int, o ListOptions) (Page[HostSummary], error) {
	if groupID <= 0 {
		return Page[HostSummary]{}, apiError(Malformed, "list group hosts")
	}
	return c.listHosts(ctx, fmt.Sprintf("groups/%d/all_hosts/", groupID), o)
}

func (c *Client) listHosts(ctx context.Context, route string, o ListOptions) (Page[HostSummary], error) {
	p, err := readPage[wireInventoryMember](ctx, c, route, o)
	if err != nil {
		return Page[HostSummary]{}, err
	}
	out := Page[HostSummary]{Count: p.Count, Next: p.Next, Previous: p.Previous}
	for _, w := range p.Items {
		if w.ID <= 0 || w.Inventory <= 0 || w.Type != "host" {
			return Page[HostSummary]{}, apiError(Malformed, "list hosts")
		}
		out.Items = append(out.Items, HostSummary{ID: w.ID, InventoryID: w.Inventory, Name: c.cleanLabel(w.Name), Description: c.CleanText(w.Description), Enabled: w.Enabled})
	}
	return out, nil
}
