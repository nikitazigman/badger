package sqlite

// PageIndex maps each b-tree root page to the walk that enumerated it.
// It is plain data (PageWalk is itself serializable), so the whole index can later be
// persisted to / restored from disk. Build it from a single goroutine (the TUI Update
// loop); the walks it stores are produced in parallel upstream by PagesForRoot.
type PageIndex struct {
	Walks map[uint32]PageWalk // keyed by PageWalk.Root
}

// NewPageIndex returns an empty index ready to accept walks.
func NewPageIndex() PageIndex {
	return PageIndex{Walks: make(map[uint32]PageWalk)}
}

// Set stores (or replaces) the walk for walk.Root.
//
// The value receiver mutates the underlying map (maps are reference types), which is
// intentional and matches Go idiom for map-backed wrappers.
func (idx PageIndex) Set(walk PageWalk) {
	idx.Walks[walk.Root] = walk
}

// Walk returns the stored walk for root and whether it is present (indexed yet).
func (idx PageIndex) Walk(root uint32) (PageWalk, bool) {
	walk, ok := idx.Walks[root]
	return walk, ok
}

// Pages returns the page set for root, or nil if not indexed. Convenience for callers
// that only want the page numbers (the filtered PAGES list).
func (idx PageIndex) Pages(root uint32) []uint32 {
	walk, ok := idx.Walks[root]
	if !ok {
		return nil
	}
	return walk.Pages
}
