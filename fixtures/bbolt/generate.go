//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"
)

func main() {
	cleanupFixtures()

	mustCreate("fixtures/bbolt/empty.db", func(db *bolt.DB) error {
		return nil
	})

	mustCreate("fixtures/bbolt/users.db", func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			bucket, err := tx.CreateBucketIfNotExists([]byte("users"))
			if err != nil {
				return err
			}
			for i, name := range []string{"ada", "grace", "katherine"} {
				if err := bucket.Put([]byte(strconv.Itoa(i+1)), []byte(name)); err != nil {
					return err
				}
			}
			return nil
		})
	})

	mustCreate("fixtures/bbolt/many_buckets.db", func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			for i := range 24 {
				name := []byte(fmt.Sprintf("bucket_%02d", i))
				bucket, err := tx.CreateBucketIfNotExists(name)
				if err != nil {
					return err
				}
				for j := range 8 {
					if err := bucket.Put([]byte(fmt.Sprintf("key_%02d", j)), []byte(fmt.Sprintf("value_%02d_%02d", i, j))); err != nil {
						return err
					}
				}
			}
			return nil
		})
	})

	mustCreate("fixtures/bbolt/nested_inline.db", func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			for _, rootName := range []string{"alpha", "beta", "gamma"} {
				root, err := tx.CreateBucketIfNotExists([]byte(rootName))
				if err != nil {
					return err
				}
				if err := root.Put([]byte("root_value"), []byte("root_"+rootName)); err != nil {
					return err
				}
				current := root
				for depth := 1; depth <= 5; depth++ {
					inline, err := current.CreateBucketIfNotExists([]byte(fmt.Sprintf("inline_%d", depth)))
					if err != nil {
						return err
					}
					if err := inline.Put([]byte("kind"), []byte("small_inline_bucket")); err != nil {
						return err
					}

					next, err := current.CreateBucketIfNotExists([]byte(fmt.Sprintf("level_%d", depth)))
					if err != nil {
						return err
					}
					if err := next.Put([]byte("depth"), []byte(strconv.Itoa(depth))); err != nil {
						return err
					}
					current = next
				}
			}
			return nil
		})
	})

	mustCreate("fixtures/bbolt/overflow.db", func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			large, err := tx.CreateBucketIfNotExists([]byte("large_values"))
			if err != nil {
				return err
			}
			for i := range 48 {
				key := []byte(fmt.Sprintf("blob_%03d", i))
				value := []byte(fmt.Sprintf("payload_%03d_%s", i, repeated(byte('a'+i%26), 32*1024+(i%5)*8192)))
				if err := large.Put(key, value); err != nil {
					return err
				}
			}
			return nil
		})
	})

	mustCreate("fixtures/bbolt/branch_pages.db", func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			events, err := tx.CreateBucketIfNotExists([]byte("events"))
			if err != nil {
				return err
			}
			for i := range 12000 {
				key := []byte(fmt.Sprintf("event_%05d", i))
				value := []byte(fmt.Sprintf("type=branch-test seq=%05d payload=%s", i, repeated(byte('0'+i%10), 96)))
				if err := events.Put(key, value); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func cleanupFixtures() {
	for _, path := range []string{
		"fixtures/bbolt/empty",
		"fixtures/bbolt/single_bucket",
		"fixtures/bbolt/nested_buckets",
		"fixtures/bbolt/large_values",
		"fixtures/bbolt/empty.db",
		"fixtures/bbolt/users.db",
		"fixtures/bbolt/many_buckets.db",
		"fixtures/bbolt/nested_inline.db",
		"fixtures/bbolt/overflow.db",
		"fixtures/bbolt/branch_pages.db",
	} {
		if err := os.RemoveAll(path); err != nil {
			panic(err)
		}
	}
}

func mustCreate(path string, fill func(*bolt.DB) error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}

	db, err := bolt.Open(path, 0o600, fixtureOptions())
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			panic(err)
		}
	}()

	if err := fill(db); err != nil {
		panic(err)
	}
}

func fixtureOptions() *bolt.Options {
	return &bolt.Options{
		Timeout:         time.Second,
		NoGrowSync:      true,
		FreelistType:    bolt.FreelistArrayType,
		InitialMmapSize: 64 << 20,
		PageSize:        4096,
		NoSync:          true,
	}
}

func repeated(ch byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = ch
	}
	return string(buf)
}
