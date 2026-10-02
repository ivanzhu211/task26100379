// Package ledger implements a changed-block interval ledger for a
// backup copy engine. It tracks dirty block ranges with versions and
// confirms replication of captured versions without moving block bytes.
//
// The logical disk holds 2^48 blocks numbered from 0. All ranges are
// half-open [start, end). Calls are executed serially; Capture, Mark
// and Finish may interleave arbitrarily and receipts may arrive out
// of order. No persistence is provided.
//
// Internal cost: with m dirty intervals and k intervals touched by
// one operation (plus ticket intervals), each operation runs in
// amortized O((k+1) log(m+2)) time and never scans the whole ledger.
package ledger

// Blocks is the number of logical blocks: 2^48, numbered from 0.
const Blocks uint64 = 1 << 48

// Version identifies one Mark call. Versions increase globally.
type Version uint64

// Range is a half-open dirty interval [Start, End) carrying Version.
type Range struct {
	Start   uint64
	End     uint64
	Version Version
}

// Ticket identifies one Capture record. It is immutable and becomes
// invalid after the first Finish call that references it.
type Ticket uint64

// Ledger tracks dirty intervals and open capture tickets.
//
// Invariant: stored intervals are disjoint, sorted by start, and two
// adjacent intervals never carry the same version (merged eagerly).
type Ledger struct {
	tree     *node
	size     int
	nextVer  Version
	nextTick Ticket
	open     map[Ticket]*ticket
}

type ticket struct {
	ranges []Range
}

// New returns an empty ledger with no dirty blocks.
func New() *Ledger {
	return &Ledger{open: make(map[Ticket]*ticket)}
}

// Mark flags [start, end) dirty with a fresh, globally increasing
// version and returns that version. Re-marking already dirty blocks
// still produces a new version.
func (l *Ledger) Mark(start, end uint64) Version {
	checkRange(start, end)
	l.nextVer++
	v := l.nextVer
	if start == end {
		return v
	}

	// Clip the interval straddling start, if any.
	if p := predecessor(l.tree, start); p != nil && p.end > start {
		if p.end > end {
			// [start, end) lies strictly inside p: split p.
			right := &node{start: end, end: p.end, ver: p.ver, prio: nextPrio()}
			p.end = start
			l.tree = insert(l.tree, right)
			l.size++
			l.tree = insert(l.tree, &node{start: start, end: end, ver: v, prio: nextPrio()})
			l.size++
			return v
		}
		p.end = start
	}

	// Remove covered intervals; clip the one straddling end.
	for _, n := range collectOverlapping(l.tree, start, end, nil) {
		l.tree = erase(l.tree, n.start)
		if n.end > end {
			l.tree = insert(l.tree, &node{start: end, end: n.end, ver: n.ver, prio: nextPrio()})
		} else {
			l.size--
		}
	}

	l.tree = insert(l.tree, &node{start: start, end: end, ver: v, prio: nextPrio()})
	l.size++
	return v
}

// Capture returns a unique ticket and the dirty intervals inside
// [start, end) at this moment. The ticket records only blocks dirty
// at capture time and is immutable; dirty state is not cleared, and
// open tickets may overlap.
func (l *Ledger) Capture(start, end uint64) (Ticket, []Range) {
	checkRange(start, end)
	l.nextTick++
	t := l.nextTick
	var rs []Range
	if start < end {
		for _, n := range collectOverlapping(l.tree, start, end, nil) {
			rs = append(rs, Range{Start: max(n.start, start), End: min(n.end, end), Version: n.ver})
		}
	}
	l.open[t] = &ticket{ranges: rs}
	var out []Range
	out = append(out, rs...)
	return t, out
}

// Finish confirms (success=true) or rejects (success=false) the
// replication of every block version recorded by the ticket. Only
// blocks still carrying the recorded version are cleared; blocks
// re-written after the capture keep their newer version. A failed
// finish clears nothing. The first Finish invalidates the ticket;
// later calls with the same ticket change nothing.
func (l *Ledger) Finish(t Ticket, success bool) {
	tk, ok := l.open[t]
	if !ok {
		return
	}
	delete(l.open, t)
	if !success {
		return
	}
	for _, r := range tk.ranges {
		l.clearVersion(r.Start, r.End, r.Version)
	}
}

// Ranges returns the current dirty intervals inside [start, end),
// ordered by block number; adjacent intervals with equal versions
// are merged, clean blocks are never crossed.
func (l *Ledger) Ranges(start, end uint64) []Range {
	checkRange(start, end)
	if start >= end {
		return nil
	}
	var rs []Range
	for _, n := range collectOverlapping(l.tree, start, end, nil) {
		s, e := max(n.start, start), min(n.end, end)
		if last := len(rs) - 1; last >= 0 && rs[last].Version == n.ver && rs[last].End == s {
			rs[last].End = e
		} else {
			rs = append(rs, Range{Start: s, End: e, Version: n.ver})
		}
	}
	return rs
}

// clearVersion clears dirty state exactly where the current interval
// intersecting [s, e) still carries version v.
func (l *Ledger) clearVersion(s, e uint64, v Version) {
	for _, n := range collectOverlapping(l.tree, s, e, nil) {
		if n.ver != v {
			continue
		}
		switch {
		case n.start >= s && n.end <= e:
			// Fully confirmed: drop it. The neighbours are separated
			// by a clean gap now, so no merge can apply.
			l.tree = erase(l.tree, n.start)
			l.size--
		case n.start < s && n.end > e:
			// Confirmation sits strictly inside n: split n.
			right := &node{start: e, end: n.end, ver: n.ver, prio: nextPrio()}
			n.end = s
			l.tree = insert(l.tree, right)
			l.size++
		case n.start < s:
			n.end = s
		default: // n.end > e
			l.tree = erase(l.tree, n.start)
			l.tree = insert(l.tree, &node{start: e, end: n.end, ver: n.ver, prio: nextPrio()})
		}
	}
}

func checkRange(start, end uint64) {
	if start > end || end > Blocks {
		panic("ledger: range out of bounds")
	}
}
