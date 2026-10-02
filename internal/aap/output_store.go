package aap

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// OutputLimits bound text, line metadata, and disk files independently.
type OutputLimits struct{ MemoryBytes, DiskBytes, MaxLines, MaxFiles int }

func DefaultOutputLimits() OutputLimits {
	return OutputLimits{MemoryBytes: 256 * 1024, DiskBytes: 16 * 1024 * 1024, MaxLines: 65536, MaxFiles: 256}
}

type storedLine struct {
	Number int
	Text   string
}
type diskChunk struct {
	path  string
	bytes int
}
type location struct {
	path  string
	index int
}

// OutputStore owns one random private session directory. Methods are synchronized
// because asynchronous UI commands may read history while a follower appends.
type OutputStore struct {
	mu                     sync.Mutex
	dir, token             string
	limits                 OutputLimits
	index                  map[int]location
	memory                 map[int]string
	memoryBytes, diskBytes int
	files                  []diskChunk
	cursor                 OutputCursor
	closed                 bool
	writeFile              func(string, []byte, os.FileMode) error
}

func NewOutputStore(token string, limits OutputLimits) (*OutputStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, apiError(Storage, "locate output cache home")
	}
	return newOutputStore(home, token, limits)
}
func newOutputStore(home, token string, limits OutputLimits) (*OutputStore, error) {
	if !filepath.IsAbs(home) || limits.MemoryBytes < 1 || limits.DiskBytes < 1 || limits.MaxLines < 1 || limits.MaxFiles < 1 {
		return nil, apiError(Storage, "configure output cache")
	}
	cache := filepath.Join(home, ".cache")
	base := filepath.Join(cache, "aaptui")
	// Reject symlinks in cache ancestors so files remain under the user's home.
	for _, dir := range []string{cache, base} {
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return nil, apiError(Storage, "create private output cache")
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, apiError(Storage, "validate private output cache")
		}
	}
	if err := os.Chmod(base, 0700); err != nil {
		return nil, apiError(Storage, "secure output cache directory")
	}
	dir, err := os.MkdirTemp(base, "session-")
	if err != nil {
		return nil, apiError(Storage, "create output session")
	}
	return &OutputStore{dir: dir, token: token, limits: limits, index: make(map[int]location), memory: make(map[int]string), writeFile: os.WriteFile}, nil
}
func (s *OutputStore) Cursor() OutputCursor { s.mu.Lock(); defer s.mu.Unlock(); return s.cursor }
func (s *OutputStore) readLines(start, end int) ([]storedLine, error) {
	result := make([]storedLine, 0, end-start)
	var decodedPath string
	var decodedRows []storedLine
	totalBytes := 0
	for n := start; n < end; n++ {
		if text, ok := s.memory[n]; ok {
			totalBytes += len(text)
			if totalBytes > OutputBytes {
				return nil, apiError(History, "Output viewport exceeds byte budget; request fewer lines")
			}
			result = append(result, storedLine{n, text})
			continue
		}
		loc, ok := s.index[n]
		if !ok {
			return nil, apiError(History, "Requested lines are not cached; reload from server")
		}
		rows := decodedRows
		if loc.path != decodedPath {
			f, err := os.Open(loc.path)
			if err != nil {
				return nil, apiError(Storage, "read cached output")
			}
			data, err := io.ReadAll(io.LimitReader(f, 7*OutputBytes+1))
			closeErr := f.Close()
			if closeErr != nil {
				return nil, apiError(Storage, "close cached output")
			}
			if err != nil {
				return nil, apiError(Storage, "read cached output")
			}
			if len(data) > s.limits.DiskBytes || len(data) > 7*OutputBytes || json.Unmarshal(data, &rows) != nil {
				return nil, apiError(Storage, "decode cached output")
			}
			decodedPath = loc.path
			decodedRows = rows
		}
		if loc.index < 0 || loc.index >= len(rows) || rows[loc.index].Number != n {
			return nil, apiError(Storage, "index cached output")
		}
		totalBytes += len(rows[loc.index].Text)
		if totalBytes > OutputBytes {
			return nil, apiError(History, "Output viewport exceeds byte budget; request fewer lines")
		}
		result = append(result, rows[loc.index])
		s.remember(n, rows[loc.index].Text)
	}
	return result, nil
}
func (s *OutputStore) remember(n int, text string) {
	if old, ok := s.memory[n]; ok {
		s.memoryBytes -= len(old) + 64
	}
	s.memory[n] = text
	s.memoryBytes += len(text) + 64
	for s.memoryBytes > s.limits.MemoryBytes {
		oldest := n
		for line := range s.memory {
			if line < oldest {
				oldest = line
			}
		}
		s.memoryBytes -= len(s.memory[oldest]) + 64
		delete(s.memory, oldest)
	}
}

