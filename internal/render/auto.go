package render

import (
	"math"
	"sort"

	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// autoRings is how many quantile rings the auto scale places before its last ring.
const autoRings = 8

// niceMinutes are the ring values the auto scale rounds to, so labels read 30m, 6h, 2d, 90d, 1y,
// 1.5y. Half years up to 3 years: long-lived clusters keep most resources between 1 and 3 years.
var niceMinutes = func() []int {
	const h, d, y = 60, 24 * 60, 365 * 24 * 60
	out := []int{5, 10, 15, 30, 1 * h, 2 * h, 3 * h, 6 * h, 12 * h}
	for _, n := range []int{1, 2, 3, 5, 7, 10, 14, 21, 30, 45, 60, 90, 120, 180, 270} {
		out = append(out, n*d)
	}
	out = append(out, y, y*3/2, 2*y, y*5/2)
	for _, n := range []int{3, 4, 5, 6, 7, 8, 10, 12, 15, 20, 25, 30, 40, 50, 75, 100} {
		out = append(out, n*y)
	}
	return out
}()

// AutoRings returns the rings of the auto scale, in minutes: one ring at each of autoRings
// quantiles of the resource ages on the axis (created, or changed with created as fallback), the
// last one at the oldest resource, each rounded to the nearest value of niceMinutes on a log
// scale; rings that round to the same value merge. A resource older than the rounded last ring
// lands in the compressed band past it, at most 1.41 times its age. Ages are measured from each
// snapshot's collected_at, over every snapshot of the view. Resources of system namespaces are
// left out like in the default view, unless nothing else is left. Nil when the snapshots hold no
// resources.
func AutoRings(snaps []*snapshot.Snapshot, axis string) []int {
	var apps, all []float64
	for _, s := range snaps {
		system := map[string]bool{}
		for _, n := range s.Namespaces {
			system[n.Name] = n.System
		}
		for _, r := range s.Resources {
			t := r.Created
			if axis == "changed" && !r.Changed.IsZero() {
				t = r.Changed
			}
			age := math.Max(0, s.CollectedAt.Sub(t).Minutes())
			all = append(all, age)
			if !system[r.NS] {
				apps = append(apps, age)
			}
		}
	}
	ages := apps
	if len(ages) == 0 {
		ages = all
	}
	if len(ages) == 0 {
		return nil
	}
	sort.Float64s(ages)
	var out []int
	add := func(m int) {
		if len(out) == 0 || m > out[len(out)-1] {
			out = append(out, m)
		}
	}
	for k := 1; k <= autoRings; k++ {
		add(nearestNice(ages[k*(len(ages)-1)/autoRings]))
	}
	return out
}

// nearestNice rounds minutes to the closest value of niceMinutes on a log scale.
func nearestNice(minutes float64) int {
	best := niceMinutes[0]
	bd := math.Inf(1)
	x := math.Log(math.Max(minutes, 1))
	for _, n := range niceMinutes {
		if d := math.Abs(math.Log(float64(n)) - x); d < bd {
			best, bd = n, d
		}
	}
	return best
}
