package logbuf

// Clear erases user-facing retained records, not the external logging sink.
func (b *Buffer) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	clear(b.entries)
	b.start, b.count = 0, 0
}
