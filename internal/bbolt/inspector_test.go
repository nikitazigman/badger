package bbolt

import (
	"hash/fnv"
	"path/filepath"
	"reflect"
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
		Version:  meta.version,
		PageSize: meta.pageSize,
		Root:     PageID(meta.root.root),
		Freelist: PageID(meta.freelist),
	}
	if config.PageSize != uint32(db.Info().PageSize) {
		t.Fatalf("reflected page size = %d, bolt.Info().PageSize = %d", config.PageSize, db.Info().PageSize)
	}
	return config
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
