package logbuf

import (
	"testing"
	"time"
)

func messages(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out
}

func TestBufferKeepsInsertionOrder(t *testing.T) {
	b := New(3)
	for _, m := range []string{"a", "b"} {
		b.Add(Entry{Time: time.Now(), Level: "INFO", Message: m})
	}
	got := messages(b.Entries())
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
}

func TestBufferEvictsOldestWhenFull(t *testing.T) {
	b := New(3)
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		b.Add(Entry{Time: time.Now(), Level: "INFO", Message: m})
	}
	got := messages(b.Entries())
	if len(got) != 3 || got[0] != "c" || got[1] != "d" || got[2] != "e" {
		t.Fatalf("got %v, want [c d e]", got)
	}
}

func TestEntriesReturnsIndependentSnapshot(t *testing.T) {
	b := New(2)
	b.Add(Entry{Message: "a"})
	snap := b.Entries()
	b.Add(Entry{Message: "b"})
	b.Add(Entry{Message: "c"})
	if len(snap) != 1 || snap[0].Message != "a" {
		t.Fatalf("snapshot was mutated: %v", messages(snap))
	}
}
