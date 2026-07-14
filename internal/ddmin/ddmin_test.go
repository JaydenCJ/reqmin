// ddmin tests: the algorithm must find 1-minimal subsets for the failure
// shapes that occur in real requests — one culprit, interacting pairs,
// everything mattering, nothing mattering — and must degrade gracefully
// when the probe budget runs out.
package ddmin

import (
	"errors"
	"reflect"
	"testing"
)

// needs builds a test function that reproduces iff every index in required
// is kept, and records how many probes ran.
func needs(required ...int) (func(keep []bool) (bool, error), *int) {
	probes := 0
	fn := func(keep []bool) (bool, error) {
		probes++
		for _, r := range required {
			if !keep[r] {
				return false, nil
			}
		}
		return true, nil
	}
	return fn, &probes
}

func keptIndices(mask []bool) []int {
	var out []int
	for i, k := range mask {
		if k {
			out = append(out, i)
		}
	}
	return out
}

func TestSingleCulpritIsFound(t *testing.T) {
	test, _ := needs(17)
	mask, _, err := Minimize(40, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	if got := keptIndices(mask); !reflect.DeepEqual(got, []int{17}) {
		t.Fatalf("kept %v, want [17]", got)
	}
}

func TestInteractingPairIsFound(t *testing.T) {
	// Neither item reproduces alone — the classic "Authorization plus
	// exactly one Accept header" interaction.
	test, _ := needs(3, 29)
	mask, _, err := Minimize(32, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	if got := keptIndices(mask); !reflect.DeepEqual(got, []int{3, 29}) {
		t.Fatalf("kept %v, want [3 29]", got)
	}
}

func TestScatteredTripleIsFound(t *testing.T) {
	test, _ := needs(0, 15, 30)
	mask, _, err := Minimize(31, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	if got := keptIndices(mask); !reflect.DeepEqual(got, []int{0, 15, 30}) {
		t.Fatalf("kept %v, want [0 15 30]", got)
	}
}

func TestNothingNeededCostsOneProbe(t *testing.T) {
	test, probes := needs() // always reproduces
	mask, stats, err := Minimize(50, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	if len(keptIndices(mask)) != 0 {
		t.Fatalf("kept %v, want nothing", keptIndices(mask))
	}
	if *probes != 1 || stats.Probes != 1 {
		t.Fatalf("probes = %d (stats %d), want the single empty-set probe", *probes, stats.Probes)
	}
	// And the degenerate zero-item case never probes at all.
	mask, stats, err = Minimize(0, func([]bool) (bool, error) {
		t.Fatal("test must not be called for n=0")
		return false, nil
	})
	if err != nil || len(mask) != 0 || stats.Probes != 0 {
		t.Fatalf("mask=%v stats=%+v err=%v", mask, stats, err)
	}
}

func TestEverythingNeededKeepsEverything(t *testing.T) {
	n := 8
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	test, _ := needs(all...)
	mask, _, err := Minimize(n, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	if got := keptIndices(mask); !reflect.DeepEqual(got, all) {
		t.Fatalf("kept %v, want all %d items", got, n)
	}
}

func TestResultIsOneMinimal(t *testing.T) {
	// Whatever ddmin returns, removing any single kept item must break
	// reproduction — the formal guarantee the report relies on.
	test, _ := needs(2, 5, 11)
	mask, _, err := Minimize(12, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	kept := keptIndices(mask)
	for _, drop := range kept {
		probe := append([]bool(nil), mask...)
		probe[drop] = false
		if ok, _ := test(probe); ok {
			t.Fatalf("kept set %v is not 1-minimal: %d is removable", kept, drop)
		}
	}
}

func TestMemoizationSkipsRepeatConfigurations(t *testing.T) {
	seen := map[string]int{}
	test := func(keep []bool) (bool, error) {
		key := ""
		for _, k := range keep {
			if k {
				key += "1"
			} else {
				key += "0"
			}
		}
		seen[key]++
		return keep[0] && keep[7], nil
	}
	_, stats, err := Minimize(8, test)
	if err != nil {
		t.Fatalf("Minimize: %v", err)
	}
	for key, n := range seen {
		if n > 1 {
			t.Errorf("configuration %s probed %d times, want memoized", key, n)
		}
	}
	if stats.Probes != len(seen) {
		t.Errorf("stats.Probes = %d, distinct configs = %d", stats.Probes, len(seen))
	}
}

func TestBudgetErrorReturnsBestSoFar(t *testing.T) {
	budgetErr := errors.New("budget")
	calls := 0
	test := func(keep []bool) (bool, error) {
		calls++
		if calls > 3 {
			return false, budgetErr
		}
		return keep[1], nil
	}
	mask, _, err := Minimize(16, test)
	if !errors.Is(err, budgetErr) {
		t.Fatalf("err = %v, want the budget error surfaced", err)
	}
	// The partial result must still reproduce.
	if !mask[1] {
		t.Fatalf("partial result dropped a required item: %v", keptIndices(mask))
	}
}

func TestSplitPartitionsEvenly(t *testing.T) {
	chunks := split([]int{0, 1, 2, 3, 4, 5, 6}, 3)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %v", chunks)
	}
	total := 0
	for _, c := range chunks {
		if len(c) < 2 || len(c) > 3 {
			t.Errorf("uneven chunk %v", c)
		}
		total += len(c)
	}
	if total != 7 {
		t.Errorf("elements lost: %d", total)
	}
}
