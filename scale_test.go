package blockledger

import (
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// runScale builds n discrete single-block dirty intervals, then performs n
// rounds of local work: rewrite one block, capture a small window around
// it, and confirm the ticket. It returns elapsed time, heap growth, and
// the number of dirty intervals left.
func runScale(tb testing.TB, n int) (elapsed time.Duration, heapDelta int64, heapFinal uint64, dirtyLeft int) {
	l := New()
	stride := uint64(4)
	for i := 0; i < n; i++ {
		l.Mark(uint64(i)*stride, uint64(i)*stride+1)
	}

	rng := rand.New(rand.NewSource(int64(n)))
	cleared := make([]bool, n)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()

	for round := 0; round < n; round++ {
		i := rng.Intn(n)
		pos := uint64(i) * stride
		l.Mark(pos, pos+1)
		lo := uint64(0)
		if pos >= stride {
			lo = pos - stride
		}
		hi := pos + 2*stride
		if hi > MaxBlocks {
			hi = MaxBlocks
		}
		ticket, _ := l.Capture(lo, hi)
		l.Finish(ticket, true)
		for j := i - 1; j <= i+1; j++ {
			if j >= 0 && j < n {
				cleared[j] = true
			}
		}
	}

	elapsed = time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&after)

	want := 0
	for _, c := range cleared {
		if !c {
			want++
		}
	}
	if got := l.dirtyCount; got != want {
		tb.Fatalf("dirty intervals = %d, want %d", got, want)
	}
	return elapsed, int64(after.HeapAlloc) - int64(before.HeapAlloc), after.HeapAlloc, l.dirtyCount
}

// TestScale runs the 50k-interval / 50k-update workload and repeats it at
// doubled size. Local operations must stay local: per-op cost at 2x scale
// must not grow as if the whole ledger were scanned each time.
func TestScale(t *testing.T) {
	type result struct {
		n       int
		elapsed time.Duration
		heap    int64
		dirty   int
	}
	var results []result
	for _, n := range []int{50_000, 100_000} {
		elapsed, heap, heapFinal, dirty := runScale(t, n)
		results = append(results, result{n, elapsed, heap, dirty})
		t.Logf("n=%d: %d update+capture+finish rounds in %v (%.0f ns/op), heap delta %+d bytes during rounds, final heap %d bytes, dirty intervals left %d",
			n, n, elapsed, float64(elapsed.Nanoseconds())/float64(n), heap, heapFinal, dirty)
	}
	perOp0 := float64(results[0].elapsed.Nanoseconds()) / float64(results[0].n)
	perOp1 := float64(results[1].elapsed.Nanoseconds()) / float64(results[1].n)
	t.Logf("per-op cost ratio at 2x scale: %.2f (full-ledger scans would show ~2x)", perOp1/perOp0)
}
