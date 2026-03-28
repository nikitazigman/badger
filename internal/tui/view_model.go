package tui

import (
	"fmt"
	"strconv"

	"github.com/nikitazigman/badger/internal/sqlite"
)

type schemaObjectViewModel struct {
	Type      string
	Name      string
	TableName string
	RootPage  uint32
	SQL       string
}

type labelValue struct {
	Label string
	Value string
}

type databaseViewModel struct {
	Path               string
	PageSize           uint32
	PageCount          uint32
	DatabaseSizeBytes  uint64
	FreelistPageCount  uint32
	EncodingLabel      string
	SQLiteVersionLabel string
	DBHeader           sqlite.DBHeader
	HeaderRows         []labelValue
	Tables             []schemaObjectViewModel
	Indexes            []schemaObjectViewModel
}

func newDatabaseViewModel(metadata *sqlite.MetadataInspection) (databaseViewModel, error) {
	if metadata == nil {
		return databaseViewModel{}, fmt.Errorf("metadata is nil")
	}

	db := databaseViewModel{
		Path:               metadata.Path,
		PageSize:           metadata.DBHeader.PageSize,
		PageCount:          metadata.DBHeader.DatabasePageCount,
		DatabaseSizeBytes:  uint64(metadata.DBHeader.PageSize) * uint64(metadata.DBHeader.DatabasePageCount),
		FreelistPageCount:  metadata.DBHeader.FreelistPageCount,
		EncodingLabel:      textEncodingLabel(metadata.DBHeader.TextEncoding),
		SQLiteVersionLabel: sqliteVersionLabel(metadata.DBHeader.SQLiteVersionNumber),
		DBHeader:           metadata.DBHeader,
		HeaderRows:         buildHeaderRows(metadata.DBHeader),
	}

	for _, row := range metadata.SchemaRecords {
		object := schemaObjectViewModel{
			Type:      stringValue(row["type"]),
			Name:      stringValue(row["name"]),
			TableName: stringValue(row["tbl_name"]),
			RootPage:  uint32Value(row["rootpage"]),
			SQL:       stringValue(row["sql"]),
		}
		switch object.Type {
		case "table":
			db.Tables = append(db.Tables, object)
		case "index":
			db.Indexes = append(db.Indexes, object)
		}
	}

	return db, nil
}

func buildHeaderRows(header sqlite.DBHeader) []labelValue {
	return []labelValue{
		{Label: "Page size", Value: strconv.FormatUint(uint64(header.PageSize), 10)},
		{Label: "Page count", Value: strconv.FormatUint(uint64(header.DatabasePageCount), 10)},
		{Label: "Read version", Value: strconv.FormatUint(uint64(header.ReadVersion), 10)},
		{Label: "Write version", Value: strconv.FormatUint(uint64(header.WriteVersion), 10)},
		{Label: "Reserved bytes/page", Value: strconv.FormatUint(uint64(header.ReservedBytesPerPage), 10)},
		{Label: "Freelist pages", Value: strconv.FormatUint(uint64(header.FreelistPageCount), 10)},
		{Label: "Schema cookie", Value: strconv.FormatUint(uint64(header.SchemaCookie), 10)},
		{Label: "Schema format", Value: strconv.FormatUint(uint64(header.SchemaFormat), 10)},
		{Label: "Encoding", Value: textEncodingLabel(header.TextEncoding)},
		{Label: "User version", Value: strconv.FormatUint(uint64(header.UserVersion), 10)},
		{Label: "Application ID", Value: strconv.FormatUint(uint64(header.ApplicationID), 10)},
		{Label: "SQLite version", Value: sqliteVersionLabel(header.SQLiteVersionNumber)},
	}
}

func textEncodingLabel(value uint32) string {
	switch value {
	case 1:
		return "UTF-8"
	case 2:
		return "UTF-16le"
	case 3:
		return "UTF-16be"
	default:
		return fmt.Sprintf("unknown (%d)", value)
	}
}

func sqliteVersionLabel(value uint32) string {
	major := value / 1000000
	minor := (value / 1000) % 1000
	patch := value % 1000
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func uint32Value(value any) uint32 {
	switch v := value.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int64:
		return uint32(v)
	case int:
		return uint32(v)
	case float64:
		return uint32(v)
	default:
		return 0
	}
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
