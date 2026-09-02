package hardware

import (
	"os"
	"path/filepath"
	"testing"
)

// writeDRMFixture builds a sysfs-shaped DRM tree. hwmon values use the kernel's
// units: millidegrees and microwatts.
func writeDRMFixture(t *testing.T, cards map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for card, files := range cards {
		for rel, content := range files {
			path := filepath.Join(root, card, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}
	return root
}

// strixCard mirrors what this box publishes.
func strixCard() map[string]string {
	return map[string]string{
		"device/vendor":                       "0x1002\n",
		"device/gpu_busy_percent":             "37\n",
		"device/hwmon/hwmon12/temp1_input":    "47000\n",
		"device/hwmon/hwmon12/power1_average": "11057000\n",
	}
}

func TestAMDGPUStats_StrixHalo(t *testing.T) {
	root := writeDRMFixture(t, map[string]map[string]string{"card1": strixCard()})

	s, ok := amdGPUStatsFrom(root)
	if !ok {
		t.Fatal("expected the AMD card to be found")
	}
	if s.UtilizationPct != 37 {
		t.Errorf("utilization = %v, want 37", s.UtilizationPct)
	}
	// Millidegrees and microwatts must be converted, not reported raw.
	if !s.HasTemperature || s.TemperatureC != 47.0 {
		t.Errorf("temperature = %v (has=%v), want 47.0", s.TemperatureC, s.HasTemperature)
	}
	if !s.HasPower || s.PowerW < 11.05 || s.PowerW > 11.06 {
		t.Errorf("power = %v (has=%v), want ~11.057", s.PowerW, s.HasPower)
	}
}

// The DRM class directory is full of connector entries like "card1-DP-1" that
// are not GPUs and have no device/vendor node.
func TestAMDGPUStats_IgnoresConnectorDirs(t *testing.T) {
	root := writeDRMFixture(t, map[string]map[string]string{
		"card1-DP-1":     {"device/vendor": "0x1002\n"},
		"card1-HDMI-A-1": {"device/vendor": "0x1002\n"},
		"card1":          strixCard(),
	})
	s, ok := amdGPUStatsFrom(root)
	if !ok || s.UtilizationPct != 37 {
		t.Errorf("must read the real card, got ok=%v util=%v", ok, s.UtilizationPct)
	}
}

func TestAMDGPUStats_SkipsNonAMDVendor(t *testing.T) {
	root := writeDRMFixture(t, map[string]map[string]string{
		"card0": {"device/vendor": "0x10de\n", "device/gpu_busy_percent": "99\n"},
	})
	if _, ok := amdGPUStatsFrom(root); ok {
		t.Error("a non-AMD card must not be reported as an AMD GPU")
	}
}

// A card with no hwmon must still report utilization, and must mark the sensors
// missing rather than reporting a plausible-looking 0 degrees.
func TestAMDGPUStats_MissingHwmonIsDistinguishable(t *testing.T) {
	root := writeDRMFixture(t, map[string]map[string]string{
		"card1": {"device/vendor": "0x1002\n", "device/gpu_busy_percent": "12\n"},
	})
	s, ok := amdGPUStatsFrom(root)
	if !ok {
		t.Fatal("card should still be found")
	}
	if s.UtilizationPct != 12 {
		t.Errorf("utilization = %v, want 12", s.UtilizationPct)
	}
	if s.HasTemperature {
		t.Error("a missing sensor must not report HasTemperature")
	}
	if s.HasPower {
		t.Error("a missing sensor must not report HasPower")
	}
}

// A genuine 0 must be distinguishable from an absent reading.
func TestAMDGPUStats_ZeroReadingIsNotMissing(t *testing.T) {
	card := strixCard()
	card["device/hwmon/hwmon12/temp1_input"] = "0\n"
	root := writeDRMFixture(t, map[string]map[string]string{"card1": card})

	s, _ := amdGPUStatsFrom(root)
	if !s.HasTemperature {
		t.Error("a real 0 reading must still set HasTemperature")
	}
	if s.TemperatureC != 0 {
		t.Errorf("temperature = %v, want 0", s.TemperatureC)
	}
}

func TestAMDGPUStats_NoDRMTree(t *testing.T) {
	if _, ok := amdGPUStatsFrom(filepath.Join(t.TempDir(), "absent")); ok {
		t.Error("a box with no DRM tree must report ok=false")
	}
}

func TestIsCardDir(t *testing.T) {
	cases := map[string]bool{
		"card0": true, "card1": true, "card10": true,
		"card1-DP-1": false, "card1-HDMI-A-1": false,
		"card": false, "renderD128": false, "version": false,
	}
	for name, want := range cases {
		if got := isCardDir(name); got != want {
			t.Errorf("isCardDir(%q) = %v, want %v", name, got, want)
		}
	}
}

// Cards must be visited in numeric order relative to one another, so card10
// does not precede card2. Non-card entries may sort anywhere -- the scan skips
// them -- so only the relative order of the real cards is asserted.
func TestSortCardNames_Numeric(t *testing.T) {
	names := []string{"card10", "card2", "card1", "card1-DP-1", "renderD128"}
	sortCardNames(names)

	var cards []string
	for _, n := range names {
		if isCardDir(n) {
			cards = append(cards, n)
		}
	}
	want := []string{"card1", "card2", "card10"}
	if len(cards) != len(want) {
		t.Fatalf("expected %d cards, got %v", len(want), cards)
	}
	for i := range want {
		if cards[i] != want[i] {
			t.Errorf("card order = %v, want %v", cards, want)
			break
		}
	}
}
