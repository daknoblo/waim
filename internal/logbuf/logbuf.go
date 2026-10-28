// Package logbuf provides an in-memory ring buffer of recent log records that
// can be displayed in the UI, in addition to normal slog output to stdout.
package logbuf

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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
	start   int // index of the oldest entry
	count   int // number of entries currently held
	max     int
}

// New returns a Buffer holding at most max entries.
func New(max int) *Buffer {
	if max <= 0 {
		max = 200
	}
	return &Buffer{max: max, entries: make([]Entry, max)}
}

// Add appends an entry, evicting the oldest when at capacity. Insertion is O(1)
// so debug-level logging during a scan does not shift the whole buffer.
func (b *Buffer) Add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[(b.start+b.count)%b.max] = e
	if b.count < b.max {
		b.count++
		return
	}
	b.start = (b.start + 1) % b.max
}

// Entries returns a snapshot of the buffered entries, newest last.
func (b *Buffer) Entries() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Entry, b.count)
	n := copy(out, b.entries[b.start:min(b.start+b.count, b.max)])
	copy(out[n:], b.entries[:b.count-n])
	return out
}

// Handler is an slog.Handler that records messages into a Buffer while
// delegating the same sanitized record to the container output handler.
type Handler struct {
	inner  slog.Handler
	buf    *Buffer
	attrs  []slog.Attr
	groups []string
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
	record := slog.NewRecord(r.Time, r.Level, safeText(r.Message), r.PC)
	record.AddAttrs(h.attrs...)
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, safeAttr(a))
		return true
	})
	record.AddAttrs(groupAttrs(h.groups, attrs)...)
	var formatted bytes.Buffer
	formatter := slog.NewTextHandler(&formatted, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && (a.Key == slog.TimeKey || a.Key == slog.LevelKey || a.Key == slog.MessageKey) {
				return slog.Attr{}
			}
			return a
		},
	})
	formatErr := formatter.Handle(ctx, record)
	message := record.Message
	if fields := strings.TrimSpace(formatted.String()); fields != "" {
		message += " " + fields
	}
	h.buf.Add(Entry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: message,
	})
	return errors.Join(formatErr, h.inner.Handle(ctx, record))
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cleaned := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		cleaned = append(cleaned, safeAttr(a))
	}
	bound := append([]slog.Attr(nil), h.attrs...)
	bound = append(bound, groupAttrs(h.groups, cleaned)...)
	return &Handler{inner: h.inner, buf: h.buf, attrs: bound, groups: h.groups}
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := append(append([]string(nil), h.groups...), safeText(name))
	return &Handler{inner: h.inner, buf: h.buf, attrs: h.attrs, groups: groups}
}

func groupAttrs(groups []string, attrs []slog.Attr) []slog.Attr {
	if len(attrs) == 0 {
		return nil
	}
	for i := len(groups) - 1; i >= 0; i-- {
		attrs = []slog.Attr{safeAttr(slog.Attr{Key: groups[i], Value: slog.GroupValue(attrs...)})}
	}
	return attrs
}

func (b *Buffer) Capacity() int { return b.max }
