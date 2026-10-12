package aap

import (
	"context"
	"strings"
)

type User struct {
	ID       int
	Username string
}

// Me resolves the user associated with the configured token.
func (c *Client) Me(ctx context.Context) (User, error) {
	var empty User
	p, err := readPage[struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	}](ctx, c, "me/", ListOptions{PageSize: 1})
	if err != nil {
		return empty, err
	}
	if p.Count != 1 || len(p.Items) != 1 || p.Next.Present() || p.Previous.Present() {
		return empty, apiError(Malformed, "read authenticated user")
	}
	w := p.Items[0]
	username := c.cleanLabel(w.Username)
	if w.ID <= 0 || strings.TrimSpace(username) == "" {
		return empty, apiError(Malformed, "read authenticated user")
	}
	return User{ID: w.ID, Username: username}, nil
}
