package storage

import (
	"bytes"
	"fmt"
	"os"
)

var sqliteMagic = []byte("SQLite format 3\x00")

func Open(path string) (Database, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	header := make([]byte, len(sqliteMagic))
	if _, err := f.ReadAt(header, 0); err != nil {
		return nil, err
	}

	if bytes.Equal(header, sqliteMagic) {
		return openSQLite(path)
	}

	return nil, fmt.Errorf("unsupported database engine")
}
