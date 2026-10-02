package ledger

import (
	"math/rand"
	"reflect"
	"testing"
)

// blockModel is the per-block reference: ver[b] is the current dirty
// version of block b, 0 means clean.
type blockModel struct {
	ver     []Version
	nextVer Version
	tickets map[Ticket][]Version // ticket -> captured versions over its range
	ticketR map[Ticket][2]uint64 // ticket -> [start, end)
}

func newBlockModel(n int) *blockModel {
	return &blockModel{
		ver:     make([]Version, n),
		tickets: make(map[Ticket][]Version),
		ticketR: make(map[Ticket][2]uint64),
	}
}

func (m *blockModel) mark(s, e uint64) {
	m.nextVer++
	for b := s; b < e; b++ {
		m.ver[b] = m.nextVer
	}
}

func (m *blockModel) capture(t Ticket, s, e uint64) {
	snap := make([]Version, e-s)
	copy(snap, m.ver[s:e])
	m.tickets[t] = snap
	m.ticketR[t] = [2]uint64{s, e}
}

func (m *blockModel) finish(t Ticket, success bool) {
	snap, ok := m.tickets[t]
	if !ok {
		return
	}
	r := m.ticketR[t]
	delete(m.tickets, t)
	delete(m.ticketR, t)
	if !success {
		return
	}
	for i, v := range snap {
		if v != 0 && m.ver[r[0]+uint64(i)] == v {
			m.ver[r[0]+uint64(i)] = 0
		}
	}
}

func (m *blockModel) ranges(s, e uint64) []Range {
	var rs []Range
	for b := s; b < e; b++ {
		v := m.ver[b]
		if v == 0 {
			continue
		}
		if n := len(rs); n > 0 && rs[n-1].Version == v && rs[n-1].End == b {
			rs[n-1].End = b + 1
		} else {
			rs = append(rs, Range{Start: b, End: b + 1, Version: v})
		}
	}
	return rs
}

// TestRandomInterleaving cross-checks the ledger against a per-block
// model under random interleaved Mark/Capture/Finish, including
// overlapping tickets, holes written after capture, out-of-order
// receipts, failures and duplicate finishes. Full ranges are
// compared after every operation.
func TestRandomInterleaving(t *testing.T) {
	const space = 200
	const steps = 4000
	rng := rand.New(rand.NewSource(20261003))
	l := New()
	m := newBlockModel(space)
	var open []Ticket

	randRange := func() (uint64, uint64) {
		a := uint64(rng.Intn(space))
		b := uint64(rng.Intn(space))
		if a > b {
			a, b = b, a
		}
		return a, b
	}
	check := func(step int) {
		got := l.Ranges(0, space)
		want := m.ranges(0, space)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: ledger %v, model %v", step, got, want)
		}
		// Also probe a random sub-window.
		s, e := randRange()
		if got, want := l.Ranges(s, e), m.ranges(s, e); !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d window [%d,%d): ledger %v, model %v", step, s, e, got, want)
		}
	}

	for step := 0; step < steps; step++ {
		switch rng.Intn(10) {
		case 0, 1, 2, 3: // Mark
			s, e := randRange()
			lv := l.Mark(s, e)
			m.mark(s, e)
			if uint64(lv) != uint64(m.nextVer) {
				t.Fatalf("step %d: version mismatch %d vs %d", step, lv, m.nextVer)
			}
		case 4, 5, 6: // Capture
			s, e := randRange()
			tk, rs := l.Capture(s, e)
			m.capture(tk, s, e)
			open = append(open, tk)
			if want := m.ranges(s, e); !reflect.DeepEqual(rs, want) {
				t.Fatalf("step %d: capture %v, model %v", step, rs, want)
			}
		default: // Finish: pick any open ticket (out-of-order), or a stale one
			var tk Ticket
			if len(open) > 0 && rng.Intn(4) > 0 {
				i := rng.Intn(len(open))
				tk = open[i]
				open = append(open[:i], open[i+1:]...)
			} else {
				tk = Ticket(rng.Intn(int(l.nextTick) + 2)) // may be stale/unknown
			}
			success := rng.Intn(2) == 0
			l.Finish(tk, success)
			m.finish(tk, success)
			if rng.Intn(3) == 0 {
				l.Finish(tk, rng.Intn(2) == 0) // duplicate receipt: no-op
				m.finish(tk, false)            // model: already deleted, no-op
			}
		}
		check(step)
	}
}
