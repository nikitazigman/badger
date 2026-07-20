package bbolt

import (
	"fmt"
	"sort"
)

// PageWalk is the result of walking one bbolt bucket b+tree from its root.
// It is plain data so storage and future indexes can cache it without parser state.
type PageWalk struct {
	Root    PageID
	Pages   []PageID
	Skipped []SkippedPage
}

// SkippedPage records a child pointer that could not be followed during the walk.
type SkippedPage struct {
	Page   PageID
	Parent PageID
	Reason string
}

// PagesForRoot walks the branch/leaf pages reachable from a bucket root.
//
// A root of 0 represents an inline bucket and is not physical page 0. Root parse
// failures are hard errors; child parse failures degrade into Skipped entries.
func (i *Inspector) PagesForRoot(root PageID) (PageWalk, error) {
	walk := PageWalk{Root: root}
	if root == 0 {
		return walk, nil
	}

	inspection, err := i.InspectPage(root)
	if err != nil {
		return walk, err
	}
	if !isBucketBTreePage(inspection) {
		return walk, fmt.Errorf("root page %d is %s, want branch or leaf", root, inspection.Classification)
	}

	visited := map[PageID]bool{root: true}
	i.walkBucketBTree(inspection, &walk, visited)

	sort.Slice(walk.Pages, func(a, b int) bool { return walk.Pages[a] < walk.Pages[b] })
	return walk, nil
}

func (i *Inspector) walkBucketBTree(inspection *BTreePage, walk *PageWalk, visited map[PageID]bool) {
	walk.Pages = append(walk.Pages, inspection.ID)

	if inspection.Classification != PageClassBranch || inspection.BranchPayload == nil {
		return
	}

	parent := inspection.ID
	for _, element := range inspection.BranchPayload.BranchElements {
		child := element.PageID.Value
		if child == 0 {
			walk.Skipped = append(walk.Skipped, SkippedPage{
				Page:   child,
				Parent: parent,
				Reason: "branch child page id 0 is not a physical bucket page",
			})
			continue
		}
		if visited[child] {
			walk.Skipped = append(walk.Skipped, SkippedPage{
				Page:   child,
				Parent: parent,
				Reason: "branch child page was already visited",
			})
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
		if !isBucketBTreePage(childInspection) {
			walk.Skipped = append(walk.Skipped, SkippedPage{
				Page:   child,
				Parent: parent,
				Reason: fmt.Sprintf("child page is %s, want branch or leaf", childInspection.Classification),
			})
			continue
		}
		i.walkBucketBTree(childInspection, walk, visited)
	}
}

func isBucketBTreePage(page *BTreePage) bool {
	if page == nil {
		return false
	}
	return page.Classification == PageClassBranch || page.Classification == PageClassLeaf
}

// BucketEntries returns bucket leaf entries found in the bucket b+tree rooted at root.
// It does not recurse into child buckets; each returned entry describes a bucket value
// stored directly in the walked bucket.
func (i *Inspector) BucketEntries(root PageID) ([]BucketEntry, PageWalk, error) {
	walk, err := i.PagesForRoot(root)
	if err != nil {
		return nil, walk, err
	}

	entries := []BucketEntry{}
	for _, pageID := range walk.Pages {
		page, err := i.InspectPage(pageID)
		if err != nil || page.LeafPayload == nil {
			continue
		}

		bucketIndex := 0
		for idx, element := range page.LeafPayload.LeafElements {
			if element.Flags.Value != BucketLeafFlag {
				continue
			}
			if idx >= len(page.LeafPayload.KeyValue) || bucketIndex >= len(page.LeafPayload.NestedBucket) {
				bucketIndex++
				continue
			}
			entries = append(entries, BucketEntry{
				PageID:       page.ID,
				ElementIndex: idx,
				Key:          page.LeafPayload.KeyValue[idx].Key,
				Bucket:       page.LeafPayload.NestedBucket[bucketIndex],
			})
			bucketIndex++
		}
	}

	return entries, walk, nil
}
