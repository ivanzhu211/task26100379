package blockledger

// inode is a treap node keyed by interval start, holding one dirty
// interval [start, end) with its version. maxEnd augments the subtree
// so overlap queries can prune clean regions.
type inode struct {
	start   uint64
	end     uint64
	version uint64
	prio    uint64
	maxEnd  uint64
	left    *inode
	right   *inode
}

func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func (n *inode) recalc() {
	n.maxEnd = n.end
	if n.left != nil {
		n.maxEnd = maxU64(n.maxEnd, n.left.maxEnd)
	}
	if n.right != nil {
		n.maxEnd = maxU64(n.maxEnd, n.right.maxEnd)
	}
}

// split divides the tree into nodes with start < key (left) and
// nodes with start >= key (right).
func split(n *inode, key uint64) (l, r *inode) {
	if n == nil {
		return nil, nil
	}
	if n.start < key {
		n.right, r = split(n.right, key)
		n.recalc()
		return n, r
	}
	l, n.left = split(n.left, key)
	n.recalc()
	return l, n
}

// merge combines two treaps where every key in l is below every key in r.
func merge(l, r *inode) *inode {
	switch {
	case l == nil:
		return r
	case r == nil:
		return l
	case l.prio < r.prio:
		l.right = merge(l.right, r)
		l.recalc()
		return l
	default:
		r.left = merge(l, r.left)
		r.recalc()
		return r
	}
}

// insert adds node n; no existing node may share its start.
func insert(root, n *inode) *inode {
	n.recalc()
	l, r := split(root, n.start)
	return merge(merge(l, n), r)
}

// erase removes the node with the given start, which must exist.
func erase(root *inode, start uint64) *inode {
	l, r := split(root, start)
	_, r = split(r, start+1)
	return merge(l, r)
}

// collectOverlapping appends every interval intersecting [s, e) to out,
// in ascending start order, pruning subtrees via maxEnd.
func collectOverlapping(n *inode, s, e uint64, out *[]Range) {
	if n == nil || n.maxEnd <= s {
		return
	}
	collectOverlapping(n.left, s, e, out)
	if n.end > s && n.start < e {
		*out = append(*out, Range{Start: n.start, End: n.end, Version: n.version})
	}
	if n.start < e {
		collectOverlapping(n.right, s, e, out)
	}
}
