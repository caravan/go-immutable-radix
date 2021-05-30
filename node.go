package iradix

import (
	"bytes"
	"sort"
)

// WalkFn is used when walking the tree. Takes a
// key and value, returning if iteration should
// be terminated.
type WalkFn func(k []byte, v interface{}) bool

// leafNode is used to represent a value
type leafNode struct {
	key []byte
	val interface{}
}

// edge is used to represent an edge node
type edge struct {
	label byte
	node  *Node
}

// Node is an immutable node in the radix tree
type Node struct {
	// leaf is used to store possible leaf
	leaf *leafNode

	// prefix is the common prefix we ignore
	prefix []byte

	// Edges should be stored in-order for iteration.
	// We avoid a fully materialized slice to save memory,
	// since in most cases we expect to be sparse
	edges edges
}

func (n *Node) isLeaf() bool {
	return n.leaf != nil
}

func (n *Node) addEdge(e edge) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= e.label
	})
	n.edges = append(n.edges, e)
	if idx != num {
		copy(n.edges[idx+1:], n.edges[idx:num])
		n.edges[idx] = e
	}
}

func (n *Node) replaceEdge(e edge) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= e.label
	})
	if idx < num && n.edges[idx].label == e.label {
		n.edges[idx].node = e.node
		return
	}
	panic("replacing missing edge")
}

func (n *Node) getEdge(label byte) (int, *Node) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		return idx, n.edges[idx].node
	}
	return -1, nil
}

func (n *Node) getLowerBoundEdge(label byte) (int, *Node) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	// we want lower bound behavior so return even if it's not an exact match
	if idx < num {
		return idx, n.edges[idx].node
	}
	return -1, nil
}

func (n *Node) delEdge(label byte) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		copy(n.edges[idx:], n.edges[idx+1:])
		n.edges[len(n.edges)-1] = edge{}
		n.edges = n.edges[:len(n.edges)-1]
	}
}

func (n *Node) Get(k []byte) (interface{}, bool) {
	search := k
	curr := n
	for {
		// Check for key exhaustion
		if len(search) == 0 {
			if curr.isLeaf() {
				return curr.leaf.val, true
			}
			break
		}

		// Look for an edge
		_, curr = curr.getEdge(search[0])
		if curr == nil {
			break
		}

		// Consume the search prefix
		if bytes.HasPrefix(search, curr.prefix) {
			search = search[len(curr.prefix):]
		} else {
			break
		}
	}
	return nil, false
}

// Minimum is used to return the minimum value in the tree
func (n *Node) Minimum() ([]byte, interface{}, bool) {
	curr := n
	for {
		if curr.isLeaf() {
			return curr.leaf.key, curr.leaf.val, true
		}
		if len(curr.edges) > 0 {
			curr = curr.edges[0].node
		} else {
			break
		}
	}
	return nil, nil, false
}

// Maximum is used to return the maximum value in the tree
func (n *Node) Maximum() ([]byte, interface{}, bool) {
	curr := n
	for {
		if num := len(curr.edges); num > 0 {
			curr = curr.edges[num-1].node
			continue
		}
		if curr.isLeaf() {
			return curr.leaf.key, curr.leaf.val, true
		} else {
			break
		}
	}
	return nil, nil, false
}

// Iterator is used to return an iterator at
// the given node to walk the tree
func (n *Node) Iterator() *Iterator {
	return &Iterator{node: n}
}

// ReverseIterator is used to return an iterator at
// the given node to walk the tree backwards
func (n *Node) ReverseIterator() *ReverseIterator {
	return NewReverseIterator(n)
}

// Walk is used to walk the tree
func (n *Node) Walk(fn WalkFn) {
	recursiveWalk(n, fn)
}

// WalkBackwards is used to walk the tree in reverse order
func (n *Node) WalkBackwards(fn WalkFn) {
	reverseRecursiveWalk(n, fn)
}

// WalkPrefix is used to walk the tree under a prefix
func (n *Node) WalkPrefix(prefix []byte, fn WalkFn) {
	search := prefix
	curr := n
	for {
		// Check for key exhaustion
		if len(search) == 0 {
			recursiveWalk(curr, fn)
			return
		}

		// Look for an edge
		_, curr = curr.getEdge(search[0])
		if curr == nil {
			break
		}

		// Consume the search prefix
		if bytes.HasPrefix(search, curr.prefix) {
			search = search[len(curr.prefix):]

		} else if bytes.HasPrefix(curr.prefix, search) {
			// Child may be under our search prefix
			recursiveWalk(curr, fn)
			return
		} else {
			break
		}
	}
}

// WalkPath is used to walk the tree, but only visiting nodes
// from the root down to a given leaf. Where WalkPrefix walks
// all the entries *under* the given prefix, this walks the
// entries *above* the given prefix.
func (n *Node) WalkPath(path []byte, fn WalkFn) {
	search := path
	curr := n
	for {
		// Visit the leaf values if any
		if curr.leaf != nil && fn(curr.leaf.key, curr.leaf.val) {
			return
		}

		// Check for key exhaustion
		if len(search) == 0 {
			return
		}

		// Look for an edge
		_, curr = curr.getEdge(search[0])
		if curr == nil {
			return
		}

		// Consume the search prefix
		if bytes.HasPrefix(search, curr.prefix) {
			search = search[len(curr.prefix):]
		} else {
			break
		}
	}
}

// recursiveWalk is used to do a pre-order walk of a node
// recursively. Returns true if the walk should be aborted
func recursiveWalk(n *Node, fn WalkFn) bool {
	// Visit the leaf values if any
	if n.leaf != nil && fn(n.leaf.key, n.leaf.val) {
		return true
	}

	// Recurse on the children
	for _, e := range n.edges {
		if recursiveWalk(e.node, fn) {
			return true
		}
	}
	return false
}

// reverseRecursiveWalk is used to do a reverse pre-order
// walk of a node recursively. Returns true if the walk
// should be aborted
func reverseRecursiveWalk(n *Node, fn WalkFn) bool {
	// Visit the leaf values if any
	if n.leaf != nil && fn(n.leaf.key, n.leaf.val) {
		return true
	}

	// Recurse on the children in reverse order
	for i := len(n.edges) - 1; i >= 0; i-- {
		e := n.edges[i]
		if reverseRecursiveWalk(e.node, fn) {
			return true
		}
	}
	return false
}
