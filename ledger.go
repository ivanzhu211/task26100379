// Package blockledger implements a changed-block interval ledger for a
// backup copy engine. It tracks dirty block ranges with versions and
// confirms captured versions via tickets; it never moves block bytes.
package blockledger

// MaxBlocks is the number of logical blocks: 2^48, numbered from 0.
const MaxBlocks uint64 = 1 << 48

// Range is a half-open dirty interval [Start, End) carrying a Version.
type Range struct {
	Start   uint64
	End     uint64
	Version uint64
}

// Ledger tracks dirty block intervals and outstanding capture tickets.
// All methods must be called serially; the ledger is not goroutine-safe.
type Ledger struct {
	root        *inode
	dirtyCount  int
	nextVersion uint64
	nextTicket  uint64
	tickets     map[uint64][]Range
	rng         uint64
}

// New returns an empty ledger with no dirty blocks.
func New() *Ledger {
	return &Ledger{
		nextVersion: 1,
		nextTicket:  1,
		tickets:     make(map[uint64][]Range),
		rng:         0x9e3779b97f4a7c15,
	}
}

func (l *Ledger) nextPrio() uint64 {
	l.rng ^= l.rng << 13
	l.rng ^= l.rng >> 7
	l.rng ^= l.rng << 17
	return l.rng
}

func checkRange(start, end uint64) {
	if start > end || end > MaxBlocks {
		panic("blockledger: range out of bounds")
	}
}

func clip(rs []Range, start, end uint64) []Range {
	for i := range rs {
		if rs[i].Start < start {
			rs[i].Start = start
		}
		if rs[i].End > end {
			rs[i].End = end
		}
	}
	return rs
}

// overlapping returns the stored intervals intersecting [start, end).
func (l *Ledger) overlapping(start, end uint64) []Range {
	var out []Range
	collectOverlapping(l.root, start, end, &out)
	return out
}

func (l *Ledger) insertInterval(start, end, version uint64) {
	l.root = insert(l.root, &inode{
		start:   start,
		end:     end,
		version: version,
		prio:    l.nextPrio(),
	})
	l.dirtyCount++
}

// clearVersion removes dirty blocks in [start, end) whose current version
// equals version, splitting intervals at the boundaries. Blocks carrying
// any newer version are left untouched.
func (l *Ledger) clearVersion(start, end, version uint64) {
	for _, r := range l.overlapping(start, end) {
		if r.Version != version {
			continue
		}
		l.root = erase(l.root, r.Start)
		l.dirtyCount--
		if r.Start < start {
			l.insertInterval(r.Start, start, r.Version)
		}
		if r.End > end {
			l.insertInterval(end, r.End, r.Version)
		}
	}
}

// Mark flags every block in [start, end) dirty with a fresh, globally
// increasing version unique to this call, and returns that version.
// Re-marking already dirty blocks still produces a new version.
func (l *Ledger) Mark(start, end uint64) uint64 {
	checkRange(start, end)
	version := l.nextVersion
	l.nextVersion++
	if start == end {
		return version
	}
	for _, r := range l.overlapping(start, end) {
		l.root = erase(l.root, r.Start)
		l.dirtyCount--
		if r.Start < start {
			l.insertInterval(r.Start, start, r.Version)
		}
		if r.End > end {
			l.insertInterval(end, r.End, r.Version)
		}
	}
	l.insertInterval(start, end, version)
	return version
}

// Capture returns a unique ticket and the dirty ranges inside [start, end)
// at this moment. The ticket is an immutable record of the captured dirty
// blocks only; capturing does not clear dirty state, and any number of
// unfinished tickets may overlap.
func (l *Ledger) Capture(start, end uint64) (uint64, []Range) {
	checkRange(start, end)
	ticket := l.nextTicket
	l.nextTicket++
	if start == end {
		l.tickets[ticket] = nil
		return ticket, nil
	}
	rs := clip(l.overlapping(start, end), start, end)
	stored := make([]Range, len(rs))
	copy(stored, rs)
	l.tickets[ticket] = stored
	return ticket, rs
}

// Finish confirms a ticket. On success it clears only blocks still holding
// the exact versions recorded by the ticket; blocks rewritten since the
// capture keep their newer versions. On failure nothing is cleared. The
// first Finish invalidates the ticket; later calls are no-ops.
func (l *Ledger) Finish(ticket uint64, success bool) {
	rs, ok := l.tickets[ticket]
	if !ok {
		return
	}
	delete(l.tickets, ticket)
	if !success {
		return
	}
	for _, r := range rs {
		l.clearVersion(r.Start, r.End, r.Version)
	}
}

// Ranges returns the current dirty intervals inside [start, end), sorted by
// block number. Adjacent intervals with equal versions are merged; clean
// gaps are never bridged.
func (l *Ledger) Ranges(start, end uint64) []Range {
	checkRange(start, end)
	if start == end {
		return nil
	}
	rs := clip(l.overlapping(start, end), start, end)
	merged := rs[:0]
	for _, r := range rs {
		if n := len(merged); n > 0 && merged[n-1].End == r.Start && merged[n-1].Version == r.Version {
			merged[n-1].End = r.End
			continue
		}
		merged = append(merged, r)
	}
	return merged
}
