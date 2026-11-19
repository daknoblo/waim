package server

import (
	"context"
	"net/http"
	"sort"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
)

type scanView struct {
	settings config.Settings
	catalog  media.Catalog
	saved    *store.ScanRun
	run      *store.ScanRun
	err      error
}

type scanViewKey struct{}

func (s *Server) scanView(ctx context.Context) *scanView {
	if v, ok := ctx.Value(scanViewKey{}).(*scanView); ok {
		return v
	}
	v := &scanView{settings: s.cfg.Get()}
	v.saved, v.err = s.store.LatestSuccessfulRun(ctx)
	if v.err == nil {
		v.catalog, v.err = source.Catalog(ctx, s.store, v.settings, false, nil)
	}
	if v.err == nil {
		v.run = v.saved
		if v.saved != nil && v.saved.Metadata.Basis != "" {
			// Preserve the saved reference list for links to older scan results.
			run := *v.saved
			overlayMembership(&run, v.settings, v.catalog)
			v.run = &run
		}
	}
	return v
}

// Keep the view request-local: the next poll must observe mutations and new scans.
func (s *Server) withScanView(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(context.WithValue(r.Context(), scanViewKey{}, s.scanView(r.Context()))))
	}
}

// currentRun overlays live membership on saved metadata without a server or
// TMDB request. Deletions/disablement affect ownership immediately; gap and
// release evaluations remain explicitly pending until recomputation finishes.
func (s *Server) currentRun(ctx context.Context) (*store.ScanRun, error) {
	if v, ok := ctx.Value(scanViewKey{}).(*scanView); ok {
		return v.run, v.err
	}
	run, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || run == nil || run.Metadata.Basis == "" {
		return run, err
	}
	settings := s.cfg.Get()
	catalog, err := source.Catalog(ctx, s.store, settings, false, nil)
	if err != nil {
		return nil, err
	}
	overlayMembership(run, settings, catalog)
	return run, nil
}

