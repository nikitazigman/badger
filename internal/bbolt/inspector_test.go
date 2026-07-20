package bbolt

import (
	"encoding/binary"
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
		{name: "empty", path: fixturePath("empty", "empty.db")},
		{name: "single bucket", path: fixturePath("single_bucket", "users.db")},
		{name: "nested buckets", path: fixturePath("nested_buckets", "app.db")},
		{name: "large values", path: fixturePath("large_values", "events.db")},
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

	inspector, err := Open(fixturePath("single_bucket", "users.db"))
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

	inspector, err := Open(fixturePath("single_bucket", "users.db"))
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
		fixturePath("single_bucket", "users.db"),
		fixturePath("nested_buckets", "app.db"),
		fixturePath("large_values", "events.db"),
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

	inspector, err := Open(fixturePath("single_bucket", "users.db"))
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
			if len(page.LeafPayload.NestedBucket) != 0 {
				t.Fatal("task 04 should leave bucket values opaque; nested bucket headers belong to task 06")
			}
			return
		}
	}

	t.Fatal("single_bucket fixture did not contain a bucket leaf entry")
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

	source, err := os.ReadFile(fixturePath("single_bucket", "users.db"))
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

	source := fixturePath("single_bucket", "users.db")
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
