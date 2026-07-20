package storage

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"

	"github.com/nikitazigman/badger/internal/bbolt"
)

const (
	blockMetaPayload       = "meta_payload"
	blockFreelistPayload   = "freelist_payload"
	blockFreelistID        = "freelist_id"
	blockBranchDescriptors = "branch_descriptors"
	blockBranchEntry       = "branch_entry"
	blockBranchDescriptor  = "branch_descriptor"
	blockLeafDescriptors   = "leaf_descriptors"
	blockLeafEntry         = "leaf_entry"
	blockLeafDescriptor    = "leaf_descriptor"
	blockLeafKey           = "leaf_key"
	blockLeafValue         = "leaf_value"
)

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
			{Label: "Freelist page", Value: bboltPageIDLabel(config.Freelist)},
			{Label: "High water mark", Value: strconv.FormatUint(uint64(config.HighWaterMark), 10)},
		},
	}
	btrees := []BTreeItem{rootBucket}

	bucketEntries, _, err := db.inspector.BucketEntries(config.Root)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(bucketEntries, func(a, b int) bool {
		return bytes.Compare(bucketEntries[a].Key.Data, bucketEntries[b].Key.Data) < 0
	})
	for _, entry := range bucketEntries {
		btrees = append(btrees, bboltBucketItem(entry))
	}

	overview := &DatabaseOverview{
		Path:              db.path,
		PageSizeBytes:     uint64(config.PageSize),
		PageCount:         uint64(config.HighWaterMark),
		FirstPageID:       0,
		DatabaseSizeBytes: uint64(config.PageSize) * uint64(config.HighWaterMark),
		HeaderRows:        bboltHeaderRows(config),
		BTrees:            btrees,
		PageSummaries:     bboltPageSummaries(db.inspector.PageSummaries()),
	}

	db.btrees = make(map[BTreeID]BTreeItem, len(btrees))
	for _, item := range btrees {
		db.btrees[item.ID] = item
	}
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
	walk, err := db.inspector.PagesForRoot(bbolt.PageID(item.RootPage.ID))
	if err != nil {
		return nil, err
	}
	pages := make([]PageRef, 0, len(walk.Pages))
	for _, page := range walk.Pages {
		pages = append(pages, PageRef{ID: uint64(page)})
	}
	return pages, nil
}

func bboltBucketItem(entry bbolt.BucketEntry) BTreeItem {
	name := bboltBucketName(entry.Key.Data)
	var root *PageRef
	if entry.Bucket.Root.Value != 0 {
		ref := PageRef{ID: uint64(entry.Bucket.Root.Value)}
		root = &ref
	}
	return BTreeItem{
		ID:       BTreeID("bucket:root/" + hex.EncodeToString(entry.Key.Data)),
		Kind:     BTreeBucket,
		Name:     name,
		RootPage: root,
		Rows: []Field{
			{Label: "Type", Value: "bucket"},
			{Label: "Name", Value: name},
			{Label: "Key", Value: bboltKeyLabel(entry.Key.Data), Span: bboltSpanPtr(entry.Key.Meta)},
			{Label: "Root page", Value: bboltBucketRootLabel(entry.Bucket.Root.Value), Span: bboltSpanPtr(entry.Bucket.Root.Meta)},
			{Label: "Sequence", Value: strconv.FormatUint(entry.Bucket.Sequence.Value, 10), Span: bboltSpanPtr(entry.Bucket.Sequence.Meta)},
			{Label: "Parent page", Value: strconv.FormatUint(uint64(entry.PageID), 10)},
			{Label: "Entry index", Value: strconv.Itoa(entry.ElementIndex)},
		},
	}
}

func bboltHeaderRows(config bbolt.BboltConfig) []Field {
	return []Field{
		{Label: "Page size", Value: strconv.FormatUint(uint64(config.PageSize), 10)},
		{Label: "Version", Value: strconv.FormatUint(uint64(config.Version), 10)},
		{Label: "Root page", Value: strconv.FormatUint(uint64(config.Root), 10)},
		{Label: "Freelist page", Value: bboltPageIDLabel(config.Freelist)},
		{Label: "High water mark", Value: strconv.FormatUint(uint64(config.HighWaterMark), 10)},
		{Label: "Transaction ID", Value: strconv.FormatUint(config.TransactionID, 10)},
	}
}

