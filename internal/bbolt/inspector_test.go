package bbolt

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	bolt "go.etcd.io/bbolt"
)

type boltMeta struct {
	magic    uint32
	version  uint32
	pageSize uint32
	flags    uint32
	root     boltInBucket
	freelist uint64
	pgid     uint64
	txid     uint64
	checksum uint64
}

type boltInBucket struct {
	root     uint64
	sequence uint64
}

func TestOpenReturnsConfigFromBoltFixtures(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "empty", path: fixturePath("empty.db")},
		{name: "single bucket", path: fixturePath("users.db")},
		{name: "many buckets", path: fixturePath("many_buckets.db")},
		{name: "nested inline", path: fixturePath("nested_inline.db")},
		{name: "overflow", path: fixturePath("overflow.db")},
		{name: "branch pages", path: fixturePath("branch_pages.db")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inspector, err := Open(tt.path)
			if err != nil {
				t.Fatalf("Open returned error: %v", err)
			}
			t.Cleanup(func() {
				if err := inspector.Close(); err != nil {
					t.Fatalf("close inspector: %v", err)
				}
			})

			want := expectedBoltConfig(t, tt.path)
			if inspector.config != want {
				t.Fatalf("config mismatch\n got: %+v\nwant: %+v", inspector.config, want)
			}
		})
	}
}

func fixturePath(parts ...string) string {
	joined := append([]string{"..", "..", "fixtures", "bbolt"}, parts...)
	return filepath.Join(joined...)
}

func expectedBoltConfig(t *testing.T, path string) BboltConfig {
	t.Helper()

	db, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("bolt.Open fixture: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close bolt db: %v", err)
		}
	}()

	meta := currentBoltMetaFromDB(t, db)
	config := BboltConfig{
		Version:       meta.version,
		PageSize:      meta.pageSize,
		Root:          PageID(meta.root.root),
		Freelist:      PageID(meta.freelist),
		HighWaterMark: PageID(meta.pgid),
		TransactionID: meta.txid,
	}
	if config.PageSize != uint32(db.Info().PageSize) {
		t.Fatalf("reflected page size = %d, bolt.Info().PageSize = %d", config.PageSize, db.Info().PageSize)
	}
	return config
}

