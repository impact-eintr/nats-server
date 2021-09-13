package server

import (
	"errors"
	"sync"
)

// Common byte variables for wildcards and token separator
const (
	pwc   = '*'
	fwc   = '>'
	tsp   = "."
	btsep = '.'
)

// Sublist related errors
var (
	ErrInvalidSubject = errors.New("sublist: Invalid Subject")
	ErrNotFound       = errors.New("sublist: No Matches Found")
)

// A Sublist stores and efficiently(有效地) retrieves(检索) subscriptions(订阅).
type Sublist struct {
	sync.RWMutex
	genid     uint64
	matches   uint64
	cacheHits uint64
	inserts   uint64
	removes   uint64
	cache     map[string]*SublistResult
	root      *level
	count     uint32
}

// A result structrue better optimized for queue subs.
type SublistResult struct {
	psubs []*subscription
	qsubs [][]*subscription // don't make this a map, too expensive to iterate(迭代)
}

// A node contains subscriptions and a poiter to the next level
type node struct {
	next  *level
	psubs []subscription
	qsubs [][]*subscription
}

// A level represents(代表) a group of nodes and special poiters to wildcard(通配符) nodes
type level struct {
	nodes    map[string]*node
	pwc, fwc *node
}

func newNode() *node {
	return &node{psubs: make([]subscription, 0, 4)}
}

func newLevel() *level {
	return &level{nodes: make(map[string]*node)}
}

func NewSubList() *Sublist {
	return &Sublist{root: newLevel(), cache: make(map[string]*SublistResult)}
}

// Insert adds a subscription into the sublist
func (s *Sublist) Insert(sub *subscription) error {
	// TODO
}
