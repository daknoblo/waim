package server

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/logbuf"
)

func TestOutputLogIncludesContainerFieldsWithoutSecrets(t *testing.T) {
	s := featureServer(t)
	var container bytes.Buffer
	s.logs = logbuf.New(10)
	logger := slog.New(logbuf.NewHandler(slog.NewTextHandler(&container, nil), s.logs))
	logger.Warn("refresher: refresh failed", "path", "/movie/42?api_key=private-secret", "error", errors.New("tmdb: not found"))
	logger.Info("tmdb cache refreshed", "refreshed", 5, "failed", 2, "total", 70)
	for _, locale := range []string{"en", "de"} {
		html := diagRequest(s, "/logs", locale, "").Body.String()
		for _, field := range []string{"path=/movie/42", "tmdb: not found", "refreshed=5", "failed=2", "total=70", s.catalog.For(locale).T("logs.retention", 10)} {
			if !strings.Contains(html, field) {
				t.Fatal("UI dropped log detail or buffer explanation: " + field)
			}
		}
		if strings.Contains(html, "private-secret") || strings.Contains(container.String(), "private-secret") {
			t.Fatal("credentials leaked through output logs")
		}
		warn := diagRequest(s, "/partials/log?level=WARN", locale, "").Body.String()
		if !strings.Contains(warn, "tmdb: not found") || strings.Contains(warn, "refreshed=5") {
			t.Fatal("level filtering lost details or included INFO rows")
		}
	}
}