func TestInspectPageReadsZeroBasedPhysicalPages(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("users.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	config := inspector.Config()
	page, err := inspector.InspectPage(0)
	if err != nil {
		t.Fatalf("InspectPage(0) returned error: %v", err)
	}
	if page.Header.ID.Value != 0 {
		t.Fatalf("page header id = %d, want 0", page.Header.ID.Value)
	}
	if len(page.Raw) != int(config.PageSize) {
		t.Fatalf("raw page bytes = %d, want %d", len(page.Raw), config.PageSize)
	}
	if page.MetaPayload == nil {
		t.Fatal("page 0 missing parsed meta payload")
	}

	root, err := inspector.InspectPage(config.Root)
	if err != nil {
		t.Fatalf("InspectPage(root) returned error: %v", err)
	}
	if root.Header.ID.Value != config.Root {
		t.Fatalf("root page header id = %d, want %d", root.Header.ID.Value, config.Root)
	}
}

func TestInspectPageRecordsHeaderAndMetaSpans(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("users.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	page, err := inspector.InspectPage(0)
	if err != nil {
		t.Fatalf("InspectPage(0) returned error: %v", err)
	}
	if page.MetaPayload == nil {
		t.Fatal("page 0 missing parsed meta payload")
	}

	assertMeta(t, page.Header.Meta, 0, 16)
	assertMeta(t, page.Header.ID.Meta, 0, 8)
	assertMeta(t, page.Header.Flags.Meta, 8, 2)
	assertMeta(t, page.Header.Count.Meta, 10, 2)
	assertMeta(t, page.Header.Overflow.Meta, 12, 4)
	if got := PageID(binary.LittleEndian.Uint64(page.Raw[page.Header.ID.Meta.StartOffset:page.Header.ID.Meta.EndOffset()])); got != page.Header.ID.Value {
		t.Fatalf("header id bytes decode to %d, parsed value = %d", got, page.Header.ID.Value)
	}

	meta := page.MetaPayload
	assertMeta(t, meta.Meta, 16, 64)
	assertMeta(t, meta.Magic.Meta, 16, 4)
	assertMeta(t, meta.Version.Meta, 20, 4)
	assertMeta(t, meta.PageSize.Meta, 24, 4)
	assertMeta(t, meta.Flags.Meta, 28, 4)
	assertMeta(t, meta.Root.Meta, 32, 8)
	assertMeta(t, meta.Sequence.Meta, 40, 8)
	assertMeta(t, meta.FreeList.Meta, 48, 8)
	assertMeta(t, meta.PageID.Meta, 56, 8)
	assertMeta(t, meta.TransactionID.Meta, 64, 8)
	assertMeta(t, meta.CheckSum.Meta, 72, 8)
	if got := binary.LittleEndian.Uint32(page.Raw[meta.Magic.Meta.StartOffset:meta.Magic.Meta.EndOffset()]); got != meta.Magic.Value {
		t.Fatalf("magic bytes decode to 0x%x, parsed value = 0x%x", got, meta.Magic.Value)
	}
}

func TestOpenParsesPersistedFreelistAndClassifiesFreePages(t *testing.T) {
	t.Parallel()

	path := writeSyntheticFreelistDB(t)
	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	summaries := inspector.PageSummaries()
	if summaries[2].Classification != PageClassFreelist {
		t.Fatalf("page 2 classification = %q, want %q", summaries[2].Classification, PageClassFreelist)
	}
	if summaries[4].Classification != PageClassFree {
		t.Fatalf("page 4 classification = %q, want %q", summaries[4].Classification, PageClassFree)
	}

	freelist, err := inspector.InspectPage(2)
	if err != nil {
		t.Fatalf("InspectPage(freelist) returned error: %v", err)
	}
	if freelist.FreelistPayload == nil {
		t.Fatal("freelist page missing parsed freelist payload")
	}
	if len(freelist.FreelistPayload.IDs) != 1 || freelist.FreelistPayload.IDs[0] != 4 {
		t.Fatalf("freelist IDs = %v, want [4]", freelist.FreelistPayload.IDs)
	}
	assertMeta(t, freelist.FreelistPayload.IDFields[0].Meta, 16, 8)
}

func TestFreePageClassificationPrecedesStaleHeader(t *testing.T) {
	t.Parallel()

	path := writeSyntheticFreelistDB(t)
	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	page, err := inspector.InspectPage(4)
	if err != nil {
		t.Fatalf("InspectPage(free stale leaf) returned error: %v", err)
	}
	if page.Header.Flags.Value != LeafPageFlag {
		t.Fatalf("stale header flag = 0x%x, want leaf", uint16(page.Header.Flags.Value))
	}
	if page.Classification != PageClassFree {
		t.Fatalf("classification = %q, want %q", page.Classification, PageClassFree)
	}
	if page.LeafPayload != nil {
		t.Fatal("free page parsed stale leaf payload")
	}
}

func TestInspectLeafPageParsesOrdinaryKeyValueEntriesFromFixtures(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		fixturePath("users.db"),
		fixturePath("nested_inline.db"),
		fixturePath("overflow.db"),
		fixturePath("branch_pages.db"),
	} {
		inspector, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%s) returned error: %v", path, err)
		}

		found := false
		config := inspector.Config()
		for id := PageID(0); id < config.HighWaterMark && !found; id++ {
			page, err := inspector.InspectPage(id)
			if err != nil {
				t.Fatalf("InspectPage(%d) in %s returned error: %v", id, path, err)
			}
			if page.LeafPayload == nil {
				continue
			}
			for idx, element := range page.LeafPayload.LeafElements {
				if element.Flags.Value != OrdinaryKeyValueFlag {
					continue
				}
				if idx >= len(page.LeafPayload.KeyValue) {
					t.Fatalf("leaf element %d missing key/value payload", idx)
				}
				kv := page.LeafPayload.KeyValue[idx]
				if len(kv.Key.Data) == 0 {
					t.Fatalf("ordinary leaf entry %d on page %d has empty key", idx, id)
				}
				if kv.Key.Meta.Size != len(kv.Key.Data) {
					t.Fatalf("key span size = %d, key bytes = %d", kv.Key.Meta.Size, len(kv.Key.Data))
				}
				if kv.Value.Meta.Size != len(kv.Value.Data) {
					t.Fatalf("value span size = %d, value bytes = %d", kv.Value.Meta.Size, len(kv.Value.Data))
				}
				assertLeafElementDescriptorSpans(t, element)
				if kv.Key.Meta.EndOffset() <= len(page.Raw) {
					got := page.Raw[kv.Key.Meta.StartOffset:kv.Key.Meta.EndOffset()]
					if string(got) != string(kv.Key.Data) {
						t.Fatalf("key span bytes = %q, parsed key = %q", got, kv.Key.Data)
					}
				}
				found = true
				break
			}
		}
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
		if found {
			return
		}
	}
	t.Fatal("fixtures did not contain an ordinary bbolt leaf key/value entry")
}

