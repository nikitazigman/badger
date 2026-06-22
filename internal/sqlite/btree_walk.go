package sqlite

import "sort"

// PageWalk is the result of walking one b-tree from its root.
// Serializable by design (plain integers) so it can later be persisted to disk.
type PageWalk struct {
	Root    uint32        // the root page the walk started from
	Pages   []uint32      // sorted, unique; includes Root; only pages actually reached
	Skipped []SkippedPage // child pages that could not be parsed (degraded walk)
}

// SkippedPage records a child pointer that failed to parse during the walk.
type SkippedPage struct {
	Page   uint32 // the unreadable child page
	Parent uint32 // the interior page that pointed to it
	Reason string // human-readable parse error
}

// PagesForRoot walks the b-tree rooted at `root` and returns every page in it
// (interior + leaf nodes reachable from the root).
//
//   - Per-child parse failures are recorded in PageWalk.Skipped and the walk continues
//     (degrade, don't crash).
//   - An error is returned ONLY for a hard failure: an invalid root, or the root page
//     itself being unreadable.
//   - root == 0 (e.g. virtual tables with no b-tree) returns an empty PageWalk and no error.
//
// Overflow-page chains are intentionally not followed; an index is its own b-tree
// and must be walked from its own root.
func (i *Inspector) PagesForRoot(root uint32) (PageWalk, error) {
	walk := PageWalk{Root: root}
	if root == 0 {
		return walk, nil
	}

	// The root is inspected here so its parse failure is a hard error; children are
	// inspected inside the recursion, where failures degrade into walk.Skipped.
	inspection, err := i.InspectPage(root)
	if err != nil {
		return walk, err
	}

	visited := map[uint32]bool{root: true}
	i.walkBTree(inspection, &walk, visited)

	sort.Slice(walk.Pages, func(a, b int) bool { return walk.Pages[a] < walk.Pages[b] })
	return walk, nil
}

// walkBTree records the already-parsed page, then recurses depth-first into each
// child it points to. The visited set guards against cycles and double-counting; a
// child that fails to parse is recorded in walk.Skipped and does not stop the walk.
func (i *Inspector) walkBTree(inspection *PageInspection, walk *PageWalk, visited map[uint32]bool) {
	walk.Pages = append(walk.Pages, inspection.PageNumber)

	header := inspection.BTreePage.PageHeader
	if !header.IsInterior() {
		return
	}

	parent := inspection.PageNumber
	children := make([]uint32, 0, len(inspection.BTreePage.TableInteriorCells)+len(inspection.BTreePage.IndexInteriorCells)+1)
	for _, cell := range inspection.BTreePage.TableInteriorCells {
		children = append(children, cell.LeftChildPage.Value)
	}
	for _, cell := range inspection.BTreePage.IndexInteriorCells {
		children = append(children, cell.LeftChildPage.Value)
	}
	if header.RightMostPointer != nil {
		children = append(children, header.RightMostPointer.Value)
	}

	for _, child := range children {
		if child == 0 || visited[child] {
			continue
		}
		visited[child] = true

		childInspection, err := i.InspectPage(child)
		if err != nil {
			walk.Skipped = append(walk.Skipped, SkippedPage{
				Page:   child,
				Parent: parent,
				Reason: err.Error(),
			})
			continue
		}
		i.walkBTree(childInspection, walk, visited)
	}
}
