package sqlite

import (
	"fmt"
	"io"
	"os"
)

type Inspector struct {
	path     string
	file     *os.File
	dbHeader *DBHeader
}

func Open(path string) (*Inspector, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 100)
	_, err = f.ReadAt(buf, 0)
	if err != nil {
		return nil, err
	}

	header, err := parseHeader(buf)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &Inspector{
		path:     path,
		file:     f,
		dbHeader: header,
	}, nil
}

func (i *Inspector) Close() error {
	return i.file.Close()
}

func (i *Inspector) readPage(number uint32) ([]byte, error) {
	if i == nil || i.file == nil {
		return nil, fmt.Errorf("inspector is not open")
	}
	if i.dbHeader == nil {
		return nil, fmt.Errorf("database header is not loaded")
	}
	if number == 0 {
		return nil, fmt.Errorf("page number must be >= 1")
	}
	if i.dbHeader.DatabasePageCount != 0 && number > i.dbHeader.DatabasePageCount {
		return nil, fmt.Errorf("page number %d out of range (page count: %d)", number, i.dbHeader.DatabasePageCount)
	}

	pageSize := i.dbHeader.PageSize
	if pageSize == 0 {
		return nil, fmt.Errorf("invalid page size: 0")
	}

	offset := uint64(number-1) * uint64(pageSize)
	buf := make([]byte, pageSize)
	n, err := i.file.ReadAt(buf, int64(offset))
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("page %d is truncated: read %d of %d bytes", number, n, pageSize)
		}
		return nil, fmt.Errorf("read page %d at offset %d: %w", number, offset, err)
	}

	return buf, nil
}

type MetadataInspection struct {
	Path          string
	DBHeader      DBHeader
	SchemaRecords []Row
}

func (i *Inspector) InspectDatabaseMetadata() (*MetadataInspection, error) {
	definition, err := ParseSchemaDefinitionSQL(sqliteSchemaTableSQL)
	if err != nil {
		return nil, err
	}

	// The sqlite_schema table is rooted at page 1, but when the schema is large
	// enough page 1 becomes an interior b-tree node and the rows live on the leaf
	// pages it points to. Walk the whole b-tree from page 1 so we read every
	// schema row regardless of tree depth, rather than assuming page 1 is a leaf.
	walk, err := i.PagesForRoot(1)
	if err != nil {
		return nil, err
	}

	schemaRecords := make([]Row, 0, len(walk.Pages))
	for _, pageNumber := range walk.Pages {
		page, err := i.InspectPage(pageNumber)
		if err != nil {
			return nil, err
		}

		// Only leaf table pages carry rows; interior pages just route to children.
		if page.BTreePage.PageHeader.PageKind.Value != LeafTableBTreePage {
			continue
		}

		for idx, cell := range page.BTreePage.TableLeafCells {
			if cell.ParsedPayload == nil {
				return nil, fmt.Errorf("sqlite_schema cell %d on page %d payload is missing", idx, pageNumber)
			}

			record, err := ParseRecord(cell.ParsedPayload, definition)
			if err != nil {
				return nil, fmt.Errorf("sqlite_schema cell %d on page %d: %w", idx, pageNumber, err)
			}
			schemaRecords = append(schemaRecords, record)
		}
	}

	return &MetadataInspection{
		Path:          i.path,
		DBHeader:      *i.dbHeader,
		SchemaRecords: schemaRecords,
	}, nil
}

type PageInspection struct {
	PageNumber uint32
	DBHeader   *DBHeader
	BTreePage  BTreePage
}

func (i *Inspector) InspectPage(number uint32) (*PageInspection, error) {
	page, err := i.readPage(number)
	if err != nil {
		return nil, err
	}
	if i == nil || i.dbHeader == nil {
		return nil, fmt.Errorf("database header is not loaded")
	}

	btreePage, err := parseBTreePage(page, number, i.dbHeader.ReservedBytesPerPage)
	if err != nil {
		return nil, err
	}

	inspection := &PageInspection{
		PageNumber: number,
		BTreePage:  *btreePage,
	}
	if number == 1 {
		inspection.DBHeader = i.dbHeader
	}

	return inspection, nil
}
