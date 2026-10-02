// Package aap owns controller API transport and resource operations.
package aap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"aaptui/internal/config"
)

const maxBody = 1024 * 1024
const defaultPageSize = 50

// Client is safe for concurrent reads. Its token and origin are deliberately private.
type Client struct {
	base          *url.URL
	prefix, token string
	http          *http.Client
	timeout       time.Duration
}

func NewClient(s config.Settings, token string, injected *http.Client) (*Client, error) {
	if err := config.Validate(s); err != nil {
		return nil, apiError(Malformed, "configure client")
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, apiError(Authentication, "configure client")
	}
	base, err := url.Parse(s.URL)
	if err != nil {
		return nil, apiError(Malformed, "configure client")
	}
	h := http.Client{}
	if injected != nil {
		h = *injected
	} else {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !s.TLSVerify}
		h.Transport = tr
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	prefix := "/api/controller/v2/"
	if s.Mode == config.Direct {
		prefix = "/api/v2/"
	}
	return &Client{base: base, prefix: prefix, token: token, http: &h, timeout: 15 * time.Second}, nil
}
func (c *Client) safeLink(link string) (*url.URL, error) {
	if len(link) > 8192 || strings.Contains(link, "#") {
		return nil, apiError(Malformed, "validate API link")
	}
	u, err := url.Parse(link)
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, apiError(Malformed, "validate API link")
	}
	u = c.base.ResolveReference(u)
	if u.Scheme != c.base.Scheme || !strings.EqualFold(u.Host, c.base.Host) || !strings.HasPrefix(u.Path, c.prefix) || u.RawPath != "" || strings.ContainsAny(u.Path, "\\\x00") || path.Clean(u.Path)+"/" != u.Path {
		return nil, apiError(Malformed, "validate API link")
	}
	return u, nil
}
func (c *Client) endpoint(route string) string { return c.prefix + route }
func (c *Client) request(ctx context.Context, method, link, op string, target any) (resultErr error) {
	u, err := c.safeLink(link)
	if err != nil {
		return apiError(Malformed, op)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return apiError(Malformed, op)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apiError(Connection, op)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && resultErr == nil {
			resultErr = apiError(Connection, op)
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := Malformed
		switch resp.StatusCode {
		case 401:
			kind = Authentication
		case 403:
			kind = Permission
		case 404, 410:
			kind = Missing
		case 405:
			kind = Unsupported
		case 408, 429:
			kind = Temporary
		default:
			if resp.StatusCode >= 500 {
				kind = Temporary
			}
		}
		retry := time.Duration(0)
		if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 {
			retry = time.Duration(min(n, 60)) * time.Second
		} else if date, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			retry = min(max(time.Until(date), 0), 60*time.Second)
		}
		return &APIError{Kind: kind, Operation: op, RetryAfter: retry}
	}
	if target == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return apiError(Connection, op)
	}
	if len(data) > maxBody {
		return apiError(Malformed, op)
	}
	if len(data) == 0 || string(data) == "null" || json.Unmarshal(data, target) != nil {
		return apiError(Malformed, op)
	}
	return nil
}

type wirePage[T any] struct {
	Count    *int   `json:"count"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
	Results  *[]T   `json:"results"`
}

func readPage[T any](ctx context.Context, c *Client, route string, o ListOptions) (Page[T], error) {
	link := c.endpoint(route)
	if o.Cursor.Present() {
		link = o.Cursor.link
		u, err := c.safeLink(link)
		if err != nil || u.Path != c.endpoint(route) {
			return Page[T]{}, apiError(Malformed, "read page")
		}
	} else {
		size := o.PageSize
		if size == 0 {
			size = defaultPageSize
		}
		if size < 1 || size > 100 {
			return Page[T]{}, apiError(Malformed, "read page")
		}
		q := url.Values{"page_size": {strconv.Itoa(size)}, "order_by": {"-id"}}
		if o.Search != "" {
			q.Set("search", o.Search)
		}
		link += "?" + q.Encode()
	}
	u, err := c.safeLink(link)
	if err != nil {
		return Page[T]{}, err
	}
	q := u.Query()
	size := defaultPageSize
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return Page[T]{}, apiError(Malformed, "read page")
		}
		size = n
	}
	q.Set("page_size", strconv.Itoa(size))
	u.RawQuery = q.Encode()
	link = u.String()
	var w wirePage[T]
	if err := c.request(ctx, http.MethodGet, link, "read page", &w); err != nil {
		return Page[T]{}, err
	}
	if w.Results == nil || w.Count == nil || *w.Count < len(*w.Results) || len(*w.Results) > 100 {
		return Page[T]{}, apiError(Malformed, "read page")
	}
	p := Page[T]{Items: *w.Results, Count: *w.Count}
	for i, l := range []string{w.Next, w.Previous} {
		if l == "" {
			continue
		}
		u, err := c.safeLink(l)
		if err != nil || u.Path != c.endpoint(route) {
			return Page[T]{}, apiError(Malformed, "read page")
		}
		if i == 0 {
			p.Next = PageCursor{u.String()}
		} else {
			p.Previous = PageCursor{u.String()}
		}
	}
	return p, nil
}

// CleanText removes terminal controls and redacts the configured token. It also
// discards CSI/OSC/DCS payloads rather than letting untrusted text control the terminal.
func (c *Client) cleanLabel(s string) string {
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(c.CleanText(s))
}
func validType(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
func (c *Client) CleanText(s string) string {
	if c.token == "" {
		return sanitize(s)
	}
	return strings.ReplaceAll(sanitize(strings.ReplaceAll(s, c.token, "[redacted]")), c.token, "[redacted]")
}
func sanitize(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		v := r[i]
		if v == 27 {
			if i+1 >= len(r) {
				break
			}
			i++
			switch r[i] {
			case '[':
				for i+1 < len(r) {
					i++
					if r[i] >= 0x40 && r[i] <= 0x7e {
						break
					}
				}
			case ']', 'P', '^', '_':
				for i+1 < len(r) {
					i++
					if r[i] == 7 {
						break
					}
					if r[i] == 27 && i+1 < len(r) && r[i+1] == '\\' {
						i++
						break
					}
				}
			}
			continue
		}
		if v == 0x9b {
			for i+1 < len(r) {
				i++
				if r[i] >= 0x40 && r[i] <= 0x7e {
					break
				}
			}
			continue
		}
		if v == '\n' || v == '\t' || (!unicode.IsControl(v) && !unicode.Is(unicode.Cf, v)) {
			b.WriteRune(v)
		}
	}
	return b.String()
}
