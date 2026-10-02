package aap

type PageCursor struct{ link string }

func (c PageCursor) Present() bool { return c.link != "" }

type ListOptions struct {
	Search   string
	Cursor   PageCursor
	PageSize int
}
type Page[T any] struct {
	Items          []T
	Count          int
	Next, Previous PageCursor
}
