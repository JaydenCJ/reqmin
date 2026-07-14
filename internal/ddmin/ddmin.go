// Package ddmin implements Zeller's ddmin delta-debugging algorithm over an
// abstract item set. It minimizes the set of kept items while a caller-owned
// test keeps reporting "still reproduces", and guarantees the result is
// 1-minimal: removing any single remaining item breaks reproduction.
//
// The package is pure — no I/O, no time — so its behavior is exactly as
// deterministic as the test function it is given.
package ddmin

import "strconv"

// Stats reports how much work a minimization did.
type Stats struct {
	Probes    int // distinct configurations handed to the test function
	CacheHits int // configurations answered from the memo table
}

// Minimize searches for a 1-minimal subset of n items such that
// test(keep) still returns true. test receives a keep-mask of length n.
//
// The full set is assumed to reproduce (the caller verified the baseline).
// If test returns an error, the search stops and the best configuration
// found so far is returned together with that error — useful for request
// budgets, where a partial reduction is still valuable.
func Minimize(n int, test func(keep []bool) (bool, error)) ([]bool, Stats, error) {
	var stats Stats
	cache := map[string]bool{}

	probe := func(idx []int) (bool, error) {
		key := cacheKey(idx)
		if ok, seen := cache[key]; seen {
			stats.CacheHits++
			return ok, nil
		}
		stats.Probes++
		ok, err := test(mask(idx, n))
		if err != nil {
			return false, err
		}
		cache[key] = ok
		return ok, nil
	}

	if n == 0 {
		return nil, stats, nil
	}

	// Cheap first probe: very often nothing matters at all.
	if ok, err := probe(nil); err != nil {
		return mask(fullSet(n), n), stats, err
	} else if ok {
		return mask(nil, n), stats, nil
	}

	cur := fullSet(n)
	gran := 2
	for len(cur) >= 2 {
		chunks := split(cur, gran)
		reduced := false

		// Phase 1: reduce to one subset.
		for _, c := range chunks {
			if len(c) == len(cur) {
				continue
			}
			ok, err := probe(c)
			if err != nil {
				return mask(cur, n), stats, err
			}
			if ok {
				cur = c
				gran = 2
				reduced = true
				break
			}
		}

		// Phase 2: reduce to a complement. At granularity 2 the two
		// complements are the two subsets already tried.
		if !reduced && gran > 2 {
			for i := range chunks {
				comp := without(cur, chunks[i])
				ok, err := probe(comp)
				if err != nil {
					return mask(cur, n), stats, err
				}
				if ok {
					cur = comp
					if gran > 2 {
						gran--
					}
					reduced = true
					break
				}
			}
		}

		// Phase 3: refine granularity, or stop at single items.
		if !reduced {
			if gran >= len(cur) {
				break
			}
			gran = min(gran*2, len(cur))
		}
	}
	return mask(cur, n), stats, nil
}

func fullSet(n int) []int {
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	return all
}

// split partitions idx into k contiguous chunks of near-equal size.
func split(idx []int, k int) [][]int {
	if k > len(idx) {
		k = len(idx)
	}
	chunks := make([][]int, 0, k)
	base := len(idx) / k
	rem := len(idx) % k
	start := 0
	for i := 0; i < k; i++ {
		size := base
		if i < rem {
			size++
		}
		chunks = append(chunks, idx[start:start+size])
		start += size
	}
	return chunks
}

// without returns cur minus the elements of sub (both sorted ascending).
func without(cur, sub []int) []int {
	out := make([]int, 0, len(cur)-len(sub))
	j := 0
	for _, v := range cur {
		if j < len(sub) && sub[j] == v {
			j++
			continue
		}
		out = append(out, v)
	}
	return out
}

func mask(idx []int, n int) []bool {
	m := make([]bool, n)
	for _, i := range idx {
		m[i] = true
	}
	return m
}

func cacheKey(idx []int) string {
	buf := make([]byte, 0, len(idx)*3)
	for _, i := range idx {
		buf = strconv.AppendInt(buf, int64(i), 10)
		buf = append(buf, ',')
	}
	return string(buf)
}
