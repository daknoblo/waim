package config

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/daknoblo/waim/internal/media"
)

func TestSourceScanIntervalMigrationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.Scan.IntervalMinutes = 37
	settings.Sources = append(settings.Sources, Source{ID: "real", Type: media.Jellyfin, Name: "Real", Enabled: true, Jellyfin: JellyfinSettings{APIKey: "secret"}})
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	disk, err := readStored(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := disk.Sources[1].Jellyfin.APIKeyEnc
	disk.Sources[1].ScanIntervalMinutes = nil
	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	src, _ := cfg.Get().Source("real")
	if src.ScanIntervalMinutes == nil || src.ScanInterval(60) != 37 {
		t.Fatalf("legacy interval not migrated: %+v", src)
	}
	disk, err = readStored(cfg.Path())
	if err != nil || disk.Sources[1].ScanIntervalMinutes == nil || *disk.Sources[1].ScanIntervalMinutes != 37 || disk.Sources[1].Jellyfin.APIKeyEnc != ciphertext {
		t.Fatalf("migration did not persist interval and preserve ciphertext: %v", err)
	}
	settings = cfg.Get()
	settings.Scan.IntervalMinutes = 99
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Get().Source("real"); got.ScanInterval(99) != 37 {
		t.Fatal("persisted source still follows global interval")
	}
	before := src.Fingerprint()
	if err := cfg.UpdateSource("real", src.Revision, func(source *Source) error {
		zero := 0
		source.ScanIntervalMinutes = &zero
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	src, _ = cfg.Get().Source("real")
	if src.ScanIntervalMinutes == nil || src.ScanInterval(99) != 0 || src.Fingerprint() != before {
		t.Fatal("manual interval was lost or changed source identity")
	}
	virtual, _ := cfg.Get().Source(media.VirtualID)
	if virtual.ScanIntervalMinutes != nil || virtual.ScanInterval(99) != 0 {
		t.Fatal("virtual source acquired an interval")
	}
}

func TestSourceScanIntervalCloningAndBounds(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	interval := 12
	src := Source{ID: "real", Type: media.Jellyfin, Name: "Real", Enabled: true, ScanIntervalMinutes: &interval}
	if err := cfg.AddSource(src); err != nil {
		t.Fatal(err)
	}
	interval = 15
	got, _ := cfg.Get().Source("real")
	if got.ScanInterval(60) != 12 {
		t.Fatal("AddSource retained caller's interval pointer")
	}
	*got.ScanIntervalMinutes = 17
	settings := cfg.Get()
	copy := settings.Clone()
	*copy.Sources[1].ScanIntervalMinutes = 19
	if settings.Sources[1].ScanInterval(60) != 12 {
		t.Fatal("Clone shared interval pointer")
	}
	for _, minutes := range []int{-1, MaxSourceScanIntervalMinutes + 1, int(^uint(0) >> 1)} {
		settings.Sources[1].ScanIntervalMinutes = &minutes
		if err := cfg.Save(settings); err == nil {
			t.Fatalf("accepted invalid source interval %d", minutes)
		}
	}
	for _, minutes := range []int{0, maxScanIntervalMinutes + 1, MaxSourceScanIntervalMinutes} {
		settings.Sources[1].ScanIntervalMinutes = &minutes
		if err := cfg.Save(settings); err != nil {
			t.Fatalf("rejected valid source interval %d: %v", minutes, err)
		}
	}
	if (Source{}).ScanInterval(42) != 42 {
		t.Fatal("legacy fallback lost")
	}
}
