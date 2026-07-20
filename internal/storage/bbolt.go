package storage

import (
	"fmt"
	"strconv"

	"github.com/nikitazigman/badger/internal/bbolt"
)

const blockMetaPayload = "meta_payload"

type bboltDatabase struct {
	inspector *bbolt.Inspector
	path      string
	overview  *DatabaseOverview
	btrees    map[BTreeID]BTreeItem
}

func openBbolt(path string) (Database, error) {
	inspector, err := bbolt.Open(path)
	if err != nil {
		return nil, err
	}
	return &bboltDatabase{inspector: inspector, path: path}, nil
}

func (db *bboltDatabase) Close() error {
	return db.inspector.Close()
}

func (db *bboltDatabase) Engine() Engine {
	return EngineBbolt
}

func (db *bboltDatabase) Overview() (*DatabaseOverview, error) {
	if db.overview != nil {
		return db.overview, nil
	}

	config := db.inspector.Config()
	root := PageRef{ID: uint64(config.Root)}
	rootBucket := BTreeItem{
		ID:       "bucket:root",
		Kind:     BTreeBucket,
		Name:     "root",
		RootPage: &root,
		Rows: []Field{
			{Label: "Type", Value: "bucket"},
			{Label: "Name", Value: "root"},
			{Label: "Root page", Value: strconv.FormatUint(uint64(config.Root), 10)},
			{Label: "Freelist page", Value: strconv.FormatUint(uint64(config.Freelist), 10)},
			{Label: "High water mark", Value: strconv.FormatUint(uint64(config.HighWaterMark), 10)},
		},
	}

	overview := &DatabaseOverview{
		Path:              db.path,
		PageSizeBytes:     uint64(config.PageSize),
		PageCount:         uint64(config.HighWaterMark),
		FirstPageID:       0,
		DatabaseSizeBytes: uint64(config.PageSize) * uint64(config.HighWaterMark),
		HeaderRows:        bboltHeaderRows(config),
		BTrees:            []BTreeItem{rootBucket},
	}

	db.btrees = map[BTreeID]BTreeItem{rootBucket.ID: rootBucket}
	db.overview = overview
	return overview, nil
}

func (db *bboltDatabase) InspectPage(ref PageRef) (*PageInspection, error) {
	page, err := db.inspector.InspectPage(bbolt.PageID(ref.ID))
	if err != nil {
		return nil, err
	}
	return adaptBboltPage(page), nil
}

func (db *bboltDatabase) PagesForBTree(id BTreeID) ([]PageRef, error) {
	if db.btrees == nil {
		if _, err := db.Overview(); err != nil {
			return nil, err
		}
	}
	item, ok := db.btrees[id]
	if !ok {
		return nil, fmt.Errorf("b-tree %q not found", id)
	}
	if item.RootPage == nil {
		return []PageRef{}, nil
	}
	return []PageRef{*item.RootPage}, nil
}

func bboltHeaderRows(config bbolt.BboltConfig) []Field {
	return []Field{
		{Label: "Page size", Value: strconv.FormatUint(uint64(config.PageSize), 10)},
		{Label: "Version", Value: strconv.FormatUint(uint64(config.Version), 10)},
		{Label: "Root page", Value: strconv.FormatUint(uint64(config.Root), 10)},
		{Label: "Freelist page", Value: strconv.FormatUint(uint64(config.Freelist), 10)},
		{Label: "High water mark", Value: strconv.FormatUint(uint64(config.HighWaterMark), 10)},
		{Label: "Transaction ID", Value: strconv.FormatUint(config.TransactionID, 10)},
	}
}

