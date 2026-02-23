package rewriter

import (
	"testing"
	"time"
)

func TestTimeInterpolation(t *testing.T) {
	start, _ := time.Parse(DateLayout, "2024-01-01")
	end, _ := time.Parse(DateLayout, "2024-12-31")
	duration := end.Sub(start)

	cases := []struct {
		n        int
		i        int
		wantFrac float64 // expected fraction of duration (0.0 to 1.0)
	}{
		{1, 0, 0.0},  // single chunk → startDate
		{2, 0, 0.0},  // first of two → startDate
		{2, 1, 1.0},  // last of two → endDate
		{3, 0, 0.0},  // first of three
		{3, 1, 0.5},  // middle of three
		{3, 2, 1.0},  // last of three
	}

	for _, c := range cases {
		var fraction float64
		if c.n == 1 {
			fraction = 0.0
		} else {
			fraction = float64(c.i) / float64(c.n-1)
		}
		got := start.Add(time.Duration(float64(duration) * fraction))
		want := start.Add(time.Duration(float64(duration) * c.wantFrac))
		if !got.Equal(want) {
			t.Errorf("n=%d i=%d: got %v, want %v", c.n, c.i, got, want)
		}
	}
}
