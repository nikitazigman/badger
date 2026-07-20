package tui

import (
	"testing"

	"github.com/nikitazigman/badger/internal/storage"
)

func TestDatabaseViewModelIncludesBucketItemsInBTrees(t *testing.T) {
	t.Parallel()

	overview := &storage.DatabaseOverview{
		Path:          "fixture.db",
		PageSizeBytes: 4096,
		PageCount:     4,
		FirstPageID:   0,
		BTrees: []storage.BTreeItem{{
			ID:       "bucket:root",
			Kind:     storage.BTreeBucket,
			Name:     "root",
			RootPage: &storage.PageRef{ID: 3},
			Rows: []storage.Field{
				{Label: "Type", Value: "bucket"},
				{Label: "Name", Value: "root"},
			},
		}},
	}

	db, err := newDatabaseViewModel(overview)
	if err != nil {
		t.Fatalf("newDatabaseViewModel returned error: %v", err)
	}
	if len(db.Tables) != 1 {
		t.Fatalf("bucket rows in B-TREES = %d, want 1", len(db.Tables))
	}
	if got := db.Tables[0]; got.Kind != storage.BTreeBucket || got.Name != "root" || got.RootPage != 3 {
		t.Fatalf("bucket view model = %+v", got)
	}

	items := buildNavItems(db, nil, nil)
	if len(items) == 0 || items[0].kind != navTable || items[0].schema == nil || items[0].schema.Name != "root" {
		t.Fatalf("first nav item = %+v, want root bucket B-TREES row", items)
	}
}

func TestPageSummariesDoNotLabelPageNavRows(t *testing.T) {
	t.Parallel()

	overview := &storage.DatabaseOverview{
		Path:          "fixture.db",
		PageSizeBytes: 4096,
		PageCount:     3,
		FirstPageID:   0,
		PageSummaries: []storage.PageSummary{
			{Ref: storage.PageRef{ID: 0}, Classification: storage.PageClassMeta, Label: "meta"},
			{Ref: storage.PageRef{ID: 1}, Classification: storage.PageClassFreelist, Label: "freelist"},
			{Ref: storage.PageRef{ID: 2}, Classification: storage.PageClassFree, Label: "free"},
		},
	}

	db, err := newDatabaseViewModel(overview)
	if err != nil {
		t.Fatalf("newDatabaseViewModel returned error: %v", err)
	}
	items := buildNavItems(db, nil, nil)
	if len(items) != 3 {
		t.Fatalf("nav items = %d, want 3", len(items))
	}
	if items[0].title != "page 0" || items[1].title != "page 1" || items[2].title != "page 2" {
		t.Fatalf("page nav titles = %q, %q, %q", items[0].title, items[1].title, items[2].title)
	}
}