// Accept validates and persists before advancing the live cursor. Older ranges
// and overlapping last lines never move the cursor backwards.
func (s *OutputStore) Accept(c OutputChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return apiError(Storage, "write closed output cache")
	}
	if err := validateChunk(c); err != nil {
		return err
	}
	if s.cursor.Line > 0 && c.Start > s.cursor.Line {
		return apiError(History, "Gap in output; reload the missing range")
	}
	raw := outputLines(c.Text)
	changes := make([]storedLine, 0, len(raw))
	cleaner := &Client{token: s.token}
	for i, line := range raw {
		n := c.Start + i
		text := cleaner.CleanText(line)
		if _, ok := s.index[n]; ok {
			old, err := s.readLines(n, n+1)
			if err != nil {
				return err
			}
			if old[0].Text == text {
				continue
			}
		}
		changes = append(changes, storedLine{n, text})
	}
	if len(changes) > 0 {
		data, err := json.Marshal(changes)
		if err != nil {
			return apiError(Storage, "encode output cache")
		}
		if len(data) > s.limits.DiskBytes || len(data) > 7*OutputBytes {
			return apiError(Storage, "Output chunk exceeds disk budget")
		}
		// Reserve space before writing so even transient disk use stays bounded.
		for s.diskBytes+len(data) > s.limits.DiskBytes || len(s.files) >= s.limits.MaxFiles {
			if err := s.evictOldest(); err != nil {
				return err
			}
		}
		f, err := os.CreateTemp(s.dir, "chunk-")
		if err != nil {
			return apiError(Storage, "allocate output cache file")
		}
		name := f.Name()
		if err = f.Close(); err != nil {
			return apiError(Storage, "close output cache file")
		}
		if err = s.writeFile(name, data, 0600); err != nil {
			if removeErr := os.Remove(name); removeErr != nil {
				return apiError(Storage, "clean failed output cache write")
			}
			return apiError(Storage, "write output cache (check available disk space)")
		}
		s.files = append(s.files, diskChunk{name, len(data)})
		s.diskBytes += len(data)
		for i, row := range changes {
			s.index[row.Number] = location{name, i}
			s.remember(row.Number, row.Text)
		}
		for s.diskBytes > s.limits.DiskBytes || len(s.files) > s.limits.MaxFiles || len(s.index) > s.limits.MaxLines {
			if err := s.evictOldest(); err != nil {
				return err
			}
		}
	}
	s.cursor.Line = max(s.cursor.Line, c.End)
	return nil
}
func (s *OutputStore) evictOldest() error {
	if len(s.files) == 0 {
		return apiError(Storage, "Cannot reserve output cache space")
	}
	oldest := s.files[0]
	if err := os.Remove(oldest.path); err != nil {
		return apiError(Storage, "evict cached output")
	}
	s.files = s.files[1:]
	s.diskBytes -= oldest.bytes
	for n, loc := range s.index {
		if loc.path == oldest.path {
			delete(s.index, n)
			if text, ok := s.memory[n]; ok {
				s.memoryBytes -= len(text) + 64
				delete(s.memory, n)
			}
		}
	}
	return nil
}

func (s *OutputStore) Read(start, end int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", apiError(Storage, "read closed output cache")
	}
	if start < 0 || end < start || end-start > OutputLines {
		return "", apiError(History, "invalid cached output range")
	}
	rows, err := s.readLines(start, end)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(row.Text)
	}
	return b.String(), nil
}
func (s *OutputStore) CachedRange() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]int, 0, len(s.index))
	for n := range s.index {
		keys = append(keys, n)
	}
	sort.Ints(keys)
	if len(keys) == 0 {
		return 0, 0
	}
	return keys[0], keys[len(keys)-1] + 1
}
func (s *OutputStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if err := os.RemoveAll(s.dir); err != nil {
		return apiError(Storage, "remove owned output session cache")
	}
	s.closed = true
	s.index = nil
	s.memory = nil
	s.files = nil
	return nil
}