func adaptBboltPage(page *bbolt.BTreePage) *PageInspection {
	pageSize := len(page.Raw)
	pageID := page.Header.ID.Value
	pageFlags := page.Header.Flags.Value
	offset := uint64(pageID) * uint64(pageSize)
	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(pageID), 10)},
		{Label: "Type", Value: bboltPageKindLabel(pageFlags)},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", pageSize)},
		{Label: "File offset", Value: strconv.FormatUint(offset, 10)},
		Blank(),
		Section("HEADER"),
		{Label: "Header page id", Value: strconv.FormatUint(uint64(page.Header.ID.Value), 10), Span: bboltSpanPtr(page.Header.ID.Meta)},
		{Label: "Flags", Value: bboltPageFlagLabel(page.Header.Flags.Value), Span: bboltSpanPtr(page.Header.Flags.Meta)},
		{Label: "Count", Value: strconv.FormatUint(uint64(page.Header.Count.Value), 10), Span: bboltSpanPtr(page.Header.Count.Meta)},
		{Label: "Overflow", Value: strconv.FormatUint(uint64(page.Header.Overflow.Value), 10), Span: bboltSpanPtr(page.Header.Overflow.Meta)},
	}

	blocks := []HexBlock{{
		ID:    "page-header",
		Kind:  blockPageHeader,
		Title: "Page Header",
		Span:  bboltSpanFromMeta(page.Header.Meta),
		Rows: []Field{
			{Label: "Page Header", Value: ""},
			{Label: "Offset", Value: bboltOffsetRange(page.Header.Meta)},
			{Label: "Size", Value: byteCount(page.Header.Meta.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Page id", Value: strconv.FormatUint(uint64(page.Header.ID.Value), 10), Span: bboltSpanPtr(page.Header.ID.Meta)},
			{Label: "Flags", Value: bboltPageFlagLabel(page.Header.Flags.Value), Span: bboltSpanPtr(page.Header.Flags.Meta)},
			{Label: "Count", Value: strconv.FormatUint(uint64(page.Header.Count.Value), 10), Span: bboltSpanPtr(page.Header.Count.Meta)},
			{Label: "Overflow", Value: strconv.FormatUint(uint64(page.Header.Overflow.Value), 10), Span: bboltSpanPtr(page.Header.Overflow.Meta)},
		},
	}}

	if page.MetaPayload != nil {
		rows = append(rows,
			Blank(),
			Section("META"),
			Field{Label: "Magic", Value: fmt.Sprintf("0x%x", page.MetaPayload.Magic.Value), Span: bboltSpanPtr(page.MetaPayload.Magic.Meta)},
			Field{Label: "Version", Value: strconv.FormatUint(uint64(page.MetaPayload.Version.Value), 10), Span: bboltSpanPtr(page.MetaPayload.Version.Meta)},
			Field{Label: "Page size", Value: strconv.FormatUint(uint64(page.MetaPayload.PageSize.Value), 10), Span: bboltSpanPtr(page.MetaPayload.PageSize.Meta)},
			Field{Label: "Flags", Value: strconv.FormatUint(uint64(page.MetaPayload.Flags.Value), 10), Span: bboltSpanPtr(page.MetaPayload.Flags.Meta)},
			Field{Label: "Root page", Value: strconv.FormatUint(uint64(page.MetaPayload.Root.Value), 10), Span: bboltSpanPtr(page.MetaPayload.Root.Meta)},
			Field{Label: "Sequence", Value: strconv.FormatUint(page.MetaPayload.Sequence.Value, 10), Span: bboltSpanPtr(page.MetaPayload.Sequence.Meta)},
			Field{Label: "Freelist page", Value: strconv.FormatUint(uint64(page.MetaPayload.FreeList.Value), 10), Span: bboltSpanPtr(page.MetaPayload.FreeList.Meta)},
			Field{Label: "High water mark", Value: strconv.FormatUint(uint64(page.MetaPayload.PageID.Value), 10), Span: bboltSpanPtr(page.MetaPayload.PageID.Meta)},
			Field{Label: "Transaction ID", Value: strconv.FormatUint(page.MetaPayload.TransactionID.Value, 10), Span: bboltSpanPtr(page.MetaPayload.TransactionID.Meta)},
			Field{Label: "Checksum", Value: fmt.Sprintf("0x%x", page.MetaPayload.CheckSum.Value), Span: bboltSpanPtr(page.MetaPayload.CheckSum.Meta)},
		)
		blocks = append(blocks, HexBlock{
			ID:    "meta-payload",
			Kind:  blockMetaPayload,
			Title: "Meta Payload",
			Span:  bboltSpanFromMeta(page.MetaPayload.Meta),
			Rows: []Field{
				{Label: "Meta Payload", Value: ""},
				{Label: "Offset", Value: bboltOffsetRange(page.MetaPayload.Meta)},
				{Label: "Size", Value: byteCount(page.MetaPayload.Meta.Size)},
				Blank(),
				Section("FIELDS"),
				{Label: "Magic", Value: fmt.Sprintf("0x%x", page.MetaPayload.Magic.Value), Span: bboltSpanPtr(page.MetaPayload.Magic.Meta)},
				{Label: "Version", Value: strconv.FormatUint(uint64(page.MetaPayload.Version.Value), 10), Span: bboltSpanPtr(page.MetaPayload.Version.Meta)},
				{Label: "Page size", Value: strconv.FormatUint(uint64(page.MetaPayload.PageSize.Value), 10), Span: bboltSpanPtr(page.MetaPayload.PageSize.Meta)},
				{Label: "Flags", Value: strconv.FormatUint(uint64(page.MetaPayload.Flags.Value), 10), Span: bboltSpanPtr(page.MetaPayload.Flags.Meta)},
				{Label: "Root page", Value: strconv.FormatUint(uint64(page.MetaPayload.Root.Value), 10), Span: bboltSpanPtr(page.MetaPayload.Root.Meta)},
				{Label: "Sequence", Value: strconv.FormatUint(page.MetaPayload.Sequence.Value, 10), Span: bboltSpanPtr(page.MetaPayload.Sequence.Meta)},
				{Label: "Freelist page", Value: strconv.FormatUint(uint64(page.MetaPayload.FreeList.Value), 10), Span: bboltSpanPtr(page.MetaPayload.FreeList.Meta)},
				{Label: "High water mark", Value: strconv.FormatUint(uint64(page.MetaPayload.PageID.Value), 10), Span: bboltSpanPtr(page.MetaPayload.PageID.Meta)},
				{Label: "Transaction ID", Value: strconv.FormatUint(page.MetaPayload.TransactionID.Value, 10), Span: bboltSpanPtr(page.MetaPayload.TransactionID.Meta)},
				{Label: "Checksum", Value: fmt.Sprintf("0x%x", page.MetaPayload.CheckSum.Value), Span: bboltSpanPtr(page.MetaPayload.CheckSum.Meta)},
			},
		})
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(pageID)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: blocks,
	}
}

func bboltSpanFromMeta(meta bbolt.Meta) ByteSpan {
	return ByteSpan{Start: meta.StartOffset, Size: meta.Size}
}

func bboltSpanPtr(meta bbolt.Meta) *ByteSpan {
	span := bboltSpanFromMeta(meta)
	return &span
}

func bboltOffsetRange(meta bbolt.Meta) string {
	return spanRange(bboltSpanFromMeta(meta))
}

func bboltPageKindLabel(flag bbolt.FlagType) string {
	switch flag {
	case bbolt.BranchPageFlag:
		return "branch"
	case bbolt.LeafPageFlag:
		return "leaf"
	case bbolt.MetaPageFlag:
		return "meta"
	case bbolt.FreelistPageFlag:
		return "freelist"
	default:
		return "unknown"
	}
}

func bboltPageFlagLabel(flag bbolt.FlagType) string {
	return fmt.Sprintf("%s (0x%x)", bboltPageKindLabel(flag), uint16(flag))
}
