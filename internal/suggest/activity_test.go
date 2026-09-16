package suggest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestSuggestionsReportTitleAndPendingAIAndJoinOnClose(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "invalidate", "error", "unresolved"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			settings := cfg.Get()
			settings.TMDB.APIKey = "secret"
			settings.Scan.TMDBRateLimitRPS = 50
			settings.AI.Enabled = true
			settings.AI.Endpoint = "https://ai.example.test/chat?credential=hidden"
			settings.AI.APIKey = "ai-secret"
			settings.Sources = append(settings.Sources, config.Source{ID: "home", Type: media.Jellyfin, Name: "Home", Enabled: true})
			if err := cfg.Save(settings); err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(filepath.Join(dir, "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			src, _ := cfg.Get().Source("home")
			items := []media.Item{
				{ID: "home/movie", Name: "A readable title", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "7"}, References: []media.Reference{{ID: "home", Type: media.Jellyfin}}},
			}
			if mode == "unresolved" {
				items = append(items, media.Item{ID: "home/unknown", Name: "Unresolved title", Type: media.Movie, References: []media.Reference{{ID: "home", Type: media.Jellyfin}}})
			}
			if err := st.SaveSourceAttempt(context.Background(), src.ID, src.Fingerprint(), &media.Snapshot{Items: items}, ""); err != nil {
				t.Fatal(err)
			}
			tracker := activity.New()
			entered := make(chan activity.State)
			release := make(chan struct{})
			original := http.DefaultTransport
			http.DefaultTransport = recommendationTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"results":[]}`
				if strings.HasSuffix(r.URL.Path, "/recommendations") || r.URL.Host == "ai.example.test" {
					select {
					case entered <- tracker.Snapshot()[2]:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
					select {
					case <-release:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				if r.URL.Host == "ai.example.test" {
					body = `{"choices":[{"message":{"content":"{\"suggestions\":[{\"title\":\"New pick\",\"type\":\"movie\",\"year\":\"2026\",\"reason\":\"A match\"}]}"}}]}`
				}
				status := http.StatusOK
				if mode == "error" && r.URL.Host == "ai.example.test" {
					status = http.StatusServiceUnavailable
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			defer func() { http.DefaultTransport = original }()
			s := New(cfg, st, nil, tracker)
			defer s.Close()
			s.Generate()
			for i := range 2 {
				select {
				case state := <-entered:
					if lease, err := cfg.Gate().TryReset(); err == nil {
						lease.Finish(cfg.Gate().Epoch(), cfg.Gate().FactoryEpoch(), false)
						t.Error("reset admitted while suggestions are in flight")
					}
					if state.Status != activity.Running {
						t.Errorf("not live: %+v", state)
					}
					if mode == "unresolved" && state.Warnings == 0 {
						t.Errorf("catalog warnings not published before long recommendation/AI calls: %+v", state)
					}
					if i == 0 && (state.Phase != activity.Similar || state.Current != "A readable title" || !state.Known || state.Total != 1 || state.Done != 0) {
						t.Errorf("recommendation title missing: %+v", state)
					}
					if i == 1 && (state.Phase != activity.AI || state.Known || state.Query != "" || strings.Contains(fmt.Sprint(state), "secret")) {
						t.Errorf("AI waiting scope misleading or unsafe: %+v", state)
					}
					if i == 1 && mode == "cancel" {
						s.Close()
					} else {
						if i == 1 && mode == "invalidate" {
							s.Invalidate()
						}
						release <- struct{}{}
					}
				case <-time.After(5 * time.Second):
					s.Close()
					t.Fatal("suggestion phase never entered")
				}
			}
			s.wg.Wait()
			state := tracker.Snapshot()[2]
			if mode == "cancel" || mode == "invalidate" {
				if state.Status != activity.Cancelled || s.Running() {
					t.Fatalf("Close did not cancel/join activity: %+v", state)
				}
				return
			}
			result, _ := s.Result()
			if mode == "error" {
				if state.Status != activity.Partial || state.Warnings == 0 || result != nil {
					t.Fatalf("failed AI job not partial: %+v", state)
				}
				if len(state.Diagnostics) != 1 || state.Diagnostics[0].Reason != activity.AIUnavailable || state.Diagnostics[0].Query != "" {
					t.Fatalf("AI failure lacks a safe reason: %+v", state.Diagnostics)
				}
				return
			}
			expected := activity.Completed
			if mode == "unresolved" {
				expected = activity.Partial
				if state.Severity() != activity.Warning || len(state.Diagnostics) != 1 || state.Diagnostics[0].Reason != activity.Unresolved || state.Diagnostics[0].Current != "Unresolved title" {
					t.Fatalf("unresolved suggestion title must remain a concrete activity warning: %+v", state)
				}
			}
			if state.Status != expected || result == nil || len(result.AI) != 1 {
				t.Fatalf("suggestion result not persisted: %+v, %+v", state, result)
			}
		})
	}
}
