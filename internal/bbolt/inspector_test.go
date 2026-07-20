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
	if _, err := inspector.InspectPage(config.HighWaterMark - 1); err == nil || !strings.Contains(err.Error(), "truncated bbolt page") {
		t.Fatalf("InspectPage(last truncated page) error = %v, want truncated-page error", err)
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