func bboltPageIDLabel(id bbolt.PageID) string {
	if id == bbolt.PgidNoFreelist {
		return "none"
	}
	return strconv.FormatUint(uint64(id), 10)
}

func bboltBucketRootLabel(id bbolt.PageID) string {
	if id == 0 {
		return "inline"
	}
	return strconv.FormatUint(uint64(id), 10)
}

func adaptBboltPage(page *bbolt.BTreePage) *PageInspection {
	pageSize := len(page.Raw)
	pageID := page.ID
	offset := uint64(pageID) * uint64(pageSize)
	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(pageID), 10)},
		{Label: "Type", Value: bboltClassificationLabel(page.Classification)},
		{Label: "Classification", Value: bboltClassificationLabel(page.Classification)},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", pageSize)},
		{Label: "File offset", Value: strconv.FormatUint(offset, 10)},
	}

	if page.Classification == bbolt.PageClassFree {
		rows = append(rows,
			Field{Label: "Freelist membership", Value: "yes"},
			Field{Label: "Note", Value: "free page; bytes may contain stale data"},
		)
	}
	if page.Classification == bbolt.PageClassContinuation && page.ContinuationOf != nil {
		rows = append(rows, Field{Label: "Continuation of", Value: strconv.FormatUint(uint64(*page.ContinuationOf), 10)})
	}

	blocks := []HexBlock{}
	if page.HasHeader {
		rows = append(rows,
			Blank(),
			Section("HEADER"),
			Field{Label: "Header page id", Value: strconv.FormatUint(uint64(page.Header.ID.Value), 10), Span: bboltSpanPtr(page.Header.ID.Meta)},
			Field{Label: "Flags", Value: bboltPageFlagLabel(page.Header.Flags.Value), Span: bboltSpanPtr(page.Header.Flags.Meta)},
			Field{Label: "Count", Value: strconv.FormatUint(uint64(page.Header.Count.Value), 10), Span: bboltSpanPtr(page.Header.Count.Meta)},
			Field{Label: "Overflow", Value: strconv.FormatUint(uint64(page.Header.Overflow.Value), 10), Span: bboltSpanPtr(page.Header.Overflow.Meta)},
		)
		blocks = append(blocks, HexBlock{
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
		})
	}

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
			Field{Label: "Freelist page", Value: bboltPageIDLabel(page.MetaPayload.FreeList.Value), Span: bboltSpanPtr(page.MetaPayload.FreeList.Meta)},
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
				{Label: "Freelist page", Value: bboltPageIDLabel(page.MetaPayload.FreeList.Value), Span: bboltSpanPtr(page.MetaPayload.FreeList.Meta)},
				{Label: "High water mark", Value: strconv.FormatUint(uint64(page.MetaPayload.PageID.Value), 10), Span: bboltSpanPtr(page.MetaPayload.PageID.Meta)},
				{Label: "Transaction ID", Value: strconv.FormatUint(page.MetaPayload.TransactionID.Value, 10), Span: bboltSpanPtr(page.MetaPayload.TransactionID.Meta)},
				{Label: "Checksum", Value: fmt.Sprintf("0x%x", page.MetaPayload.CheckSum.Value), Span: bboltSpanPtr(page.MetaPayload.CheckSum.Meta)},
			},
		})
	}
	if page.FreelistPayload != nil {
		rows = append(rows,
			Blank(),
			Section("FREELIST"),
			Field{Label: "Free page ids", Value: strconv.Itoa(len(page.FreelistPayload.IDs))},
		)
		if page.FreelistPayload.ActualCount != nil {
			rows = append(rows, Field{Label: "Actual count", Value: strconv.FormatUint(page.FreelistPayload.ActualCount.Value, 10), Span: bboltSpanPtr(page.FreelistPayload.ActualCount.Meta)})
		}
		for idx, field := range page.FreelistPayload.IDFields {
			rows = append(rows, Field{Label: fmt.Sprintf("Free page id %d", idx), Value: strconv.FormatUint(uint64(field.Value), 10), Span: bboltSpanPtr(field.Meta)})
		}

		children := make([]HexBlock, 0, len(page.FreelistPayload.IDFields))
		for idx, field := range page.FreelistPayload.IDFields {
			if field.Meta.EndOffset() > len(page.Raw) {
				continue
			}
			children = append(children, HexBlock{
				ID:    fmt.Sprintf("freelist-id-%d", idx),
				Kind:  blockFreelistID,
				Title: fmt.Sprintf("Freelist ID %d", idx),
				Span:  bboltSpanFromMeta(field.Meta),
				Rows: []Field{
					{Label: fmt.Sprintf("Freelist ID %d", idx), Value: ""},
					{Label: "Offset", Value: bboltOffsetRange(field.Meta)},
					{Label: "Size", Value: byteCount(field.Meta.Size)},
					Blank(),
					Section("FIELDS"),
					{Label: "Page id", Value: strconv.FormatUint(uint64(field.Value), 10), Span: bboltSpanPtr(field.Meta)},
				},
			})
		}
		block := HexBlock{
			ID:       "freelist-payload",
			Kind:     blockFreelistPayload,
			Title:    "Freelist Payload",
			Span:     bboltSpanFromMeta(page.FreelistPayload.Meta),
			Children: children,
			Rows: []Field{
				{Label: "Freelist Payload", Value: ""},
				{Label: "Offset", Value: bboltOffsetRange(page.FreelistPayload.Meta)},
				{Label: "Size", Value: byteCount(page.FreelistPayload.Meta.Size)},
				Blank(),
				Section("FIELDS"),
				{Label: "Free page ids", Value: strconv.Itoa(len(page.FreelistPayload.IDs))},
			},
		}
		if block.Span.End() > len(page.Raw) {
			block.Span.Size = max(0, len(page.Raw)-block.Span.Start)
		}
		if page.FreelistPayload.ActualCount != nil {
			block.Rows = append(block.Rows, Field{Label: "Actual count", Value: strconv.FormatUint(page.FreelistPayload.ActualCount.Value, 10), Span: bboltSpanPtr(page.FreelistPayload.ActualCount.Meta)})
		}
		blocks = append(blocks, block)
	}
	if page.BranchPayload != nil {
		branchRows, branchBlocks := adaptBboltBranchPage(page)
		rows = append(rows, branchRows...)
		blocks = append(blocks, branchBlocks...)
	}
	if page.LeafPayload != nil {
		leafRows, leafBlocks := adaptBboltLeafPage(page)
		rows = append(rows, leafRows...)
		blocks = append(blocks, leafBlocks...)
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(pageID)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: blocks,
	}
}

