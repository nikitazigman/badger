package sqlite

import "testing"

func TestPageIndexEmpty(t *testing.T) {
	t.Parallel()

	idx := NewPageIndex()
	if idx.Walks == nil {
		t.Fatal("NewPageIndex().Walks is nil, want initialized map")
	}
	if len(idx.Walks) != 0 {
		t.Fatalf("NewPageIndex() has %d walks, want 0", len(idx.Walks))
	}
	if _, ok := idx.Walk(1); ok {
		t.Fatal("Walk on empty index returned ok=true")
	}
	if pages := idx.Pages(1); pages != nil {
		t.Fatalf("Pages on empty index = %v, want nil", pages)
	}
}

func TestPageIndexSetWalkRoundTrip(t *testing.T) {
	t.Parallel()

	idx := NewPageIndex()
	walk := PageWalk{Root: 7, Pages: []uint32{7, 9, 12}}
	idx.Set(walk)

	got, ok := idx.Walk(7)
	if !ok {
		t.Fatal("Walk(7) returned ok=false after Set")
	}
	if got.Root != 7 || !equalUint32Slice(got.Pages, walk.Pages) {
		t.Fatalf("Walk(7) = %+v, want %+v", got, walk)
	}

	if pages := idx.Pages(7); !equalUint32Slice(pages, walk.Pages) {
		t.Fatalf("Pages(7) = %v, want %v", pages, walk.Pages)
	}

	if _, ok := idx.Walk(99); ok {
		t.Fatal("Walk(99) returned ok=true for unindexed root")
	}
	if pages := idx.Pages(99); pages != nil {
		t.Fatalf("Pages(99) = %v, want nil for unindexed root", pages)
	}
}

func TestPageIndexSetReplaces(t *testing.T) {
	t.Parallel()

	idx := NewPageIndex()
	idx.Set(PageWalk{Root: 3, Pages: []uint32{3}})
	idx.Set(PageWalk{Root: 3, Pages: []uint32{3, 5, 8}})

	if pages := idx.Pages(3); !equalUint32Slice(pages, []uint32{3, 5, 8}) {
		t.Fatalf("Pages(3) after replace = %v, want [3 5 8]", pages)
	}
	if len(idx.Walks) != 1 {
		t.Fatalf("index holds %d walks after replacing root 3, want 1", len(idx.Walks))
	}
}

func equalUint32Slice(got []uint32, want []uint32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
