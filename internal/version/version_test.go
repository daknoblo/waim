package version

import "testing"

func TestReleaseClassification(t *testing.T) {
	cases := []struct {
		version          string
		release, feature bool
	}{
		{"1.3.0", true, true},
		{"1.10.0", true, true},
		{"1.2.1", true, false},  // patch tags publish an image but no release page
		{"1.2.10", true, false}, // must not be mistaken for a ".0" suffix
		{"dev", false, false},
		{"20260816-1936", false, false},
		{"v1.2.0", false, false},
		{"1.2", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		i := Info{Version: c.version}
		if got := i.IsRelease(); got != c.release {
			t.Errorf("IsRelease(%q) = %v, want %v", c.version, got, c.release)
		}
		if got := i.IsFeatureRelease(); got != c.feature {
			t.Errorf("IsFeatureRelease(%q) = %v, want %v", c.version, got, c.feature)
		}
	}
}
