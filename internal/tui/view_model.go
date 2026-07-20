package tui

import (
	"fmt"
	"strconv"

	"github.com/nikitazigman/badger/internal/storage"
)

type schemaObjectViewModel struct {
	ID        storage.BTreeID
	Kind      storage.BTreeKind
	Type      string
	Name      string
	TableName string
	RootPage  uint64
	SQL       string
	IsSystem  bool
}

type labelValue struct {
	Label string
	Value string
}

type databaseViewModel struct {
	Path               string
	PageSize           uint64
	PageCount          uint64
	FirstPageID        uint64
	DatabaseSizeBytes  uint64
	FreelistPageCount  uint64
	EncodingLabel      string
	SQLiteVersionLabel string
	HeaderRows         []storage.Field
	Tables             []schemaObjectViewModel
	Indexes            []schemaObjectViewModel
}

func newDatabaseViewModel(overview *storage.DatabaseOverview) (databaseViewModel, error) {
	if overview == nil {
		return databaseViewModel{}, fmt.Errorf("database overview is nil")
	}

	db := databaseViewModel{
		Path:               overview.Path,
		PageSize:           overview.PageSizeBytes,
		PageCount:          overview.PageCount,
		FirstPageID:        overview.FirstPageID,
		DatabaseSizeBytes:  overview.DatabaseSizeBytes,
		FreelistPageCount:  uint64FromHeader(overview.HeaderRows, "Freelist pages"),
		EncodingLabel:      stringFromHeader(overview.HeaderRows, "Encoding"),
		SQLiteVersionLabel: stringFromHeader(overview.HeaderRows, "SQLite version"),
		HeaderRows:         overview.HeaderRows,
	}

	for _, item := range overview.BTrees {
		object := schemaObjectViewModel{
			ID:        item.ID,
			Kind:      item.Kind,
			Type:      string(item.Kind),
			Name:      item.Name,
			TableName: fieldValue(item.Rows, "Table"),
			SQL:       fieldValue(item.Rows, "SQL"),
			IsSystem:  item.System,
		}
		if object.Type == string(storage.BTreeRootless) {
			object.Type = fieldValue(item.Rows, "Type")
		}
		if item.RootPage != nil {
			object.RootPage = item.RootPage.ID
		}

		switch item.Kind {
		case storage.BTreeTable, storage.BTreeRootless:
			db.Tables = append(db.Tables, object)
		case storage.BTreeIndex:
			db.Indexes = append(db.Indexes, object)
		}
	}

	return db, nil
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}

	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	suffixes := []string{"KiB", "MiB", "GiB", "TiB"}
	return fmt.Sprintf("%.1f %s", float64(value)/float64(div), suffixes[exp])
}

func fieldValue(rows []storage.Field, label string) string {
	for _, row := range rows {
		if row.Label == label {
			return row.Value
		}
	}
	return ""
}

func stringFromHeader(rows []storage.Field, label string) string {
	return fieldValue(rows, label)
}

func uint64FromHeader(rows []storage.Field, label string) uint64 {
	value, err := strconv.ParseUint(fieldValue(rows, label), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
