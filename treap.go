package ledger

// node is one disjoint dirty interval in a treap keyed by start.
// Invariant: intervals are disjoint, sorted, and two adjacent
// intervals never carry the same version (they are merged eagerly).
type node struct {
	start, end uint64
	ver        Version
	prio       uint32
	left       *node
	right      *node
}

// xorshift PRNG for treap priorities; calls are serial per the contract.
var prioState uint32 = 0x9e3779b9

func nextPrio() uint32 {
	x := prioState
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	prioState = x
	return x
}

func rotateRight(n *node) *node {
	l := n.left
	n.left = l.right
	l.right = n
	return l
}

func rotateLeft(n *node) *node {
	r := n.right
	n.right = r.left
	r.left = n
	return r
}

// insert adds n to the treap rooted at root. No existing interval may
// overlap [n.start, n.end).
func insert(root, n *node) *node {
	if root == nil {
		return n
	}
	if n.start < root.start {
		root.left = insert(root.left, n)
		if root.left.prio < root.prio {
			root = rotateRight(root)
		}
	} else {
		root.right = insert(root.right, n)
		if root.right.prio < root.prio {
			root = rotateLeft(root)
		}
	}
	return root
}

// erase removes the interval whose start equals key.
func erase(root *node, key uint64) *node {
	if root == nil {
		return nil
	}
	switch {
	case key < root.start:
		root.left = erase(root.left, key)
	case key > root.start:
		root.right = erase(root.right, key)
	default:
		root = merge(root.left, root.right)
	}
	return root
}

// merge combines two treaps where every key in l is less than every
// key in r.
func merge(l, r *node) *node {
	switch {
	case l == nil:
		return r
	case r == nil:
		return l
	case l.prio < r.prio:
		l.right = merge(l.right, r)
		return l
	default:
		r.left = merge(l, r.left)
		return r
	}
}

// predecessor returns the interval with the greatest start < key,
// or nil if none exists.
func predecessor(root *node, key uint64) *node {
	var best *node
	for root != nil {
		if root.start < key {
			best = root
			root = root.right
		} else {
			root = root.left
		}
	}
	return best
}

// collectOverlapping appends, in start order, every interval that
// intersects the half-open range [s, e) to out. It descends only
// into subtrees that can contain an overlap.
func collectOverlapping(root *node, s, e uint64, out []*node) []*node {
	if root == nil {
		return out
	}
	if root.start >= s {
		out = collectOverlapping(root.left, s, e, out)
	}
	if root.end > s && root.start < e {
		out = append(out, root)
	}
	if root.start < e {
		out = collectOverlapping(root.right, s, e, out)
	}
	return out
}
