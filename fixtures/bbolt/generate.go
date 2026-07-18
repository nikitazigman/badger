//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	bolt "go.etcd.io/bbolt"
)

func main() {
	if err := os.RemoveAll("fixtures/bbolt/empty"); err != nil {
		panic(err)
	}
	if err := os.RemoveAll("fixtures/bbolt/single_bucket"); err != nil {
		panic(err)
	}
	if err := os.RemoveAll("fixtures/bbolt/nested_buckets"); err != nil {
		panic(err)
	}
	if err := os.RemoveAll("fixtures/bbolt/large_values"); err != nil {
		panic(err)
	}

	mustCreate("fixtures/bbolt/empty/empty.db", func(db *bolt.DB) error {
		return nil
	})

	mustCreate("fixtures/bbolt/single_bucket/users.db", func(db *bolt.DB) error {
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

	mustCreate("fixtures/bbolt/nested_buckets/app.db", func(db *bolt.DB) error {
		if err := db.Update(func(tx *bolt.Tx) error {
			accounts, err := tx.CreateBucketIfNotExists([]byte("accounts"))
			if err != nil {
				return err
			}
			alice, err := accounts.CreateBucketIfNotExists([]byte("alice"))
			if err != nil {
				return err
			}
			if err := alice.Put([]byte("plan"), []byte("pro")); err != nil {
				return err
			}
			return accounts.Put([]byte("count"), []byte("1"))
		}); err != nil {
			return err
		}
		return db.Update(func(tx *bolt.Tx) error {
			settings, err := tx.CreateBucketIfNotExists([]byte("settings"))
			if err != nil {
				return err
			}
			if err := settings.Put([]byte("theme"), []byte("dark")); err != nil {
				return err
			}
			return settings.Put([]byte("timezone"), []byte("UTC"))
		})
	})

	mustCreate("fixtures/bbolt/large_values/events.db", func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			events, err := tx.CreateBucketIfNotExists([]byte("events"))
			if err != nil {
				return err
			}
			for i := range 128 {
				key := []byte(fmt.Sprintf("event-%03d", i))
				value := []byte(fmt.Sprintf("payload-%03d-%s", i, repeated('x', 512+(i%7)*83)))
				if err := events.Put(key, value); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func mustCreate(path string, fill func(*bolt.DB) error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}

	db, err := bolt.Open(path, 0o600, nil)
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

func repeated(ch byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = ch
	}
	return string(buf)
}