func TestInspectLeafPageParsesBucketEntriesFromFixtures(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("users.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	config := inspector.Config()
	for id := PageID(0); id < config.HighWaterMark; id++ {
		page, err := inspector.InspectPage(id)
		if err != nil {
			t.Fatalf("InspectPage(%d) returned error: %v", id, err)
		}
		if page.LeafPayload == nil {
			continue
		}

		for idx, element := range page.LeafPayload.LeafElements {
			if element.Flags.Value != BucketLeafFlag {
				continue
			}
			if idx >= len(page.LeafPayload.KeyValue) {
				t.Fatalf("bucket leaf element %d missing key/value payload", idx)
			}
			kv := page.LeafPayload.KeyValue[idx]
			if len(kv.Key.Data) == 0 {
				t.Fatalf("bucket leaf entry %d on page %d has empty key", idx, id)
			}
			if kv.Value.Meta.Size != len(kv.Value.Data) {
				t.Fatalf("bucket value span size = %d, value bytes = %d", kv.Value.Meta.Size, len(kv.Value.Data))
			}
			assertLeafElementDescriptorSpans(t, element)
			if len(page.LeafPayload.NestedBucket) == 0 {
				t.Fatal("bucket leaf page did not decode nested bucket header")
			}
			bucket := page.LeafPayload.NestedBucket[0]
			assertMeta(t, bucket.Meta, kv.Value.Meta.StartOffset, 16)
			assertMeta(t, bucket.Root.Meta, kv.Value.Meta.StartOffset, 8)
			assertMeta(t, bucket.Sequence.Meta, kv.Value.Meta.StartOffset+8, 8)
			if kv.Value.Meta.Size < bucket.Meta.Size {
				t.Fatalf("bucket value span size = %d, want at least %d", kv.Value.Meta.Size, bucket.Meta.Size)
			}
			return
		}
	}

	t.Fatal("users fixture did not contain a bucket leaf entry")
}

func TestGeneratedNestedInlineFixtureHasDepthAndInlineBuckets(t *testing.T) {
	t.Parallel()

	path := fixturePath("nested_inline.db")
	db, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("bolt.Open fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close bolt db: %v", err)
		}
	})

	if err := db.View(func(tx *bolt.Tx) error {
		for _, rootName := range []string{"alpha", "beta", "gamma"} {
			current := tx.Bucket([]byte(rootName))
			if current == nil {
				return fmt.Errorf("missing root bucket %q", rootName)
			}
			for depth := 1; depth <= 5; depth++ {
				inline := current.Bucket([]byte(fmt.Sprintf("inline_%d", depth)))
				if inline == nil {
					return fmt.Errorf("%s depth %d missing inline bucket", rootName, depth)
				}
				if got := inline.Get([]byte("kind")); string(got) != "small_inline_bucket" {
					return fmt.Errorf("%s inline_%d kind = %q", rootName, depth, got)
				}
				next := current.Bucket([]byte(fmt.Sprintf("level_%d", depth)))
				if next == nil {
					return fmt.Errorf("%s missing level_%d", rootName, depth)
				}
				current = next
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("nested_inline fixture shape: %v", err)
	}

	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	inlineBuckets := 0
	config := inspector.Config()
	for id := PageID(0); id < config.HighWaterMark; id++ {
		page, err := inspector.InspectPage(id)
		if err != nil {
			t.Fatalf("InspectPage(%d) returned error: %v", id, err)
		}
		if page.LeafPayload == nil {
			continue
		}
		for _, bucket := range page.LeafPayload.NestedBucket {
			if bucket.Root.Value == 0 {
				inlineBuckets++
			}
		}
	}
	if inlineBuckets == 0 {
		t.Fatal("nested_inline fixture did not contain any inline bucket headers")
	}
}

func TestGeneratedOverflowFixtureHasContinuationPages(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("overflow.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	continuations := 0
	for _, summary := range inspector.PageSummaries() {
		if summary.Classification == PageClassContinuation {
			continuations++
		}
	}
	if continuations == 0 {
		t.Fatal("overflow fixture did not produce continuation pages")
	}
}

func TestOverflowContinuationInspectionReportsExtentParts(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("overflow.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	var continuation PageSummary
	found := false
	for _, summary := range inspector.PageSummaries() {
		if summary.Classification != PageClassContinuation || summary.OverflowExtent == nil {
			continue
		}
		continuation = summary
		found = true
		break
	}
	if !found {
		t.Fatal("overflow fixture did not expose a continuation extent")
	}
	if continuation.OverflowExtent.PartIndex <= 1 {
		t.Fatalf("continuation part index = %d, want > 1", continuation.OverflowExtent.PartIndex)
	}
	if continuation.OverflowExtent.PartCount < continuation.OverflowExtent.PartIndex {
		t.Fatalf("continuation part = %d of %d, want part within extent", continuation.OverflowExtent.PartIndex, continuation.OverflowExtent.PartCount)
	}

	parent, err := inspector.InspectPage(continuation.OverflowExtent.Parent)
	if err != nil {
		t.Fatalf("InspectPage(parent) returned error: %v", err)
	}
	if parent.OverflowExtent == nil {
		t.Fatal("overflow parent missing extent metadata")
	}
	if parent.OverflowExtent.PartIndex != 1 {
		t.Fatalf("parent part index = %d, want 1", parent.OverflowExtent.PartIndex)
	}
	if parent.OverflowExtent.PartCount != continuation.OverflowExtent.PartCount {
		t.Fatalf("parent part count = %d, continuation part count = %d", parent.OverflowExtent.PartCount, continuation.OverflowExtent.PartCount)
	}

	page, err := inspector.InspectPage(continuation.ID)
	if err != nil {
		t.Fatalf("InspectPage(continuation) returned error: %v", err)
	}
	if page.Classification != PageClassContinuation {
		t.Fatalf("classification = %q, want %q", page.Classification, PageClassContinuation)
	}
	if page.HasHeader {
		t.Fatal("continuation page exposed an independent page header")
	}
	if page.ContinuationOf == nil || *page.ContinuationOf != continuation.OverflowExtent.Parent {
		t.Fatalf("ContinuationOf = %v, want %d", page.ContinuationOf, continuation.OverflowExtent.Parent)
	}
	if page.OverflowExtent == nil {
		t.Fatal("continuation inspection missing extent metadata")
	}
	if page.OverflowExtent.PartIndex != continuation.OverflowExtent.PartIndex {
		t.Fatalf("inspected part index = %d, summary part index = %d", page.OverflowExtent.PartIndex, continuation.OverflowExtent.PartIndex)
	}
	if page.OverflowExtent.Span.StartOffset != 0 || page.OverflowExtent.Span.Size != int(inspector.Config().PageSize) {
		t.Fatalf("continuation span = %+v, want selected physical page", page.OverflowExtent.Span)
	}
}

func TestInspectBranchPageParsesBranchElements(t *testing.T) {
	t.Parallel()

	path := writeSyntheticBranchDB(t, []syntheticBranchElement{
		{key: "a", child: 4},
		{key: "z", child: 5},
	})
	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	page, err := inspector.InspectPage(3)
	if err != nil {
		t.Fatalf("InspectPage(branch) returned error: %v", err)
	}
	if page.BranchPayload == nil {
		t.Fatal("branch page missing parsed branch payload")
	}
	if len(page.BranchPayload.BranchElements) != 2 {
		t.Fatalf("branch element count = %d, want 2", len(page.BranchPayload.BranchElements))
	}

	first := page.BranchPayload.BranchElements[0]
	assertBranchElementDescriptorSpans(t, first)
	if first.PageID.Value != 4 {
		t.Fatalf("first child page id = %d, want 4", first.PageID.Value)
	}
	kv := page.BranchPayload.KeyValue[0]
	if string(kv.Key.Data) != "a" {
		t.Fatalf("first branch key = %q, want a", kv.Key.Data)
	}
	if got := page.Raw[kv.Key.Meta.StartOffset:kv.Key.Meta.EndOffset()]; string(got) != "a" {
		t.Fatalf("branch key span bytes = %q, parsed key = %q", got, kv.Key.Data)
	}
}

func TestPagesForRootWalksBranchAndLeafPages(t *testing.T) {
	t.Parallel()

	path := writeSyntheticBranchDB(t, []syntheticBranchElement{
		{key: "a", child: 4},
		{key: "z", child: 5},
	})
	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	walk, err := inspector.PagesForRoot(3)
	if err != nil {
		t.Fatalf("PagesForRoot returned error: %v", err)
	}
	want := []PageID{3, 4, 5}
	if !reflect.DeepEqual(walk.Pages, want) {
		t.Fatalf("PagesForRoot pages = %v, want %v", walk.Pages, want)
	}
	if len(walk.Skipped) != 0 {
		t.Fatalf("PagesForRoot skipped = %+v, want none", walk.Skipped)
	}
}

func TestPagesForRootSkipsBadChildAndRecordsCycle(t *testing.T) {
	t.Parallel()

	path := writeSyntheticBranchDB(t, []syntheticBranchElement{
		{key: "a", child: 4},
		{key: "m", child: 99},
		{key: "z", child: 3},
	})
	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	walk, err := inspector.PagesForRoot(3)
	if err != nil {
		t.Fatalf("PagesForRoot returned error: %v", err)
	}
	want := []PageID{3, 4}
	if !reflect.DeepEqual(walk.Pages, want) {
		t.Fatalf("PagesForRoot pages = %v, want %v", walk.Pages, want)
	}
	if len(walk.Skipped) != 2 {
		t.Fatalf("PagesForRoot skipped = %+v, want 2 entries", walk.Skipped)
	}
	if walk.Skipped[0].Page != 99 || !strings.Contains(walk.Skipped[0].Reason, "out of range") {
		t.Fatalf("first skipped child = %+v, want out-of-range page 99", walk.Skipped[0])
	}
	if walk.Skipped[1].Page != 3 || !strings.Contains(walk.Skipped[1].Reason, "already visited") {
		t.Fatalf("second skipped child = %+v, want visited cycle page 3", walk.Skipped[1])
	}
}

func TestPagesForRootRootZeroIsInlineObject(t *testing.T) {
	t.Parallel()

	path := writeSyntheticBranchDB(t, nil)
	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	walk, err := inspector.PagesForRoot(0)
	if err != nil {
		t.Fatalf("PagesForRoot(0) returned error: %v", err)
	}
	if len(walk.Pages) != 0 || len(walk.Skipped) != 0 {
		t.Fatalf("PagesForRoot(0) = %+v, want empty inline walk", walk)
	}
}

func TestParseLeafPayloadRejectsDescriptorAndPayloadBounds(t *testing.T) {
	t.Parallel()

	t.Run("descriptor_bounds", func(t *testing.T) {
		page := make([]byte, 32)
		_, err := parseLeafPayload(page, 2)
		if err == nil || !strings.Contains(err.Error(), "leaf descriptor bytes") {
			t.Fatalf("parseLeafPayload error = %v, want descriptor bounds error", err)
		}
	})

	t.Run("key_bounds", func(t *testing.T) {
		page := make([]byte, 64)
		binary.LittleEndian.PutUint32(page[20:24], 32)
		binary.LittleEndian.PutUint32(page[24:28], 20)
		_, err := parseLeafPayload(page, 1)
		if err == nil || !strings.Contains(err.Error(), "leaf element 0 key") {
			t.Fatalf("parseLeafPayload error = %v, want key bounds error", err)
		}
	})

	t.Run("value_bounds", func(t *testing.T) {
		page := make([]byte, 64)
		binary.LittleEndian.PutUint32(page[20:24], 16)
		binary.LittleEndian.PutUint32(page[24:28], 1)
		binary.LittleEndian.PutUint32(page[28:32], 40)
		copy(page[32:33], []byte("k"))
		_, err := parseLeafPayload(page, 1)
		if err == nil || !strings.Contains(err.Error(), "leaf element 0 value") {
			t.Fatalf("parseLeafPayload error = %v, want value bounds error", err)
		}
	})
}

func TestReadMetaRejectsInvalidMagicUnsupportedVersionAndChecksum(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(fixturePath("users.db"))
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	if len(source) < 80 {
		t.Fatalf("fixture is too short: %d bytes", len(source))
	}

	tests := []struct {
		name string
		edit func([]byte)
		want string
	}{
		{
			name: "invalid magic",
			edit: func(data []byte) {
				binary.LittleEndian.PutUint32(data[16:20], 0)
			},
			want: "invalid bbolt magic",
		},
		{
			name: "unsupported version",
			edit: func(data []byte) {
				binary.LittleEndian.PutUint32(data[20:24], Version+1)
			},
			want: "unsupported bbolt version",
		},
		{
			name: "checksum mismatch",
			edit: func(data []byte) {
				data[64] ^= 0xff
			},
			want: "Invalid meta page",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte(nil), source[:80]...)
			tt.edit(data)
			path := filepath.Join(t.TempDir(), "meta.db")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatalf("WriteFile corrupt meta: %v", err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("Open corrupt meta: %v", err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Fatalf("close corrupt meta: %v", err)
				}
			}()

			_, err = readMeta(make([]byte, 80), f, 0)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("readMeta error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func writeSyntheticFreelistDB(t *testing.T) string {
	t.Helper()

	const pageSize = 4096
	data := make([]byte, pageSize*5)
	putPageHeader(data[0:pageSize], 0, MetaPageFlag, 0, 0)
	putMetaPayload(data[0:pageSize], pageSize, 3, 2, 5, 1)
	putPageHeader(data[2*pageSize:3*pageSize], 2, FreelistPageFlag, 1, 0)
	binary.LittleEndian.PutUint64(data[2*pageSize+16:2*pageSize+24], 4)
	putPageHeader(data[4*pageSize:5*pageSize], 4, LeafPageFlag, 0, 0)

	path := filepath.Join(t.TempDir(), "freelist.db")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile synthetic bbolt db: %v", err)
	}
	return path
}

type syntheticBranchElement struct {
	key   string
	child PageID
}

func writeSyntheticBranchDB(t *testing.T, elements []syntheticBranchElement) string {
	t.Helper()

	const pageSize = 4096
	data := make([]byte, pageSize*6)
	putPageHeader(data[0:pageSize], 0, MetaPageFlag, 0, 0)
	putMetaPayload(data[0:pageSize], pageSize, 3, PgidNoFreelist, 6, 1)

	root := data[3*pageSize : 4*pageSize]
	putPageHeader(root, 3, BranchPageFlag, uint16(len(elements)), 0)
	for idx, element := range elements {
		putBranchElement(root, idx, element.key, element.child)
		if element.child == 4 || element.child == 5 {
			child := data[int(element.child)*pageSize : int(element.child+1)*pageSize]
			putPageHeader(child, element.child, LeafPageFlag, 0, 0)
		}
	}

	path := filepath.Join(t.TempDir(), "branch.db")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile synthetic branch db: %v", err)
	}
	return path
}

func putBranchElement(page []byte, idx int, key string, child PageID) {
	const descriptorStart = 16
	const descriptorSize = 16
	const keyStart = 128
	const keyStride = 16

	descStart := descriptorStart + idx*descriptorSize
	keyOffset := keyStart + idx*keyStride
	copy(page[keyOffset:keyOffset+len(key)], []byte(key))
	binary.LittleEndian.PutUint32(page[descStart:descStart+4], uint32(keyOffset-descStart))
	binary.LittleEndian.PutUint32(page[descStart+4:descStart+8], uint32(len(key)))
	binary.LittleEndian.PutUint64(page[descStart+8:descStart+16], uint64(child))
}

func putPageHeader(page []byte, id PageID, flag FlagType, count uint16, overflow uint32) {
	binary.LittleEndian.PutUint64(page[0:8], uint64(id))
	binary.LittleEndian.PutUint16(page[8:10], uint16(flag))
	binary.LittleEndian.PutUint16(page[10:12], count)
	binary.LittleEndian.PutUint32(page[12:16], overflow)
}

func putMetaPayload(page []byte, pageSize int, root PageID, freelist PageID, highWaterMark PageID, txid uint64) {
	binary.LittleEndian.PutUint32(page[16:20], Magic)
	binary.LittleEndian.PutUint32(page[20:24], Version)
	binary.LittleEndian.PutUint32(page[24:28], uint32(pageSize))
	binary.LittleEndian.PutUint64(page[32:40], uint64(root))
	binary.LittleEndian.PutUint64(page[48:56], uint64(freelist))
	binary.LittleEndian.PutUint64(page[56:64], uint64(highWaterMark))
	binary.LittleEndian.PutUint64(page[64:72], txid)
	h := fnv.New64a()
	_, _ = h.Write(page[16:72])
	binary.LittleEndian.PutUint64(page[72:80], h.Sum64())
}

func TestParsePageRejectsUnknownPageFlag(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 80)
	binary.LittleEndian.PutUint64(buf[0:8], 7)
	binary.LittleEndian.PutUint16(buf[8:10], 0x08)

	_, err := parsePage(buf)
	if err == nil || !strings.Contains(err.Error(), "unsupported bbolt page flag") {
		t.Fatalf("parsePage error = %v, want unsupported page flag", err)
	}
}

func TestInspectPageRejectsHighWaterMarkAndTruncatedPages(t *testing.T) {
	t.Parallel()

	source := fixturePath("users.db")
	sourceInspector, err := Open(source)
	if err != nil {
		t.Fatalf("Open source returned error: %v", err)
	}
	sourceConfig := sourceInspector.Config()
	if err := sourceInspector.Close(); err != nil {
		t.Fatalf("close source inspector: %v", err)
	}

	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	truncatedSize := int(sourceConfig.HighWaterMark)*int(sourceConfig.PageSize) - 1
	if truncatedSize <= 0 || truncatedSize > len(data) {
		t.Fatalf("invalid truncated size %d for fixture length %d", truncatedSize, len(data))
	}
	path := filepath.Join(t.TempDir(), "truncated.db")
	if err := os.WriteFile(path, data[:truncatedSize], 0o600); err != nil {
		t.Fatalf("WriteFile truncated fixture: %v", err)
	}

	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})

	config := inspector.Config()
	if _, err := inspector.InspectPage(config.HighWaterMark); err == nil {
		t.Fatal("InspectPage(high water mark) returned nil error")
	}
	page, err := inspector.InspectPage(config.HighWaterMark - 1)
	if err != nil {
		t.Fatalf("InspectPage(last truncated page) returned error: %v", err)
	}
	if page.Classification != PageClassTruncated {
		t.Fatalf("last page classification = %q, want %q", page.Classification, PageClassTruncated)
	}
}

func currentBoltMetaFromDB(t *testing.T, db *bolt.DB) boltMeta {
	t.Helper()

	meta0 := reflectBoltMeta(t, db, "meta0")
	meta1 := reflectBoltMeta(t, db, "meta1")

	metaA, metaB := meta0, meta1
	if meta1.txid > meta0.txid {
		metaA, metaB = meta1, meta0
	}

	if metaA.valid() {
		return metaA
	}
	if metaB.valid() {
		return metaB
	}
	t.Fatalf("bolt opened fixture without a valid reflected meta page")
	return boltMeta{}
}

func reflectBoltMeta(t *testing.T, db *bolt.DB, fieldName string) boltMeta {
	t.Helper()

	field := reflect.ValueOf(db).Elem().FieldByName(fieldName)
	if !field.IsValid() {
		t.Fatalf("bbolt.DB has no %s field", fieldName)
	}
	if field.IsNil() {
		t.Fatalf("bbolt.DB.%s is nil", fieldName)
	}

	ptr := unsafe.Pointer(field.Pointer())
	return *(*boltMeta)(ptr)
}

func (m boltMeta) valid() bool {
	return m.magic == Magic && m.version == Version && m.checksum == m.sum64()
}

func (m boltMeta) sum64() uint64 {
	h := fnv.New64a()
	raw := (*[unsafe.Offsetof(boltMeta{}.checksum)]byte)(unsafe.Pointer(&m))[:]
	_, _ = h.Write(raw)
	return h.Sum64()
}

func assertMeta(t *testing.T, got Meta, start int, size int) {
	t.Helper()

	want := Meta{StartOffset: start, Size: size}
	if got != want {
		t.Fatalf("meta = %+v, want %+v", got, want)
	}
}

func assertLeafElementDescriptorSpans(t *testing.T, element LeafElement) {
	t.Helper()

	assertMeta(t, element.Meta, element.Meta.StartOffset, 16)
	assertMeta(t, element.Flags.Meta, element.Meta.StartOffset, 4)
	assertMeta(t, element.Pos.Meta, element.Meta.StartOffset+4, 4)
	assertMeta(t, element.KeySize.Meta, element.Meta.StartOffset+8, 4)
	assertMeta(t, element.ValueSize.Meta, element.Meta.StartOffset+12, 4)
}

func assertBranchElementDescriptorSpans(t *testing.T, element BranchElement) {
	t.Helper()

	assertMeta(t, element.Meta, element.Meta.StartOffset, 16)
	assertMeta(t, element.Pos.Meta, element.Meta.StartOffset, 4)
	assertMeta(t, element.KeySize.Meta, element.Meta.StartOffset+4, 4)
	assertMeta(t, element.PageID.Meta, element.Meta.StartOffset+8, 8)
}
