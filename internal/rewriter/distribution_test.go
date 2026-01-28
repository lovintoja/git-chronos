package rewriter

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestCalculateDistribution_Basic(t *testing.T) {
	files := make([]string, 20)
	for i := range files {
		files[i] = fmt.Sprintf("file_%02d.go", i)
	}

	rng := rand.New(rand.NewSource(42)) // deterministic
	plans, err := CalculateDistribution(files, "2026-01-01", "2026-02-28", rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plans) < 2 {
		t.Errorf("expected at least 2 commits, got %d", len(plans))
	}

	// All files must appear exactly once
	seen := map[string]int{}
	for _, p := range plans {
		for _, f := range p.Files {
			seen[f]++
		}
	}
	for _, f := range files {
		if seen[f] != 1 {
			t.Errorf("file %s appears %d times (want 1)", f, seen[f])
		}
	}
	if len(seen) != len(files) {
		t.Errorf("file count mismatch: got %d unique files, want %d", len(seen), len(files))
	}

	// Timestamps must be sorted and within range
	start, _ := time.Parse(DateLayout, "2026-01-01")
	end, _ := time.Parse(DateLayout, "2026-02-28")
	endInclusive := end.Add(24 * time.Hour)

	for i, p := range plans {
		if p.Timestamp.Before(start) || p.Timestamp.After(endInclusive) {
			t.Errorf("plan %d timestamp %v out of range [%v, %v]", i, p.Timestamp, start, endInclusive)
		}
		if i > 0 && p.Timestamp.Before(plans[i-1].Timestamp) {
			t.Errorf("plan %d timestamp not sorted: %v < %v", i, p.Timestamp, plans[i-1].Timestamp)
		}
		if p.Message == "" {
			t.Errorf("plan %d has empty message", i)
		}
	}
}

func TestCalculateDistribution_SingleMonth(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go"}
	rng := rand.New(rand.NewSource(99))
	plans, err := CalculateDistribution(files, "2026-03-01", "2026-03-31", rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) == 0 {
		t.Error("expected at least 1 plan")
	}
	// All 3 files covered
	covered := map[string]bool{}
	for _, p := range plans {
		for _, f := range p.Files {
			covered[f] = true
		}
	}
	for _, f := range files {
		if !covered[f] {
			t.Errorf("file %s not covered", f)
		}
	}
}

func TestMonthsBetween(t *testing.T) {
	cases := []struct {
		start, end string
		want       int
	}{
		{"2026-01-01", "2026-01-31", 1},
		{"2026-01-01", "2026-02-28", 1},
		{"2026-01-01", "2026-03-01", 2},
		{"2026-01-01", "2026-12-31", 11},
		{"2026-01-01", "2027-01-01", 12},
	}
	for _, c := range cases {
		s, _ := time.Parse(DateLayout, c.start)
		e, _ := time.Parse(DateLayout, c.end)
		got := monthsBetween(s, e)
		if got != c.want {
			t.Errorf("monthsBetween(%s, %s) = %d, want %d", c.start, c.end, got, c.want)
		}
	}
}
