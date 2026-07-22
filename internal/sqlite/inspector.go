package sqlite

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type Inspector struct {
	path                 string
	file                 *os.File
	dbHeader             *DBHeader
	cacheMu              sync.Mutex
	freelistIndex        *freelistIndex
	overflowIndex        *overflowIndex
	loadingOverflowIndex bool
}

type freelistIndex struct {
	trunkPages map[uint32]struct{}
	leafPages  map[uint32]struct{}
}

type overflowIndex struct {
	pages map[uint32]OverflowPageOwner
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
	walk, err := i.pagesForRoot(1, false)
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
	PageNumber        uint32
	DBHeader          *DBHeader
	Format            PageFormat
	Raw               []byte
	BTreePage         BTreePage
	OverflowPage      *OverflowPage
	OverflowOwner     *OverflowPageOwner
	FreelistTrunkPage *FreelistTrunkPage
	FreelistLeafPage  *FreelistLeafPage
	UnknownPage       *UnknownPage
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
		var unsupported *UnsupportedPageKindError
		if !errors.As(err, &unsupported) {
			return nil, err
		}

		if i.isFreelistTrunkPage(number) {
			trunk, err := parseFreelistTrunkPage(page, number, i.dbHeader.ReservedBytesPerPage)
			if err != nil {
				return nil, fmt.Errorf("page %d freelist trunk parse failed: %w", number, err)
			}
			return i.pageInspection(number, page, PageFormatFreelistTrunk, func(inspection *PageInspection) {
				inspection.FreelistTrunkPage = trunk
			}), nil
		}

		if i.isFreelistLeafPage(number) {
			leaf, err := parseFreelistLeafPage(page, number, i.dbHeader.ReservedBytesPerPage)
			if err != nil {
				return nil, fmt.Errorf("page %d freelist leaf parse failed: %w", number, err)
			}
			return i.pageInspection(number, page, PageFormatFreelistLeaf, func(inspection *PageInspection) {
				inspection.FreelistLeafPage = leaf
			}), nil
		}

		owner, overflowIndexErr := i.overflowPageOwner(number)
		overflow, overflowErr := parseOverflowPage(page, number, i.dbHeader.ReservedBytesPerPage)
		if overflowErr == nil && owner != nil {
			return i.pageInspection(number, page, PageFormatOverflow, func(inspection *PageInspection) {
				inspection.OverflowPage = overflow
				inspection.OverflowOwner = owner
			}), nil
		}
		if overflowErr == nil && overflowIndexErr != nil && i.validOverflowNextPage(overflow.NextPage.Value) {
			return i.pageInspection(number, page, PageFormatOverflow, func(inspection *PageInspection) {
				inspection.OverflowPage = overflow
			}), nil
		}

		return i.pageInspection(number, page, PageFormatUnknown, func(inspection *PageInspection) {
			inspection.UnknownPage = parseUnknownPage(page, number)
		}), nil
	}

	return i.pageInspection(number, page, PageFormatBTree, func(inspection *PageInspection) {
		inspection.BTreePage = *btreePage
	}), nil
}

func (i *Inspector) pageInspection(number uint32, page []byte, format PageFormat, fill func(*PageInspection)) *PageInspection {
	inspection := &PageInspection{
		PageNumber: number,
		Format:     format,
		Raw:        append([]byte(nil), page...),
	}
	if number == 1 {
		inspection.DBHeader = i.dbHeader
	}
	if fill != nil {
		fill(inspection)
	}
	return inspection
}

func (i *Inspector) isFreelistTrunkPage(number uint32) bool {
	index, err := i.loadFreelistIndex()
	if err != nil || index == nil {
		return false
	}
	_, ok := index.trunkPages[number]
	return ok
}

func (i *Inspector) isFreelistLeafPage(number uint32) bool {
	index, err := i.loadFreelistIndex()
	if err != nil || index == nil {
		return false
	}
	_, ok := index.leafPages[number]
	return ok
}

func (i *Inspector) loadFreelistIndex() (*freelistIndex, error) {
	if i == nil || i.dbHeader == nil {
		return nil, fmt.Errorf("database header is not loaded")
	}
	i.cacheMu.Lock()
	if i.freelistIndex != nil {
		index := i.freelistIndex
		i.cacheMu.Unlock()
		return index, nil
	}
	i.cacheMu.Unlock()

	index := &freelistIndex{
		trunkPages: map[uint32]struct{}{},
		leafPages:  map[uint32]struct{}{},
	}

	current := i.dbHeader.FirstFreelistTrunkPage
	seen := map[uint32]struct{}{}
	for current != 0 {
		if _, ok := seen[current]; ok {
			return nil, fmt.Errorf("freelist trunk chain contains cycle at page %d", current)
		}
		seen[current] = struct{}{}
		if current > i.dbHeader.DatabasePageCount {
			return nil, fmt.Errorf("freelist trunk page %d out of range", current)
		}

		page, err := i.readPage(current)
		if err != nil {
			return nil, err
		}
		trunk, err := parseFreelistTrunkPage(page, current, i.dbHeader.ReservedBytesPerPage)
		if err != nil {
			return nil, err
		}

		index.trunkPages[current] = struct{}{}
		for _, leaf := range trunk.LeafPages {
			if leaf.Value == 0 || leaf.Value > i.dbHeader.DatabasePageCount {
				continue
			}
			index.leafPages[leaf.Value] = struct{}{}
		}
		current = trunk.NextTrunkPage.Value
	}

	i.cacheMu.Lock()
	if i.freelistIndex == nil {
		i.freelistIndex = index
	}
	index = i.freelistIndex
	i.cacheMu.Unlock()
	return index, nil
}

