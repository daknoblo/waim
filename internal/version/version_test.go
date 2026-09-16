package version

import "testing"

func TestDevelopmentChannelClassification(t *testing.T) {
	for _, tc := range []struct {
		version string
		dev     bool
	}{
		{"dev", true},
		{"dev-20260915-1616", true},
		{"stable-20260915-1616", false},
		{"1.5.0", false},
		{"1.5.1", false},
		{"demo", false},
		{"20260915-1616", false},
		{"", false},
	} {
		if got := (Info{Version: tc.version}).IsDevelopment(); got != tc.dev {
			t.Errorf("IsDevelopment(%q) = %v, want %v", tc.version, got, tc.dev)
		}
	}
}

func TestReleaseClassification(t *testing.T) {
	cases := []struct {
		version string
		release bool
	}{
		{"1.3.0", true},
		{"1.10.0", true},
		{"1.2.1", true}, // patches are stable releases too
		{"1.2.10", true},
		{"dev", false},
		{"20260816-1936", false},
		{"dev-20260915-1200", false},
		{"stable-20260915-1200", false},
		{"v1.2.0", false},
		{"1.2", false},
		{"", false},
	}
	for _, c := range cases {
		i := Info{Version: c.version}
		if got := i.IsRelease(); got != c.release {
			t.Errorf("IsRelease(%q) = %v, want %v", c.version, got, c.release)
		}
	}
}
