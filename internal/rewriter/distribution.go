package rewriter

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
	"time"
)

// CommitPlan describes a single planned commit: which files, when, and what message.
type CommitPlan struct {
	Files     []string
	Timestamp time.Time
	Message   string
}

// CalculateDistribution decides how many commits to produce and when, given:
//   - files:     all tracked file paths (randomly shuffled internally)
//   - startDate / endDate: inclusive date range strings "YYYY-MM-DD"
//   - rng:       caller-supplied *rand.Rand for deterministic tests
//
// Algorithm:
//   - targetPerMonth = 8 (base cadence)
//   - totalMonths    = months in [startDate, endDate]
//   - baseTotal      = totalMonths * targetPerMonth
//   - clamp baseTotal so average files-per-commit stays in [1, 8]
//   - per month: actualCommits = round(basePerMonth * (1 + uniform(-0.2, +0.2))), min 1
//   - timestamps: random within each month, skewed toward business hours (09:00–18:00)
//   - files: shuffled then split proportionally across commits
func CalculateDistribution(files []string, startDateStr, endDateStr string, rng *rand.Rand) ([]CommitPlan, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no files to distribute")
	}

	start, err := time.Parse(DateLayout, startDateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid start-date: %w", err)
	}
	end, err := time.Parse(DateLayout, endDateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid end-date: %w", err)
	}
	if !end.After(start) {
		return nil, fmt.Errorf("end-date must be after start-date")
	}

	totalMonths := monthsBetween(start, end)
	if totalMonths < 1 {
		totalMonths = 1
	}

	N := len(files)
	const targetPerMonth = 8
	baseTotal := totalMonths * targetPerMonth

	// Clamp: no more than 1 file per commit, no fewer than N/8 commits
	minCommits := int(math.Ceil(float64(N) / 8.0)) // at most 8 files/commit
	maxCommits := N                                  // at least 1 file/commit
	if baseTotal < minCommits {
		baseTotal = minCommits
	}
	if baseTotal > maxCommits {
		baseTotal = maxCommits
	}

	// Distribute commits across months with ±20% deviation
	basePerMonth := float64(baseTotal) / float64(totalMonths)
	monthCounts := make([]int, totalMonths)
	for i := range monthCounts {
		deviation := rng.Float64()*0.4 - 0.2 // uniform [-0.2, +0.2]
		count := int(math.Round(basePerMonth * (1 + deviation)))
		if count < 1 {
			count = 1
		}
		monthCounts[i] = count
	}

	totalCommits := 0
	for _, c := range monthCounts {
		totalCommits += c
	}

	// Shuffle files randomly
	shuffled := make([]string, len(files))
	copy(shuffled, files)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	// Split shuffled files into totalCommits batches (as evenly as possible)
	batches := splitIntoBatches(shuffled, totalCommits)

	// Build CommitPlans
	plans := make([]CommitPlan, 0, totalCommits)
	batchIdx := 0

	for monthIdx, commitCount := range monthCounts {
		monthStart := addMonths(start, monthIdx)
		monthEnd := addMonths(start, monthIdx+1).Add(-time.Second)
		if monthEnd.After(end.Add(24*time.Hour - time.Second)) {
			monthEnd = end.Add(24*time.Hour - time.Second)
		}

		// Generate sorted random timestamps within this month
		timestamps := randomTimestamps(monthStart, monthEnd, commitCount, rng)

		for i := 0; i < commitCount; i++ {
			if batchIdx >= len(batches) {
				break
			}
			batch := batches[batchIdx]
			batchIdx++

			plans = append(plans, CommitPlan{
				Files:     batch,
				Timestamp: timestamps[i],
				Message:   generateMessage(batch, monthIdx),
			})
		}
	}

	return plans, nil
}

// monthsBetween returns the number of whole calendar months from start to end.
func monthsBetween(start, end time.Time) int {
	years := end.Year() - start.Year()
	months := int(end.Month()) - int(start.Month())
	total := years*12 + months
	if total < 1 {
		return 1
	}
	return total
}

// addMonths adds n calendar months to t.
func addMonths(t time.Time, n int) time.Time {
	return t.AddDate(0, n, 0)
}

// splitIntoBatches divides items into n roughly equal slices.
func splitIntoBatches(items []string, n int) [][]string {
	if n <= 0 {
		n = 1
	}
	batches := make([][]string, 0, n)
	total := len(items)
	for i := 0; i < n; i++ {
		lo := (i * total) / n
		hi := ((i + 1) * total) / n
		if lo >= total {
			break
		}
		batches = append(batches, items[lo:hi])
	}
	return batches
}

// randomTimestamps returns commitCount sorted timestamps within [monthStart, monthEnd],
// biased toward business hours (09:00–18:00 local).
func randomTimestamps(monthStart, monthEnd time.Time, count int, rng *rand.Rand) []time.Time {
	duration := monthEnd.Sub(monthStart)
	ts := make([]time.Time, count)
	for i := range ts {
		// Pick a random day offset within the month
		dayOffset := time.Duration(rng.Float64() * float64(duration))
		base := monthStart.Add(dayOffset).UTC().Truncate(24 * time.Hour)
		// Business hours bias: 09:00–18:00
		hour := 9 + rng.Intn(9) // 9..17
		minute := rng.Intn(60)
		second := rng.Intn(60)
		ts[i] = time.Date(base.Year(), base.Month(), base.Day(), hour, minute, second, 0, time.UTC)
		// Clamp within range
		if ts[i].Before(monthStart) {
			ts[i] = monthStart
		}
		if ts[i].After(monthEnd) {
			ts[i] = monthEnd
		}
	}
	// Sort ascending so commits are chronological
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
	return ts
}

// generateMessage produces a short, plausible commit message for a file batch.
func generateMessage(files []string, monthIdx int) string {
	if len(files) == 0 {
		return "chore: update files"
	}

	// Categorise by extension
	extCounts := map[string]int{}
	for _, f := range files {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f), "."))
		if ext == "" {
			ext = "misc"
		}
		extCounts[ext]++
	}

	// Pick dominant extension
	dominant := "misc"
	max := 0
	for ext, count := range extCounts {
		if count > max {
			max = count
			dominant = ext
		}
	}

	prefixes := []string{"feat", "fix", "chore", "refactor", "style", "docs", "perf"}
	prefix := prefixes[(monthIdx+len(files))%len(prefixes)]

	switch dominant {
	case "go":
		return fmt.Sprintf("%s: update Go source files (%d files)", prefix, len(files))
	case "md", "txt", "rst":
		return fmt.Sprintf("docs: update documentation (%d files)", len(files))
	case "json", "yaml", "yml", "toml":
		return fmt.Sprintf("chore: update configuration (%d files)", len(files))
	case "ts", "tsx", "js", "jsx":
		return fmt.Sprintf("%s: update frontend source (%d files)", prefix, len(files))
	case "py":
		return fmt.Sprintf("%s: update Python source (%d files)", prefix, len(files))
	default:
		return fmt.Sprintf("%s: update %s files (%d files)", prefix, dominant, len(files))
	}
}
