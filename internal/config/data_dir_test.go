package config

import "testing"

func TestDataDir(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
	}{
		{"local default", "", "appdata"},
		{"blank override", " \t ", "appdata"},
		{"container default", "/data", "/data"},
		{"custom path", "/var/lib/waim", "/var/lib/waim"},
		{"relative host path", "./appdata", "./appdata"},
		{"trim whitespace", " /data \t", "/data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WAIM_DATA_DIR", tc.value)
			if got := DataDir(); got != tc.want {
				t.Fatalf("DataDir() = %q, want %q", got, tc.want)
			}
		})
	}
}
