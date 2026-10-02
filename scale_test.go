package ledger

import (
	"runtime"
	"testing"
	"time"
)

type scaleResult struct {
	n            int
	build        time.Duration
	ops          time.Duration
	heapPerInter uint64
	finalCount   int
}

// runScale builds n discrete single-block dirty intervals, then
// performs n single-block updates each followed by a capture and a
// successful receipt, and reports timing and heap per dirty interval.
func runScale(n int) scaleResult {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	l := New()
	t0 := time.Now()
	for i := 0; i < n; i++ {
		b := uint64(i) * 4
		l.Mark(b, b+1)
	}
	build := time.Since(t0)

	runtime.GC()
	runtime.ReadMemStats(&after)

	t1 := time.Now()
	for i := 0; i < n; i++ {
		b := uint64(i)*4 + 2 // clean block between two dirty ones
		l.Mark(b, b+1)
		tk, _ := l.Capture(b, b+1)
		l.Finish(tk, true)
	}
	ops := time.Since(t1)

	return scaleResult{
		n:            n,
		build:        build,
		ops:          ops,
		heapPerInter: (after.HeapAlloc - before.HeapAlloc) / uint64(n),
		finalCount:   len(l.Ranges(0, Blocks)),
	}
}

// TestScale runs the 50,000-interval scenario required by the spec,
// then repeats at double size to show local operations do not
// degrade into full-ledger scans. Re-runnable: fixed structure, no
// randomness. Run with: go test -run TestScale -v
func TestScale(t *testing.T) {
	small := runScale(50_000)
	if small.finalCount != small.n {
		t.Fatalf("final dirty count %d, want %d", small.finalCount, small.n)
	}
	t.Logf("n=%d  build=%v  ops=%v  ops/op=%v  heap/interval=%dB",
		small.n, small.build, small.ops, small.ops/time.Duration(small.n), small.heapPerInter)

	large := runScale(100_000)
	if large.finalCount != large.n {
		t.Fatalf("final dirty count %d, want %d", large.finalCount, large.n)
	}
	t.Logf("n=%d  build=%v  ops=%v  ops/op=%v  heap/interval=%dB",
		large.n, large.build, large.ops, large.ops/time.Duration(large.n), large.heapPerInter)

	// Per-op cost must grow about logarithmically, not linearly:
	// doubling n may at most slightly more than double total time.
	ratio := float64(large.ops) / float64(small.ops)
	t.Logf("ops time ratio (2x scale) = %.2f", ratio)
	if ratio > 8 {
		t.Errorf("ops time ratio %.2f suggests full-ledger scans", ratio)
	}
}
