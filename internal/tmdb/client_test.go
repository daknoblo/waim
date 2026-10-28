package tmdb

import "testing"

func TestClientsShareOneRateBudget(t *testing.T) {
	a := New("fake-a", "en-US", "US", 1)
	b := New("fake-b", "de-DE", "DE", 1)
	if a.limiter != b.limiter {
		t.Fatal("clients multiplied the configured request budget")
	}
	if a.limiter.Limit() != 1 {
		t.Fatal("shared budget ignored configured rate")
	}
}
