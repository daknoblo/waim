// Package tmdbcache adapts the SQLite store to the tmdb.Cache interface so the
// TMDB client can persist and reuse responses.
package tmdbcache

import (
	"context"

	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

// New returns a tmdb.Cache backed by the given store.
func New(st *store.Store) tmdb.Cache {
	return adapter{st: st}
}

type adapter struct {
	st *store.Store
}

func (a adapter) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return a.st.TMDBCacheGet(ctx, key)
}

func (a adapter) Put(ctx context.Context, key string, payload []byte) error {
	return a.st.TMDBCachePut(ctx, key, payload)
}
