// Command seed fills a waim database with a synthetic scan run so the UI can be
// reviewed without a Jellyfin server. Development tool; never run it against a
// database you care about.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/daknoblo/waim/internal/store"
)

const (
	libMovies = "lib-movies"
	libSeries = "lib-series"
)

var seriesTitles = []string{
	"Chronicles of the Deep", "Neon Harbour", "The Quiet Frontier", "Paper Lanterns",
	"Signal Lost", "Harbourlight", "Ashfall", "The Gilded Circuit", "Nightporter",
	"Salt Flats", "Vector Prime", "The Long Thaw", "Copper Sky", "Meridian",
	"Understory", "Glass Cathedral", "Riftwalkers", "The Amber Room",
}

var movieTitles = []string{
	"The Cartographer", "Tidewater", "Midnight in Vienna", "Salt & Static",
	"Der letzte Zug", "Kite Season", "Glasshouse", "Northbound", "Foxglove",
	"The Sixth Harbour", "Anvil", "Pale Horizon", "Rook", "Winterlight",
}

var genreNames = []string{
	"Drama", "Thriller", "Science Fiction", "Comedy", "Crime", "Adventure", "Horror", "Documentary",
}

func main() {
	out := flag.String("out", "appdata", "data directory to write waim.db into")
	force := flag.Bool("force", false, "overwrite an existing database")
	flag.Parse()

	path := filepath.Join(*out, "waim.db")
	if _, err := os.Stat(path); err == nil && !*force {
		log.Fatalf("seed: %s already exists; pass -force to overwrite", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("seed: %v", err)
	}
	if *force {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Fatalf("seed: %v", err)
			}
		}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("seed: %v", err)
	}
	st, err := store.Open(path)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer func() { _ = st.Close() }()

	if err := seed(context.Background(), st, path); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func seed(ctx context.Context, st *store.Store, path string) error {
	rng := rand.New(rand.NewSource(7)) // fixed seed keeps screenshots comparable
	now := time.Now()

	var (
		media    []store.MediaStat
		findings []store.Finding
		upcoming []store.UpcomingItem
	)

	for i, title := range seriesTitles {
		id := int64(400 + i)
		stat := store.MediaStat{
			Type: store.MediaSeries, Title: title, Year: 2010 + i%14,
			Rating: 6.2 + float64(i%38)/10, Runtime: 42 + i%20,
			Genres:    []string{genreNames[i%len(genreNames)], genreNames[(i+3)%len(genreNames)]},
			LibraryID: libSeries, LibraryName: "Series", TMDBID: id,
			JellyfinID: fmt.Sprintf("s-%d", id), Language: "en", Country: "US",
		}
		seasons := 2 + i%4
		for s := 1; s <= seasons; s++ {
			total := 8 + i%5
			owned := total
			if s == seasons && i%3 == 0 {
				owned = total / 2
			}
			season := store.SeasonStat{
				Number: s, Episodes: owned, Total: total, Rating: 6.5 + float64((i+s)%30)/10,
			}
			for e := 1; e <= total; e++ {
				season.Ratings = append(season.Ratings, store.EpisodeRating{
					Number: e, Title: fmt.Sprintf("Episode %d", e),
					Rating: 6.0 + float64((i+s+e)%40)/10, Minutes: stat.Runtime, Owned: e <= owned,
				})
			}
			stat.Seasons = append(stat.Seasons, season)
			stat.Episodes += owned
			stat.TotalEpisodes += total
			stat.Minutes += owned * stat.Runtime

			if owned < total {
				sn := s
				detail, _ := json.Marshal(map[string]any{
					"seasonNumber": s, "episodeCount": total,
					"missingEpisodes": []int{owned + 1, owned + 2},
					// Recent air dates so the retrospective view has content.
					"airDates": map[string]string{
						strconv.Itoa(owned + 1): now.AddDate(0, 0, -(7 + rng.Intn(60))).Format("2006-01-02"),
						strconv.Itoa(owned + 2): now.AddDate(0, 0, -(1 + rng.Intn(6))).Format("2006-01-02"),
					},
				})
				findings = append(findings, store.Finding{
					Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries,
					LibraryID: libSeries, LibraryName: "Series", Title: title, TMDBID: id,
					JellyfinID: stat.JellyfinID, SeasonNumber: &sn,
					Summary:   fmt.Sprintf("season %d is missing %d episodes", s, total-owned),
					Details:   string(detail),
					CreatedAt: now,
				})
			}
		}
		media = append(media, stat)

		// Roughly every other series has an announced season.
		if i%2 == 0 {
			start := now.AddDate(0, 0, 3+rng.Intn(320))
			for e := 1; e <= 4+rng.Intn(6); e++ {
				upcoming = append(upcoming, store.UpcomingItem{
					Kind: store.UpcomingEpisode, MediaType: store.MediaSeries,
					Title: fmt.Sprintf("Episode %d", e), SourceTitle: title,
					SourceTMDBID: id, TMDBID: id, SeasonNumber: seasons + 1, EpisodeNumber: e,
					ReleaseDate: start.AddDate(0, 0, (e-1)*7).Format("2006-01-02"),
					LibraryID:   libSeries, LibraryName: "Series", JellyfinID: stat.JellyfinID,
				})
			}
		}
	}

	for i, title := range movieTitles {
		id := int64(300 + i)
		m := store.MediaStat{
			Type: store.MediaMovie, Title: title, Year: 1998 + i%28,
			Rating: 5.9 + float64(i%40)/10, Runtime: 92 + i%60,
			Genres:    []string{genreNames[(i+2)%len(genreNames)]},
			LibraryID: libMovies, LibraryName: "Movies", TMDBID: id,
			JellyfinID: fmt.Sprintf("m-%d", id), Language: "en", Country: "US",
		}
		if i < 6 {
			m.CollectionID = 900 + int64(i%3)
			m.CollectionName = movieTitles[i%3] + " Collection"
		}
		media = append(media, m)
	}

	for c := 0; c < 5; c++ {
		var date string
		if c < 4 { // the last one stays undated, to cover the "date to be announced" bucket
			date = now.AddDate(0, 0, 20+c*70).Format("2006-01-02")
		}
		upcoming = append(upcoming, store.UpcomingItem{
			Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie,
			Title:        movieTitles[c] + " II",
			SourceTitle:  movieTitles[c] + " Collection",
			SourceTMDBID: 900 + int64(c), TMDBID: 320 + int64(c),
			ReleaseDate: date, LibraryID: libMovies, LibraryName: "Movies",
		})
	}
	collectionDetail, _ := json.Marshal(map[string]any{
		"collectionId": 900, "collectionName": "The Cartographer Collection",
		"missingParts": []map[string]any{
			{
				"tmdbId": 309, "title": "The Cartographer: Northern Light",
				"year": "2008", "rating": 8.5,
				"releaseDate": now.AddDate(0, 0, -34).Format("2006-01-02"),
			},
		},
	})
	findings = append(findings, store.Finding{
		Kind: store.KindMissingCollection, MediaType: store.MediaMovie,
		LibraryID: libMovies, LibraryName: "Movies", Title: "The Cartographer Collection",
		TMDBID: 900, JellyfinID: "m-300", Summary: "collection is missing 1 entry",
		Details: string(collectionDetail), CreatedAt: now,
	})

	// Earlier runs so the growth trend has something to plot.
	for i := 5; i > 0; i-- {
		id, err := st.StartScanRun(ctx)
		if err != nil {
			return err
		}
		if err := st.FinishScanRun(ctx, id, store.StatusSuccess, "", 2,
			len(media)-i, len(findings)+i, nil, nil, nil); err != nil {
			return err
		}
	}

	runID, err := st.StartScanRun(ctx)
	if err != nil {
		return err
	}
	if err := st.AddFindings(ctx, runID, findings); err != nil {
		return err
	}
	libs := []store.LibrarySummary{
		{ID: libMovies, Name: "Movies", Scanned: len(movieTitles), Total: len(movieTitles), Missing: 1},
		{ID: libSeries, Name: "Series", Scanned: len(seriesTitles), Total: len(seriesTitles), Missing: len(findings) - 1},
	}
	if err := st.FinishScanRun(ctx, runID, store.StatusSuccess, "", 2,
		len(media), len(findings), libs, media, upcoming); err != nil {
		return err
	}

	fmt.Printf("seed: wrote %s (%d titles, %d findings, %d announced releases)\n",
		path, len(media), len(findings), len(upcoming))
	return nil
}