func (i *Inspector) overflowPageOwner(number uint32) (*OverflowPageOwner, error) {
	index, err := i.loadOverflowIndex()
	if err != nil || index == nil {
		return nil, err
	}
	owner, ok := index.pages[number]
	if !ok {
		return nil, nil
	}
	return overflowPageOwnerPtr(owner), nil
}

func (i *Inspector) loadOverflowIndex() (*overflowIndex, error) {
	if i == nil || i.dbHeader == nil {
		return nil, fmt.Errorf("database header is not loaded")
	}
	i.cacheMu.Lock()
	if i.overflowIndex != nil {
		index := i.overflowIndex
		i.cacheMu.Unlock()
		return index, nil
	}
	if i.loadingOverflowIndex {
		i.cacheMu.Unlock()
		return nil, fmt.Errorf("overflow index is already loading")
	}
	i.loadingOverflowIndex = true
	i.cacheMu.Unlock()
	defer func() {
		i.cacheMu.Lock()
		i.loadingOverflowIndex = false
		i.cacheMu.Unlock()
	}()

	index := &overflowIndex{pages: map[uint32]OverflowPageOwner{}}
	roots := []uint32{1}
	metadata, err := i.InspectDatabaseMetadata()
	if err == nil {
		for _, record := range metadata.SchemaRecords {
			root, ok := schemaRecordRootPage(record["rootpage"])
			if ok && root != 0 {
				roots = append(roots, root)
			}
		}
	}

	seenRoots := map[uint32]struct{}{}
	for _, root := range roots {
		if _, ok := seenRoots[root]; ok {
			continue
		}
		seenRoots[root] = struct{}{}

		walk, err := i.pagesForRoot(root, false)
		if err != nil {
			continue
		}
		for _, pageNumber := range walk.Pages {
			inspection, err := i.InspectPage(pageNumber)
			if err != nil || inspection.Format != PageFormatBTree {
				continue
			}
			i.collectPageOverflowChains(index, inspection)
		}
	}

	i.cacheMu.Lock()
	if i.overflowIndex == nil {
		i.overflowIndex = index
	}
	index = i.overflowIndex
	i.cacheMu.Unlock()
	return index, nil
}

func (i *Inspector) collectPageOverflowChains(index *overflowIndex, inspection *PageInspection) {
	for idx, cell := range inspection.BTreePage.TableLeafCells {
		i.collectPayloadOverflowChain(index, cell.ParsedPayload, OverflowPageOwner{
			ParentPage:    inspection.PageNumber,
			CellIndex:     idx,
			CellKind:      "table leaf cell",
			ParentCell:    cell.Meta,
			ParentPayload: recordPayloadMeta(cell.ParsedPayload),
		})
	}
	for idx, cell := range inspection.BTreePage.IndexLeafCells {
		i.collectPayloadOverflowChain(index, cell.ParsedPayload, OverflowPageOwner{
			ParentPage:    inspection.PageNumber,
			CellIndex:     idx,
			CellKind:      "index leaf cell",
			ParentCell:    cell.Meta,
			ParentPayload: recordPayloadMeta(cell.ParsedPayload),
		})
	}
	for idx, cell := range inspection.BTreePage.IndexInteriorCells {
		i.collectPayloadOverflowChain(index, cell.ParsedPayload, OverflowPageOwner{
			ParentPage:    inspection.PageNumber,
			CellIndex:     idx,
			CellKind:      "index interior cell",
			ParentCell:    cell.Meta,
			ParentPayload: recordPayloadMeta(cell.ParsedPayload),
		})
	}
}

func (i *Inspector) collectPayloadOverflowChain(index *overflowIndex, payload *RecordPayload, owner OverflowPageOwner) {
	if index == nil || payload == nil || payload.OverflowFirstPage == nil {
		return
	}

	current := payload.OverflowFirstPage.Value
	owner.FirstPage = current
	owner.OverflowPointer = payload.OverflowFirstPage.Meta
	seen := map[uint32]struct{}{}
	chain := []uint32{}
	for current != 0 {
		if _, ok := seen[current]; ok {
			return
		}
		seen[current] = struct{}{}
		if i.dbHeader.DatabasePageCount != 0 && current > i.dbHeader.DatabasePageCount {
			return
		}

		page, err := i.readPage(current)
		if err != nil {
			return
		}
		overflow, err := parseOverflowPage(page, current, i.dbHeader.ReservedBytesPerPage)
		if err != nil || !i.validOverflowNextPage(overflow.NextPage.Value) {
			return
		}
		chain = append(chain, current)
		current = overflow.NextPage.Value
	}
	for idx, page := range chain {
		if _, ok := index.pages[page]; ok {
			continue
		}
		pageOwner := owner
		pageOwner.PartIndex = idx + 1
		pageOwner.PartCount = len(chain)
		index.pages[page] = pageOwner
	}
}

func schemaRecordRootPage(value any) (uint32, bool) {
	switch v := value.(type) {
	case uint32:
		return v, true
	case uint64:
		if v <= uint64(^uint32(0)) {
			return uint32(v), true
		}
	case int64:
		if v >= 0 && v <= int64(^uint32(0)) {
			return uint32(v), true
		}
	case int:
		if v >= 0 {
			return uint32(v), true
		}
	}
	return 0, false
}

func (i *Inspector) validOverflowNextPage(next uint32) bool {
	return next == 0 || (i.dbHeader != nil && next <= i.dbHeader.DatabasePageCount)
}

func overflowPageOwnerPtr(owner OverflowPageOwner) *OverflowPageOwner {
	copied := owner
	return &copied
}

func recordPayloadMeta(payload *RecordPayload) Meta {
	if payload == nil {
		return Meta{}
	}
	return payload.Meta
}
