package logbuf

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestHandlerCapturesAttributesGroupsAndMatchingRedactedOutput(t *testing.T) {
	var output bytes.Buffer
	buffer := New(10)
	logger := slog.New(NewHandler(slog.NewTextHandler(&output, nil), buffer))
	logger.With("worker", "cache").WithGroup("request").With("attempt", 2).
		Warn("refresh failed", "path", "/movie/42?api_key=private-query", "error", errors.New("tmdb: not found"),
			"authorization", "Bearer private-bearer", "body", "private-response", "status", 404)
	entries := buffer.Entries()
	if len(entries) != 1 || entries[0].Level != "WARN" {
		t.Fatal("warning missing from buffer")
	}
	for _, field := range []string{`worker=cache`, `request.attempt=2`, `request.path=/movie/42`, `request.error="tmdb: not found"`, `request.status=404`} {
		if !strings.Contains(entries[0].Message, field) || !strings.Contains(output.String(), field) {
			t.Fatalf("structured field missing from UI or stdout: %s", field)
		}
	}
	for _, secret := range []string{"private-query", "private-bearer", "private-response"} {
		if strings.Contains(output.String(), secret) || strings.Contains(entries[0].Message, secret) {
			t.Fatal("secret leaked to a logging sink")
		}
	}
}

func TestHandlerPreservesAttributeBindingAndLevelThreshold(t *testing.T) {
	var output bytes.Buffer
	buffer := New(10)
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	logger := slog.New(NewHandler(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: level}), buffer))
	outer := logger.WithGroup("outer").With("fixed", "value")
	inner := outer.WithGroup("inner")
	inner.Debug("hidden")
	inner.Info("one", slog.Group("details", "count", 2))
	outer.Info("two", "count", 3)
	logger.Info("root")
	got := buffer.Entries()
	if len(got) != 3 || strings.Contains(output.String(), "hidden") ||
		!strings.Contains(got[0].Message, "outer.fixed=value") || !strings.Contains(got[0].Message, "outer.inner.details.count=2") ||
		!strings.Contains(got[1].Message, "outer.count=3") || strings.Contains(got[1].Message, "inner") || got[2].Message != "root" {
		t.Fatalf("handler binding/levels differ: %+v", got)
	}
	level.Set(slog.LevelDebug)
	inner.Debug("visible", "value", 42)
	if got := buffer.Entries(); len(got) != 4 || got[3].Level != "DEBUG" || !strings.Contains(got[3].Message, "outer.inner.value=42") {
		t.Fatal("configured DEBUG level did not reach both sinks")
	}
}

func TestHandlerRedactsSecretsInFreeTextAndStructuredObjects(t *testing.T) {
	var output bytes.Buffer
	buffer := New(20)
	logger := slog.New(NewHandler(slog.NewTextHandler(&output, nil), buffer))
	logger.Warn("failed https://user:private-pass@host.example/path?api_key=private-query#private-fragment\napi_key=\"private spaced key\" Authorization: Bearer private-token",
		"error", fmt.Errorf("GET https://user:private-user@other.example/path?token=private-transport: failed"),
		"config", map[string]string{"APIKey": "private-object"},
		"password", "private-password", "headers", slog.GroupValue(slog.String("custom", "private-header")))
	logger.WithGroup("credentials").With("value", "private-group").Error("group test")
	logger.Warn("nested secret keys", "tmdb.api_key", "private-dotted", "X-Api-Key", "private-headerkey", "client_secret", "private-client", "responseBody", "private-raw")
	for _, entry := range buffer.Entries() {
		if strings.Contains(entry.Message, "private-") || strings.Contains(entry.Message, "private spaced") || strings.Contains(entry.Message, "\n") {
			t.Fatalf("unsafe buffered entry: %q", entry.Message)
		}
	}
	if strings.Contains(output.String(), "private-") || strings.Contains(output.String(), "private spaced") {
		t.Fatal("unsafe stdout record")
	}
	if !strings.Contains(output.String(), "https://host.example/path") || !strings.Contains(output.String(), "[redacted]") {
		t.Fatal("useful safe URL or redaction marker lost")
	}
}

func TestHandlerCapturesStandardAndHTTPErrorLogs(t *testing.T) {
	var output bytes.Buffer
	buffer := New(10)
	logger := slog.New(NewHandler(slog.NewTextHandler(&output, nil), buffer))
	old := slog.Default()
	flags, prefix, writer := log.Flags(), log.Prefix(), log.Writer()
	slog.SetDefault(logger)
	defer func() {
		slog.SetDefault(old)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
		log.SetOutput(writer)
	}()
	log.Print("standard application log")
	slog.NewLogLogger(logger.Handler(), slog.LevelError).Print("http: fixture server error")
	entries := buffer.Entries()
	if len(entries) != 2 || !strings.Contains(entries[0].Message, "standard application log") ||
		entries[1].Level != "ERROR" || !strings.Contains(entries[1].Message, "http: fixture server error") {
		t.Fatalf("standard/HTTP logs did not use shared pipeline: %+v", entries)
	}
}

func TestHandlerConcurrentGroupedRecordsRemainIndependent(t *testing.T) {
	var output bytes.Buffer
	buffer := New(100)
	logger := slog.New(NewHandler(slog.NewTextHandler(&output, nil), buffer))
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.WithGroup("job").With("id", i).Info("done", "count", 1)
		}()
	}
	wg.Wait()
	if len(buffer.Entries()) != 50 || strings.Count(output.String(), "\n") != 50 {
		t.Fatal("concurrent logging lost records")
	}
}