func overlayMembership(run *store.ScanRun, settings config.Settings, catalog media.Catalog) {
	run.Metadata.Pending = run.Metadata.SourcesToken != settings.SourcesToken() || run.Metadata.Revision != catalog.Revision
	if run.FinishedAt != nil && catalog.UpdatedAt.After(*run.FinishedAt) {
		run.Metadata.Pending = true
	}
	unconfirmed := run.Metadata.Pending || len(catalog.Warnings) > 0 || len(run.Metadata.Warnings) > 0
	byKey := map[string]media.Item{}
	byLocal := map[string]media.Item{}
	for _, item := range catalog.Items {
		byKey[item.Key()] = item
		byLocal[item.ID] = item
	}
	stats := []store.MediaStat{}
	statIndex := map[string]int{}
	canonicalMetadata := map[string]bool{}
	for _, m := range run.Media {
		kind := media.Movie
		if m.Type == store.MediaSeries {
			kind = media.Series
		}
		item, ok := byKey[media.Key(kind, m.TMDBID)]
		if !ok && m.TMDBID <= 0 {
			item, ok = byLocal[m.CatalogID]
			ok = ok && item.Type == kind
		}
		if !ok {
			continue
		}
		key := item.Key()
		matchedCanonical := m.TMDBID > 0 && m.TMDBID == item.TMDBID()
		if m.TMDBID != item.TMDBID() || m.WatchOnly != item.WatchOnly || !sameMemberships(m.References, item.References) {
			run.Metadata.Pending = true
		}
		if _, exists := statIndex[key]; exists && (canonicalMetadata[key] || !matchedCanonical) {
			continue
		}
		if m.TMDBID != item.TMDBID() {
			m.MetadataUnavailable = true
		}
		if m.TMDBID <= 0 {
			m.Title, m.Year = item.Name, item.ProductionYear
		}
		m.TMDBID, m.CatalogID = item.TMDBID(), item.ID
		m.References, m.WatchOnly = item.References, item.WatchOnly
		m.Unconfirmed = unconfirmed || run.Metadata.Pending
		if m.Type == store.MediaSeries {
			oldEpisodes := m.Episodes
			present := map[[2]int]bool{}
			for _, ep := range item.Episodes {
				if ep.ParentIndexNumber != nil && ep.IndexNumber != nil && (*ep.ParentIndexNumber != 0 || settings.Scan.IncludeSpecials) {
					present[[2]int{*ep.ParentIndexNumber, *ep.IndexNumber}] = true
				}
			}
			if m.MetadataUnavailable || m.TMDBID <= 0 {
				known := map[int]bool{}
				for _, sn := range m.Seasons {
					known[sn.Number] = true
				}
				for key := range present {
					if !known[key[0]] {
						m.Seasons = append(m.Seasons, store.SeasonStat{Number: key[0]})
						known[key[0]] = true
					}
				}
				sort.Slice(m.Seasons, func(i, j int) bool { return m.Seasons[i].Number < m.Seasons[j].Number })
			}
			m.Episodes, m.Minutes = 0, 0
			for i := range m.Seasons {
				sn := &m.Seasons[i]
				sn.Episodes = 0
				for key := range present {
					if key[0] == sn.Number {
						sn.Episodes++
					}
				}
				m.Episodes += sn.Episodes
				m.Minutes += sn.Episodes * m.Runtime
				for j := range sn.Ratings {
					rating := &sn.Ratings[j]
					rating.Owned = present[[2]int{sn.Number, rating.Number}]
					if rating.Owned && rating.Minutes > 0 {
						m.Minutes += rating.Minutes - m.Runtime
					}
				}
			}
			if oldEpisodes != m.Episodes {
				run.Metadata.Pending = true
			}
		}
		if i, exists := statIndex[key]; exists {
			stats[i] = m
		} else {
			statIndex[key] = len(stats)
			stats = append(stats, m)
		}
		canonicalMetadata[key] = matchedCanonical
	}
	if run.Metadata.Pending {
		for i := range stats {
			stats[i].Unconfirmed = true
		}
		unconfirmed = true
	}
	run.Media = stats
	run.ItemsScanned = 0
	for _, m := range stats {
		if !m.WatchOnly {
			run.ItemsScanned++
		}
	}
	for i := range run.Libraries {
		lib := &run.Libraries[i]
		lib.Total, lib.Scanned = 0, 0
		for _, m := range stats {
			for _, ref := range m.References {
				if ref.LibraryID == lib.ID {
					lib.Total++
					lib.Scanned++
					break
				}
			}
		}
	}
	upcoming := []store.UpcomingItem{}
	for _, u := range run.Upcoming {
		u.Unconfirmed = unconfirmed
		keep := false
		var direct *store.MediaStat
		collectionContext := store.Provenance{WatchOnly: true}
		for _, m := range stats {
			if u.MediaType == store.MediaSeries && m.Type == store.MediaSeries && u.TMDBID == m.TMDBID {
				keep = true
				u.Provenance = m.Provenance
			}
			if u.MediaType == store.MediaMovie {
				if m.Type == store.MediaMovie && u.TMDBID == m.TMDBID {
					direct = &m
					if m.WatchOnly {
						keep = true
					}
				}
				if u.Kind == store.UpcomingCollectionPart && m.Type == store.MediaMovie && m.CollectionID > 0 && m.CollectionID == u.SourceTMDBID {
					keep = true
					collectionContext.ContextReferences = append(collectionContext.ContextReferences, m.References...)
					collectionContext.WatchOnly = collectionContext.WatchOnly && m.WatchOnly
				}
			}
		}
		if keep {
			if u.MediaType == store.MediaMovie {
				u.Provenance = collectionContext
				if direct != nil {
					u.Provenance = direct.Provenance
				}
				u.Unconfirmed = u.Unconfirmed || unconfirmed
				refs := u.References
				if direct == nil {
					refs = u.ContextReferences
				}
				u.LibraryID, u.LibraryName, u.JellyfinID = "", "", ""
				if len(refs) > 0 {
					primary := refs[0]
					for _, ref := range refs {
						if ref.Type != media.Virtual {
							primary = ref
							break
						}
					}
					u.LibraryID, u.LibraryName = primary.LibraryID, primary.LibraryName
					if direct != nil {
						u.JellyfinID = primary.ItemID
					}
				}
			}
			upcoming = append(upcoming, u)
		}
	}
	run.Upcoming = upcoming
}

func sameMemberships(a, b []media.Reference) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(ref media.Reference) string {
		return ref.ID + "\x00" + ref.LibraryID + "\x00" + ref.ItemID
	}
	counts := map[string]int{}
	for _, ref := range a {
		counts[key(ref)]++
	}
	for _, ref := range b {
		k := key(ref)
		if counts[k] == 0 {
			return false
		}
		counts[k]--
	}
	return true
}

func (s *Server) currentFindings(ctx context.Context, run *store.ScanRun) ([]store.Finding, error) {
	fs, err := s.store.FindingsForRun(ctx, run.ID)
	if err != nil || run.Metadata.Basis == "" {
		return fs, err
	}
	out := []store.Finding{}
	for _, f := range fs {
		refs := []media.Reference{}
		watchOnly := true
		unconfirmed := f.Unconfirmed || run.Metadata.Pending
		for _, m := range run.Media {
			matches := m.Type == f.MediaType && m.TMDBID == f.TMDBID
			if f.Kind == store.KindMissingCollection {
				matches = m.Type == store.MediaMovie && m.CollectionID == f.TMDBID
			}
			if !matches {
				continue
			}
			if f.Kind == store.KindMissingMovie && !m.WatchOnly {
				continue
			}
			refs = append(refs, m.References...)
			watchOnly = watchOnly && m.WatchOnly
			unconfirmed = unconfirmed || m.Unconfirmed
		}
		if len(refs) == 0 {
			continue
		}
		f.WatchOnly = watchOnly
		f.Unconfirmed = unconfirmed
		if f.Kind == store.KindMissingCollection {
			f.ContextReferences = refs
		} else {
			f.References = refs
		}
		out = append(out, f)
	}
	return out, nil
}
