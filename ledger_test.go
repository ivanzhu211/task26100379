package blockledger

import (
	"math/rand"
	"reflect"
	"testing"
)

// blockModel is a per-block reference implementation for a tiny address
// space. version 0 means clean.
type blockModel struct {
	blocks  []uint64
	tickets map[uint64][]Range
}

func newBlockModel(n int) *blockModel {
	return &blockModel{blocks: make([]uint64, n), tickets: make(map[uint64][]Range)}
}

func (m *blockModel) mark(start, end int, version uint64) {
	for i := start; i < end; i++ {
		m.blocks[i] = version
	}
}

func (m *blockModel) capture(ticket uint64, start, end int) {
	var rs []Range
	for i := start; i < end; i++ {
		if m.blocks[i] == 0 {
			continue
		}
		if n := len(rs); n > 0 && rs[n-1].End == uint64(i) && rs[n-1].Version == m.blocks[i] {
			rs[n-1].End++
			continue
		}
		rs = append(rs, Range{Start: uint64(i), End: uint64(i) + 1, Version: m.blocks[i]})
	}
	m.tickets[ticket] = rs
}

func (m *blockModel) finish(ticket uint64, success bool) {
	rs, ok := m.tickets[ticket]
	if !ok {
		return
	}
	delete(m.tickets, ticket)
	if !success {
		return
	}
	for _, r := range rs {
		for i := r.Start; i < r.End; i++ {
			if m.blocks[i] == r.Version {
				m.blocks[i] = 0
			}
		}
	}
}

func (m *blockModel) ranges(start, end int) []Range {
	var rs []Range
	for i := start; i < end; i++ {
		if m.blocks[i] == 0 {
			continue
		}
		if n := len(rs); n > 0 && rs[n-1].End == uint64(i) && rs[n-1].Version == m.blocks[i] {
			rs[n-1].End++
			continue
		}
		rs = append(rs, Range{Start: uint64(i), End: uint64(i) + 1, Version: m.blocks[i]})
	}
	return rs
}

// TestRandomInterleaving cross-checks the ledger against a per-block model
// under random interleaved Mark/Capture/Finish/Ranges calls, covering
// overlapping tickets, clean holes written after capture, out-of-order
// successful receipts, failures, and duplicate receipts.
func TestRandomInterleaving(t *testing.T) {
	const space = 96
	const ops = 20000
	rng := rand.New(rand.NewSource(20261003))
	l := New()
	m := newBlockModel(space)
	var open []uint64

	randRange := func() (int, int) {
		a := rng.Intn(space + 1)
		b := rng.Intn(space + 1)
		if a > b {
			a, b = b, a
		}
		return a, b
	}

	for op := 0; op < ops; op++ {
		switch rng.Intn(10) {
		case 0, 1, 2, 3: // Mark
			s, e := randRange()
			v := l.Mark(uint64(s), uint64(e))
			m.mark(s, e, v)
		case 4, 5, 6: // Capture
			s, e := randRange()
			ticket, got := l.Capture(uint64(s), uint64(e))
			m.capture(ticket, s, e)
			want := m.tickets[ticket]
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("op %d: Capture(%d,%d) = %+v, want %+v", op, s, e, got, want)
			}
			open = append(open, ticket)
		case 7, 8: // Finish an open ticket, possibly out of order
			if len(open) == 0 {
				continue
			}
			i := rng.Intn(len(open))
			ticket := open[i]
			open = append(open[:i], open[i+1:]...)
			success := rng.Intn(4) != 0 // 25% failures
			l.Finish(ticket, success)
			m.finish(ticket, success)
			if rng.Intn(3) == 0 { // duplicate receipt, random result
				l.Finish(ticket, rng.Intn(2) == 0)
			}
		case 9: // Finish a bogus ticket id
			l.Finish(uint64(rng.Intn(2*ops))+1, rng.Intn(2) == 0)
		}
		s, e := randRange()
		got := l.Ranges(uint64(s), uint64(e))
		want := m.ranges(s, e)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("op %d: Ranges(%d,%d) = %+v, want %+v", op, s, e, got, want)
		}
	}
}

// TestTrillionBlockRange marks a trillion-block range, captures it, then
// rewrites three blocks in the middle. After the old ticket succeeds, only
// the three new versions may remain, and nothing is expanded per block.
func TestTrillionBlockRange(t *testing.T) {
	l := New()
	const trillion = uint64(1) << 40

	v1 := l.Mark(0, trillion)
	ticket, captured := l.Capture(0, MaxBlocks)
	if len(captured) != 1 || captured[0] != (Range{Start: 0, End: trillion, Version: v1}) {
		t.Fatalf("captured = %+v, want single [0,%d) v%d", captured, trillion, v1)
	}

	mid := trillion / 2
	v2 := l.Mark(mid, mid+1)
	v3 := l.Mark(mid+1, mid+2)
	v4 := l.Mark(mid+2, mid+3)

	l.Finish(ticket, true)

	want := []Range{
		{Start: mid, End: mid + 1, Version: v2},
		{Start: mid + 1, End: mid + 2, Version: v3},
		{Start: mid + 2, End: mid + 3, Version: v4},
	}
	if got := l.Ranges(0, MaxBlocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ranges = %+v, want %+v", got, want)
	}
	if l.dirtyCount != 3 {
		t.Fatalf("internal interval count = %d, want 3 (no per-block expansion)", l.dirtyCount)
	}
	// Duplicate success must not change anything.
	l.Finish(ticket, true)
	if got := l.Ranges(0, MaxBlocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("after duplicate Finish, Ranges = %+v, want %+v", got, want)
	}
}
