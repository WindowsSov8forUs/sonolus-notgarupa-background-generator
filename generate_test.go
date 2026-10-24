package background

import (
	"context"
	"strings"
	"testing"
)

// Generate reports errors instead of exiting: offline without a cache every
// download fails.
func TestGenerateOfflineWithoutCache(t *testing.T) {
	err := Generate(context.Background(), Options{Source: Source{CacheDir: t.TempDir(), Offline: true}}, func(*Result) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("want an offline error, got %v", err)
	}
}

func TestItems(t *testing.T) {
	items, err := Items()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range items {
		if seen[n] {
			t.Errorf("duplicate %s", n)
		}
		seen[n] = true
	}
	for _, want := range []string{"garupa-skin00", "garupa-habahiro", "garupa-skin02"} {
		if !seen[want] {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"garupa-skin01", "garupa-skin_teamlivefestival"} {
		if seen[unwanted] {
			t.Errorf("unexpected %s", unwanted)
		}
	}
}

func TestPivot(t *testing.T) {
	for p, want := range map[int][2]float64{0: {0, 1}, 4: {0.5, 0.5}, 8: {1, 0}, 3: {0, 0.5}} {
		x, y := pivot(p)
		if x != want[0] || y != want[1] {
			t.Errorf("pivot %d = %v,%v", p, x, y)
		}
	}
}
