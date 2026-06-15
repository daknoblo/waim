// Package logbuf provides an in-memory ring buffer of recent log records that
// can be displayed in the UI, in addition to normal slog output to stdout.
package logbuf

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Entry is a single captured log record.
type Entry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// Buffer is a fixed-capacity ring buffer of log entries, safe for concurrent use.
type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	max     int
}

// New returns a Buffer holding at most max entries.
func New(max int) *Buffer {
	if max <= 0 {
		max = 200
	}
	return &Buffer{max: max, entries: make([]Entry, 0, max)}
}

// Add appends an entry, evicting the oldest when at capacity.
func (b *Buffer) Add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) >= b.max {
		copy(b.entries, b.entries[1:])
		b.entries = b.entries[:b.max-1]
	}
	b.entries = append(b.entries, e)
}

// Entries returns a snapshot of the buffered entries, newest last.
func (b *Buffer) Entries() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Entry, len(b.entries))
	copy(out, b.entries)
	return out
}

// Handler is an slog.Handler that records messages into a Buffer while
// delegating formatting/output to a wrapped handler.
type Handler struct {
	inner slog.Handler
	buf   *Buffer
}

// NewHandler wraps inner so that every record is also captured in buf.
func NewHandler(inner slog.Handler, buf *Buffer) *Handler {
	return &Handler{inner: inner, buf: buf}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	h.buf.Add(Entry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
	})
	return h.inner.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{inner: h.inner.WithAttrs(attrs), buf: h.buf}
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{inner: h.inner.WithGroup(name), buf: h.buf}
}
