package layout

import (
	"fmt"
	"math"
)

// Normalize turns optional sizes into shares that sum to 100. Unset sizes
// split what's left equally. If every size is set, they're scaled to fit.
func Normalize(sizes []Size) ([]float64, error) {
	var total float64
	unset := 0
	for _, s := range sizes {
		if s.Set {
			total += s.Pct
		} else {
			unset++
		}
	}

	out := make([]float64, len(sizes))
	if unset == 0 {
		for i, s := range sizes {
			out[i] = s.Pct * 100 / total
		}
		return out, nil
	}
	if total >= 100 {
		return nil, fmt.Errorf("sizes add up to %g%%, leaving nothing for %d pane(s) without a size", total, unset)
	}
	rest := (100 - total) / float64(unset)
	for i, s := range sizes {
		if s.Set {
			out[i] = s.Pct
		} else {
			out[i] = rest
		}
	}
	return out, nil
}

// SplitPercents converts shares into the `split-window -l N%` values needed
// to carve them out one at a time. tmux sizes each new pane as a percentage
// of the pane being split, which is everything not yet carved off.
func SplitPercents(shares []float64) []int {
	var out []int
	for i := 1; i < len(shares); i++ {
		var remaining float64
		for _, s := range shares[i:] {
			remaining += s
		}
		pct := int(math.Round(remaining / (remaining + shares[i-1]) * 100))
		out = append(out, min(max(pct, 1), 99))
	}
	return out
}