func adaptBboltBranchPage(page *bbolt.BTreePage) ([]Field, []HexBlock) {
	descriptorChildren := make([]HexBlock, 0, len(page.BranchPayload.BranchElements))
	entryBlocks := make([]HexBlock, 0, len(page.BranchPayload.KeyValue))
	rows := []Field{
		Blank(),
		Section("BRANCH"),
		{Label: "Branch entries", Value: strconv.Itoa(len(page.BranchPayload.BranchElements))},
	}

	for idx, element := range page.BranchPayload.BranchElements {
		if idx >= len(page.BranchPayload.KeyValue) {
			break
		}
		kv := page.BranchPayload.KeyValue[idx]
		rows = append(rows, Field{
			Label: fmt.Sprintf("Entry %d", idx),
			Value: fmt.Sprintf("key=%s child=%d", bboltKeyLabel(kv.Key.Data), element.PageID.Value),
			Span:  bboltSpanPtr(element.Meta),
		})

		descriptorChildren = append(descriptorChildren, bboltBranchDescriptorBlock(idx, element))
		entryBlocks = append(entryBlocks, bboltBranchEntryBlock(idx, element, kv))
	}

	blocks := []HexBlock{{
		ID:       "branch-descriptors",
		Kind:     blockBranchDescriptors,
		Title:    "Branch Descriptors",
		Span:     bboltBranchDescriptorsSpan(page.BranchPayload.BranchElements),
		Children: descriptorChildren,
		Rows: []Field{
			{Label: "Branch Descriptors", Value: ""},
			{Label: "Offset", Value: spanRange(bboltBranchDescriptorsSpan(page.BranchPayload.BranchElements))},
			{Label: "Size", Value: byteCount(bboltBranchDescriptorsSpan(page.BranchPayload.BranchElements).Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Descriptors", Value: strconv.Itoa(len(page.BranchPayload.BranchElements))},
		},
	}}
	blocks = append(blocks, entryBlocks...)
	return rows, blocks
}

func bboltBranchEntryBlock(idx int, element bbolt.BranchElement, kv bbolt.KeyValue) HexBlock {
	return HexBlock{
		ID:    fmt.Sprintf("branch-entry-%d", idx),
		Kind:  blockBranchEntry,
		Title: fmt.Sprintf("Branch Entry %d", idx),
		Span:  bboltSpanFromMeta(kv.Meta),
		Rows: []Field{
			{Label: fmt.Sprintf("Branch Entry %d", idx), Value: ""},
			{Label: "Offset", Value: bboltOffsetRange(kv.Meta)},
			{Label: "Size", Value: byteCount(kv.Meta.Size)},
			Blank(),
			Section("DESCRIPTOR"),
			{Label: "Position", Value: strconv.FormatUint(uint64(element.Pos.Value), 10), Span: bboltSpanPtr(element.Pos.Meta)},
			{Label: "Key size", Value: byteCount(int(element.KeySize.Value)), Span: bboltSpanPtr(element.KeySize.Meta)},
			{Label: "Child page", Value: strconv.FormatUint(uint64(element.PageID.Value), 10), Span: bboltSpanPtr(element.PageID.Meta)},
			Blank(),
			Section("PAYLOAD"),
			{Label: "Key", Value: bboltKeyLabel(kv.Key.Data), Span: bboltSpanPtr(kv.Key.Meta)},
		},
	}
}

func bboltBranchDescriptorBlock(idx int, element bbolt.BranchElement) HexBlock {
	return HexBlock{
		ID:    fmt.Sprintf("branch-entry-%d-descriptor", idx),
		Kind:  blockBranchDescriptor,
		Title: fmt.Sprintf("Branch Entry %d Descriptor", idx),
		Span:  bboltSpanFromMeta(element.Meta),
		Rows: []Field{
			{Label: fmt.Sprintf("Branch Entry %d Descriptor", idx), Value: ""},
			{Label: "Offset", Value: bboltOffsetRange(element.Meta)},
			{Label: "Size", Value: byteCount(element.Meta.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Position", Value: strconv.FormatUint(uint64(element.Pos.Value), 10), Span: bboltSpanPtr(element.Pos.Meta)},
			{Label: "Key size", Value: byteCount(int(element.KeySize.Value)), Span: bboltSpanPtr(element.KeySize.Meta)},
			{Label: "Child page", Value: strconv.FormatUint(uint64(element.PageID.Value), 10), Span: bboltSpanPtr(element.PageID.Meta)},
		},
	}
}

func bboltBranchDescriptorsSpan(elements []bbolt.BranchElement) ByteSpan {
	if len(elements) == 0 {
		return ByteSpan{Start: 16, Size: 0}
	}
	first := elements[0].Meta
	last := elements[len(elements)-1].Meta
	return ByteSpan{Start: first.StartOffset, Size: last.EndOffset() - first.StartOffset}
}

func adaptBboltLeafPage(page *bbolt.BTreePage) ([]Field, []HexBlock) {
	descriptorChildren := make([]HexBlock, 0, len(page.LeafPayload.LeafElements))
	entryBlocks := make([]HexBlock, 0, len(page.LeafPayload.KeyValue))
	bucketEntries := 0
	rows := []Field{
		Blank(),
		Section("LEAF"),
		{Label: "Leaf entries", Value: strconv.Itoa(len(page.LeafPayload.LeafElements))},
	}

	for idx, element := range page.LeafPayload.LeafElements {
		if idx >= len(page.LeafPayload.KeyValue) {
			break
		}
		kv := page.LeafPayload.KeyValue[idx]
		entryType := "key/value"
		if element.Flags.Value == bbolt.BucketLeafFlag {
			entryType = "bucket"
			bucketEntries++
		}

		rows = append(rows, Field{
			Label: fmt.Sprintf("Entry %d", idx),
			Value: fmt.Sprintf("%s key=%s value=%s", entryType, bboltKeyLabel(kv.Key.Data), byteCount(len(kv.Value.Data))),
			Span:  bboltSpanPtr(element.Meta),
		})

		descriptorChildren = append(descriptorChildren, bboltLeafDescriptorBlock(idx, element))
		entryBlocks = append(entryBlocks, bboltLeafEntryBlock(idx, element, kv, entryType))
	}
	rows = append(rows, Field{Label: "Bucket entries", Value: strconv.Itoa(bucketEntries)})

	blocks := []HexBlock{{
		ID:       "leaf-descriptors",
		Kind:     blockLeafDescriptors,
		Title:    "Leaf Descriptors",
		Span:     bboltLeafDescriptorsSpan(page.LeafPayload.LeafElements),
		Children: descriptorChildren,
		Rows: []Field{
			{Label: "Leaf Descriptors", Value: ""},
			{Label: "Offset", Value: spanRange(bboltLeafDescriptorsSpan(page.LeafPayload.LeafElements))},
			{Label: "Size", Value: byteCount(bboltLeafDescriptorsSpan(page.LeafPayload.LeafElements).Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Descriptors", Value: strconv.Itoa(len(page.LeafPayload.LeafElements))},
		},
	}}
	blocks = append(blocks, entryBlocks...)
	return rows, blocks
}

func bboltLeafEntryBlock(idx int, element bbolt.LeafElement, kv bbolt.KeyValue, entryType string) HexBlock {
	children := []HexBlock{
		bboltLeafPayloadBlock(idx, "key", blockLeafKey, kv.Key.Meta, len(kv.Key.Data), bboltKeyLabel(kv.Key.Data)),
		bboltLeafPayloadBlock(idx, "value", blockLeafValue, kv.Value.Meta, len(kv.Value.Data), byteCount(len(kv.Value.Data))),
	}

	rows := []Field{
		{Label: fmt.Sprintf("Leaf Entry %d", idx), Value: ""},
		{Label: "Type", Value: entryType},
		{Label: "Offset", Value: bboltOffsetRange(kv.Meta)},
		{Label: "Size", Value: byteCount(kv.Meta.Size)},
		Blank(),
		Section("DESCRIPTOR"),
		{Label: "Flags", Value: bboltLeafFlagLabel(element.Flags.Value), Span: bboltSpanPtr(element.Flags.Meta)},
		{Label: "Position", Value: strconv.FormatUint(uint64(element.Pos.Value), 10), Span: bboltSpanPtr(element.Pos.Meta)},
		{Label: "Key size", Value: byteCount(int(element.KeySize.Value)), Span: bboltSpanPtr(element.KeySize.Meta)},
		{Label: "Value size", Value: byteCount(int(element.ValueSize.Value)), Span: bboltSpanPtr(element.ValueSize.Meta)},
		Blank(),
		Section("PAYLOAD"),
		{Label: "Key", Value: bboltKeyLabel(kv.Key.Data), Span: bboltSpanPtr(kv.Key.Meta)},
		{Label: "Value", Value: byteCount(len(kv.Value.Data)), Span: bboltSpanPtr(kv.Value.Meta)},
	}

	return HexBlock{
		ID:       fmt.Sprintf("leaf-entry-%d", idx),
		Kind:     blockLeafEntry,
		Title:    fmt.Sprintf("Leaf Entry %d", idx),
		Span:     bboltSpanFromMeta(kv.Meta),
		Rows:     rows,
		Children: children,
	}
}

func bboltLeafDescriptorBlock(idx int, element bbolt.LeafElement) HexBlock {
	return HexBlock{
		ID:    fmt.Sprintf("leaf-entry-%d-descriptor", idx),
		Kind:  blockLeafDescriptor,
		Title: fmt.Sprintf("Leaf Entry %d Descriptor", idx),
		Span:  bboltSpanFromMeta(element.Meta),
		Rows: []Field{
			{Label: fmt.Sprintf("Leaf Entry %d Descriptor", idx), Value: ""},
			{Label: "Offset", Value: bboltOffsetRange(element.Meta)},
			{Label: "Size", Value: byteCount(element.Meta.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Flags", Value: bboltLeafFlagLabel(element.Flags.Value), Span: bboltSpanPtr(element.Flags.Meta)},
			{Label: "Position", Value: strconv.FormatUint(uint64(element.Pos.Value), 10), Span: bboltSpanPtr(element.Pos.Meta)},
			{Label: "Key size", Value: byteCount(int(element.KeySize.Value)), Span: bboltSpanPtr(element.KeySize.Meta)},
			{Label: "Value size", Value: byteCount(int(element.ValueSize.Value)), Span: bboltSpanPtr(element.ValueSize.Meta)},
		},
	}
}

func bboltLeafPayloadBlock(idx int, label string, kind string, meta bbolt.Meta, size int, value string) HexBlock {
	titleLabel := "Key"
	if label == "value" {
		titleLabel = "Value"
	}
	return HexBlock{
		ID:    fmt.Sprintf("leaf-entry-%d-%s", idx, label),
		Kind:  kind,
		Title: fmt.Sprintf("Leaf Entry %d %s", idx, titleLabel),
		Span:  bboltSpanFromMeta(meta),
		Rows: []Field{
			{Label: fmt.Sprintf("Leaf Entry %d %s", idx, titleLabel), Value: ""},
			{Label: "Offset", Value: bboltOffsetRange(meta)},
			{Label: "Size", Value: byteCount(size)},
			{Label: titleLabel, Value: value},
		},
	}
}

func bboltLeafDescriptorsSpan(elements []bbolt.LeafElement) ByteSpan {
	if len(elements) == 0 {
		return ByteSpan{Start: 16, Size: 0}
	}
	first := elements[0].Meta
	last := elements[len(elements)-1].Meta
	return ByteSpan{Start: first.StartOffset, Size: last.EndOffset() - first.StartOffset}
}

func bboltKeyLabel(data []byte) string {
	out := make([]byte, 0, len(data)+2)
	out = append(out, '"')
	for _, b := range data {
		switch b {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if b >= 0x20 && b <= 0x7e {
				out = append(out, b)
			} else {
				out = append(out, fmt.Sprintf("\\x%02x", b)...)
			}
		}
	}
	out = append(out, '"')
	return string(out)
}

func bboltBucketName(data []byte) string {
	if len(data) == 0 {
		return `""`
	}
	for _, b := range data {
		if b < 0x20 || b > 0x7e || b == '"' || b == '\\' {
			return bboltKeyLabel(data)
		}
	}
	return string(data)
}

func bboltPageSummaries(summaries []bbolt.PageSummary) []PageSummary {
	out := make([]PageSummary, 0, len(summaries))
	for _, summary := range summaries {
		classification := bboltStorageClassification(summary.Classification)
		out = append(out, PageSummary{
			Ref:            PageRef{ID: uint64(summary.ID)},
			Classification: classification,
			Label:          string(classification),
		})
	}
	return out
}

func bboltStorageClassification(classification bbolt.PageClassification) PageClassification {
	switch classification {
	case bbolt.PageClassMeta:
		return PageClassMeta
	case bbolt.PageClassBranch:
		return PageClassBranch
	case bbolt.PageClassLeaf:
		return PageClassLeaf
	case bbolt.PageClassFreelist:
		return PageClassFreelist
	case bbolt.PageClassFree:
		return PageClassFree
	case bbolt.PageClassContinuation:
		return PageClassContinuation
	case bbolt.PageClassTruncated:
		return PageClassTruncated
	default:
		return PageClassUnknown
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

func bboltLeafFlagLabel(flag bbolt.LeafFlagType) string {
	switch flag {
	case bbolt.OrdinaryKeyValueFlag:
		return "key/value (0x0)"
	case bbolt.BucketLeafFlag:
		return "bucket (0x1)"
	default:
		return fmt.Sprintf("unknown (0x%x)", uint32(flag))
	}
}

func bboltClassificationLabel(classification bbolt.PageClassification) string {
	if classification == "" {
		return string(PageClassUnknown)
	}
	return string(bboltStorageClassification(classification))
}
