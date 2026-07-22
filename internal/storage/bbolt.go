package storage

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nikitazigman/badger/internal/bbolt"
)

const (
	blockMetaPayload               = "meta_payload"
	blockFreelistPayload           = "freelist_payload"
	blockFreelistID                = "freelist_id"
	blockBranchDescriptors         = "branch_descriptors"
	blockBranchEntry               = "branch_entry"
	blockBranchDescriptor          = "branch_descriptor"
	blockLeafDescriptors           = "leaf_descriptors"
	blockLeafEntry                 = "leaf_entry"
	blockLeafDescriptor            = "leaf_descriptor"
	blockLeafKey                   = "leaf_key"
	blockLeafValue                 = "leaf_value"
	blockInlinePageHeader          = "inline_page_header"
	blockInlineLeafDescriptors     = "inline_leaf_descriptors"
	blockInlineLeafEntry           = "inline_leaf_entry"
	blockInlineLeafDescriptor      = "inline_leaf_descriptor"
	blockInlineLeafKey             = "inline_leaf_key"
	blockInlineLeafValue           = "inline_leaf_value"
	blockDescriptorFlags           = "descriptor_flags"
	blockDescriptorPosition        = "descriptor_position"
	blockDescriptorKeySize         = "descriptor_key_size"
	blockDescriptorValueSize       = "descriptor_value_size"
	blockDescriptorChildPage       = "descriptor_child_page"
	blockBucketRootPage            = "bucket_root_page"
	blockBucketSequence            = "bucket_sequence"
	blockInlineDescriptorFlags     = "inline_descriptor_flags"
	blockInlineDescriptorPosition  = "inline_descriptor_position"
	blockInlineDescriptorKeySize   = "inline_descriptor_key_size"
	blockInlineDescriptorValueSize = "inline_descriptor_value_size"
	blockInlineBucketRootPage      = "inline_bucket_root_page"
	blockInlineBucketSequence      = "inline_bucket_sequence"
	blockBboltOverflowExtent       = "bbolt_overflow_extent"
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

	btrees, err := db.appendBboltBucketItems(btrees, rootBucket.ID, "", config.Root, 0)
	if err != nil {
		return nil, err
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
		if item.Kind == BTreeInlineBucket {
			page, err := bboltInlineBucketParentPage(item)
			if err != nil {
				return nil, err
			}
			return []PageRef{{ID: page}}, nil
		}
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

func (db *bboltDatabase) appendBboltBucketItems(items []BTreeItem, parentID BTreeID, parentPath string, root bbolt.PageID, depth int) ([]BTreeItem, error) {
	bucketEntries, _, err := db.inspector.BucketEntries(root)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(bucketEntries, func(a, b int) bool {
		return bytes.Compare(bucketEntries[a].Key.Data, bucketEntries[b].Key.Data) < 0
	})
	for _, entry := range bucketEntries {
		item := bboltBucketItem(parentID, parentPath, depth, entry)
		items = append(items, item)
		if entry.Bucket.Root.Value == 0 {
			continue
		}
		childPath := bboltBucketPath(parentPath, bboltBucketName(entry.Key.Data))
		items, err = db.appendBboltBucketItems(items, item.ID, childPath, entry.Bucket.Root.Value, depth+1)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func bboltBucketItem(parentID BTreeID, parentPath string, depth int, entry bbolt.BucketEntry) BTreeItem {
	name := bboltBucketName(entry.Key.Data)
	displayName := strings.Repeat(" ", depth) + name
	path := bboltBucketPath(parentPath, name)
	var root *PageRef
	kind := BTreeBucket
	bucketType := "bucket"
	if entry.Bucket.Root.Value != 0 {
		ref := PageRef{ID: uint64(entry.Bucket.Root.Value)}
		root = &ref
	} else {
		kind = BTreeInlineBucket
		bucketType = "inline bucket"
	}
	rows := []Field{
		{Label: "Type", Value: bucketType},
		{Label: "Name", Value: name},
		{Label: "Path", Value: path},
		{Label: "Key", Value: bboltKeyLabel(entry.Key.Data), Span: bboltSpanPtr(entry.Key.Meta)},
		{Label: "Root page", Value: bboltBucketRootLabel(entry.Bucket.Root.Value), Span: bboltSpanPtr(entry.Bucket.Root.Meta)},
		{Label: "Sequence", Value: strconv.FormatUint(entry.Bucket.Sequence.Value, 10), Span: bboltSpanPtr(entry.Bucket.Sequence.Meta)},
		{Label: "Parent page", Value: strconv.FormatUint(uint64(entry.PageID), 10)},
		{Label: "Entry index", Value: strconv.Itoa(entry.ElementIndex)},
	}
	if entry.Bucket.Root.Value == 0 {
		rows = append(rows, Field{Label: "Storage", Value: "embedded in parent leaf value"})
		if entry.Inline != nil {
			rows = append(rows,
				Field{Label: "Inline page offset", Value: bboltOffsetRange(entry.Inline.Meta), Span: bboltSpanPtr(entry.Inline.Meta)},
				Field{Label: "Inline page entries", Value: strconv.Itoa(len(entry.Inline.LeafPayload.LeafElements))},
			)
		}
	}
	return BTreeItem{
		ID:       BTreeID(string(parentID) + "/" + hex.EncodeToString(entry.Key.Data)),
		Kind:     kind,
		Name:     displayName,
		RootPage: root,
		Rows:     rows,
	}
}

func bboltInlineBucketParentPage(item BTreeItem) (uint64, error) {
	for _, row := range item.Rows {
		if row.Label != "Parent page" {
			continue
		}
		page, err := strconv.ParseUint(row.Value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("inline bucket %q parent page %q is invalid: %w", item.ID, row.Value, err)
		}
		return page, nil
	}
	return 0, fmt.Errorf("inline bucket %q missing parent page metadata", item.ID)
}

func bboltBucketPath(parent string, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
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
	blocks := []HexBlock{}

	if page.Classification == bbolt.PageClassFree {
		rows = append(rows,
			Field{Label: "Freelist membership", Value: "yes"},
			Field{Label: "Note", Value: "free page; bytes may contain stale data"},
		)
	}
	if page.OverflowExtent != nil {
		rows = append(rows, bboltOverflowExtentRows(*page.OverflowExtent)...)
		if page.OverflowExtent.PartIndex > 1 {
			blocks = append(blocks, bboltOverflowExtentBlock(*page.OverflowExtent))
		}
	} else if page.Classification == bbolt.PageClassContinuation && page.ContinuationOf != nil {
		rows = append(rows, Field{Label: "Continuation of", Value: strconv.FormatUint(uint64(*page.ContinuationOf), 10)})
	}

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

func bboltOverflowExtentRows(extent bbolt.OverflowExtent) []Field {
	rows := []Field{
		Blank(),
		Section("OVERFLOW"),
		{Label: "Overflow role", Value: bboltOverflowRole(extent)},
		{Label: "Parent page", Value: strconv.FormatUint(uint64(extent.Parent), 10)},
		{Label: "Overflow part", Value: fmt.Sprintf("%d of %d", extent.PartIndex, extent.PartCount), Span: bboltSpanPtr(extent.Span)},
		{Label: "Logical extent", Value: fmt.Sprintf("pages %d-%d", extent.Start, extent.End)},
	}
	if extent.PartIndex > 1 {
		rows = append(rows, Field{Label: "Continuation of", Value: strconv.FormatUint(uint64(extent.Parent), 10)})
	}
	return rows
}

func bboltOverflowExtentBlock(extent bbolt.OverflowExtent) HexBlock {
	return HexBlock{
		ID:    "bbolt-overflow-extent",
		Kind:  blockBboltOverflowExtent,
		Title: "Overflow Extent",
		Span:  bboltSpanFromMeta(extent.Span),
		Rows: []Field{
			{Label: "Overflow Extent", Value: ""},
			{Label: "Role", Value: bboltOverflowRole(extent)},
			{Label: "Parent page", Value: strconv.FormatUint(uint64(extent.Parent), 10)},
			{Label: "Selected page", Value: strconv.FormatUint(uint64(extent.Page), 10)},
			{Label: "Overflow part", Value: fmt.Sprintf("%d of %d", extent.PartIndex, extent.PartCount)},
			{Label: "Logical extent", Value: fmt.Sprintf("pages %d-%d", extent.Start, extent.End)},
			{Label: "Selected physical span", Value: bboltOffsetRange(extent.Span), Span: bboltSpanPtr(extent.Span)},
		},
	}
}

func bboltOverflowRole(extent bbolt.OverflowExtent) string {
	if extent.PartIndex == 1 {
		return "overflow parent"
	}
	return "overflow continuation"
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
		ID:       fmt.Sprintf("branch-entry-%d-descriptor", idx),
		Kind:     blockBranchDescriptor,
		Title:    fmt.Sprintf("Branch Entry %d Descriptor", idx),
		Span:     bboltSpanFromMeta(element.Meta),
		Children: bboltBranchDescriptorFieldBlocks(idx, element),
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

func bboltBranchDescriptorFieldBlocks(idx int, element bbolt.BranchElement) []HexBlock {
	return []HexBlock{
		bboltFieldBlock(
			fmt.Sprintf("branch-entry-%d-descriptor-position", idx),
			blockDescriptorPosition,
			"Position",
			element.Pos.Meta,
			strconv.FormatUint(uint64(element.Pos.Value), 10),
		),
		bboltFieldBlock(
			fmt.Sprintf("branch-entry-%d-descriptor-key-size", idx),
			blockDescriptorKeySize,
			"Key size",
			element.KeySize.Meta,
			byteCount(int(element.KeySize.Value)),
		),
		bboltFieldBlock(
			fmt.Sprintf("branch-entry-%d-descriptor-child-page", idx),
			blockDescriptorChildPage,
			"Child page",
			element.PageID.Meta,
			strconv.FormatUint(uint64(element.PageID.Value), 10),
		),
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
	bucketIndex := 0
	inlineIndex := 0
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
		var bucket *bbolt.NestedBucket
		var inline *bbolt.InlineBucket
		if element.Flags.Value == bbolt.BucketLeafFlag && bucketIndex < len(page.LeafPayload.NestedBucket) {
			bucket = &page.LeafPayload.NestedBucket[bucketIndex]
			if bucket.Root.Value == 0 && inlineIndex < len(page.LeafPayload.InlineBucket) {
				inline = &page.LeafPayload.InlineBucket[inlineIndex]
				inlineIndex++
			}
			bucketIndex++
		}

		rows = append(rows, Field{
			Label: fmt.Sprintf("Entry %d", idx),
			Value: fmt.Sprintf("%s key=%s value=%s", entryType, bboltKeyLabel(kv.Key.Data), byteCount(len(kv.Value.Data))),
			Span:  bboltSpanPtr(element.Meta),
		})

		descriptorChildren = append(descriptorChildren, bboltLeafDescriptorBlock(idx, element))
		entryBlocks = append(entryBlocks, bboltLeafEntryBlock(idx, element, kv, entryType, bucket, inline, false))
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

func bboltLeafEntryBlock(idx int, element bbolt.LeafElement, kv bbolt.KeyValue, entryType string, bucket *bbolt.NestedBucket, inline *bbolt.InlineBucket, insideInlinePage bool) HexBlock {
	leafEntryKind := blockLeafEntry
	leafKeyKind := blockLeafKey
	if insideInlinePage {
		leafEntryKind = blockInlineLeafEntry
		leafKeyKind = blockInlineLeafKey
	}
	children := []HexBlock{
		bboltLeafPayloadBlock(idx, "key", leafKeyKind, kv.Key.Meta, len(kv.Key.Data), bboltKeyLabel(kv.Key.Data)),
		bboltLeafValueBlock(idx, kv.Value, bucket, inline, insideInlinePage),
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
	if bucket != nil {
		rows = append(rows,
			Blank(),
			Section("BUCKET"),
			Field{Label: "Root page", Value: bboltBucketRootLabel(bucket.Root.Value), Span: bboltSpanPtr(bucket.Root.Meta)},
			Field{Label: "Sequence", Value: strconv.FormatUint(bucket.Sequence.Value, 10), Span: bboltSpanPtr(bucket.Sequence.Meta)},
		)
	}
	if inline != nil {
		rows = append(rows, Field{Label: "Storage", Value: "embedded in parent leaf value"})
	}

	return HexBlock{
		ID:       fmt.Sprintf("leaf-entry-%d", idx),
		Kind:     leafEntryKind,
		Title:    fmt.Sprintf("Leaf Entry %d", idx),
		Span:     bboltSpanFromMeta(kv.Meta),
		Rows:     rows,
		Children: children,
	}
}

func bboltLeafValueBlock(idx int, value bbolt.Payload, bucket *bbolt.NestedBucket, inline *bbolt.InlineBucket, insideInlinePage bool) HexBlock {
	leafValueKind := blockLeafValue
	if insideInlinePage {
		leafValueKind = blockInlineLeafValue
	}
	rows := []Field{
		{Label: fmt.Sprintf("Leaf Entry %d Value", idx), Value: ""},
		{Label: "Offset", Value: bboltOffsetRange(value.Meta)},
		{Label: "Size", Value: byteCount(len(value.Data))},
	}
	children := []HexBlock{}
	if bucket == nil {
		rows = append(rows, Field{Label: "Value", Value: byteCount(len(value.Data))})
	} else {
		valueType := "bucket value"
		format := "InBucket header"
		if inline != nil {
			valueType = "inline bucket value"
			format = "InBucket header + embedded leaf page"
		}
		rows = append(rows,
			Blank(),
			Section("BUCKET"),
			Field{Label: "Type", Value: valueType},
			Field{Label: "Format", Value: format},
			Field{Label: "Header offset", Value: bboltOffsetRange(bucket.Meta), Span: bboltSpanPtr(bucket.Meta)},
			Field{Label: "Root page", Value: bboltBucketRootLabel(bucket.Root.Value), Span: bboltSpanPtr(bucket.Root.Meta)},
			Field{Label: "Sequence", Value: strconv.FormatUint(bucket.Sequence.Value, 10), Span: bboltSpanPtr(bucket.Sequence.Meta)},
		)
		children = append(children, bboltBucketHeaderFieldBlocks(idx, *bucket, inline != nil || insideInlinePage)...)
	}
	if inline != nil {
		rows = append(rows, Field{Label: "Storage", Value: "embedded in parent leaf value"})
		children = append(children, bboltInlineBucketPageChildren(idx, *inline)...)
	}

	return HexBlock{
		ID:       fmt.Sprintf("leaf-entry-%d-value", idx),
		Kind:     leafValueKind,
		Title:    fmt.Sprintf("Leaf Entry %d Value", idx),
		Span:     bboltSpanFromMeta(value.Meta),
		Rows:     rows,
		Children: children,
	}
}

func bboltBucketHeaderFieldBlocks(idx int, bucket bbolt.NestedBucket, inlineBucket bool) []HexBlock {
	rootKind := blockBucketRootPage
	sequenceKind := blockBucketSequence
	if inlineBucket {
		rootKind = blockInlineBucketRootPage
		sequenceKind = blockInlineBucketSequence
	}
	return []HexBlock{
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-bucket-root-page", idx),
			rootKind,
			"Root page",
			bucket.Root.Meta,
			bboltBucketRootLabel(bucket.Root.Value),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-bucket-sequence", idx),
			sequenceKind,
			"Sequence",
			bucket.Sequence.Meta,
			strconv.FormatUint(bucket.Sequence.Value, 10),
		),
	}
}

func bboltInlineBucketPageChildren(idx int, inline bbolt.InlineBucket) []HexBlock {
	children := []HexBlock{bboltInlinePageHeaderBlock(idx, inline.Header)}
	children = append(children, bboltInlineLeafDescriptorListBlock(idx, inline.LeafPayload))

	bucketIndex := 0
	inlineIndex := 0
	for childIdx, element := range inline.LeafPayload.LeafElements {
		if childIdx >= len(inline.LeafPayload.KeyValue) {
			break
		}
		kv := inline.LeafPayload.KeyValue[childIdx]
		entryType := "key/value"
		var bucket *bbolt.NestedBucket
		var childInline *bbolt.InlineBucket
		if element.Flags.Value == bbolt.BucketLeafFlag && bucketIndex < len(inline.LeafPayload.NestedBucket) {
			entryType = "bucket"
			bucket = &inline.LeafPayload.NestedBucket[bucketIndex]
			if bucket.Root.Value == 0 && inlineIndex < len(inline.LeafPayload.InlineBucket) {
				childInline = &inline.LeafPayload.InlineBucket[inlineIndex]
				inlineIndex++
			}
			bucketIndex++
		}
		children = append(children, bboltLeafEntryBlock(childIdx, element, kv, entryType, bucket, childInline, true))
	}
	return children
}

func bboltInlinePageHeaderBlock(idx int, header bbolt.PageHeader) HexBlock {
	return HexBlock{
		ID:    fmt.Sprintf("leaf-entry-%d-inline-page-header", idx),
		Kind:  blockInlinePageHeader,
		Title: fmt.Sprintf("Leaf Entry %d Inline Page Header", idx),
		Span:  bboltSpanFromMeta(header.Meta),
		Rows: []Field{
			{Label: fmt.Sprintf("Leaf Entry %d Inline Page Header", idx), Value: ""},
			{Label: "Offset", Value: bboltOffsetRange(header.Meta)},
			{Label: "Size", Value: byteCount(header.Meta.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Page id", Value: strconv.FormatUint(uint64(header.ID.Value), 10), Span: bboltSpanPtr(header.ID.Meta)},
			{Label: "Flags", Value: bboltPageFlagLabel(header.Flags.Value), Span: bboltSpanPtr(header.Flags.Meta)},
			{Label: "Count", Value: strconv.FormatUint(uint64(header.Count.Value), 10), Span: bboltSpanPtr(header.Count.Meta)},
			{Label: "Overflow", Value: strconv.FormatUint(uint64(header.Overflow.Value), 10), Span: bboltSpanPtr(header.Overflow.Meta)},
		},
	}
}

func bboltInlineLeafDescriptorListBlock(idx int, payload bbolt.LeafPayload) HexBlock {
	children := make([]HexBlock, 0, len(payload.LeafElements))
	for childIdx, element := range payload.LeafElements {
		children = append(children, bboltInlineLeafDescriptorBlock(childIdx, element))
	}
	span := bboltLeafDescriptorsSpan(payload.LeafElements)
	if len(payload.LeafElements) == 0 {
		span = ByteSpan{Start: payload.Meta.StartOffset, Size: 0}
	}
	return HexBlock{
		ID:       fmt.Sprintf("leaf-entry-%d-inline-leaf-descriptors", idx),
		Kind:     blockInlineLeafDescriptors,
		Title:    fmt.Sprintf("Leaf Entry %d Inline Leaf Descriptors", idx),
		Span:     span,
		Children: children,
		Rows: []Field{
			{Label: fmt.Sprintf("Leaf Entry %d Inline Leaf Descriptors", idx), Value: ""},
			{Label: "Offset", Value: spanRange(span)},
			{Label: "Size", Value: byteCount(span.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Descriptors", Value: strconv.Itoa(len(payload.LeafElements))},
		},
	}
}

func bboltLeafDescriptorBlock(idx int, element bbolt.LeafElement) HexBlock {
	return HexBlock{
		ID:       fmt.Sprintf("leaf-entry-%d-descriptor", idx),
		Kind:     blockLeafDescriptor,
		Title:    fmt.Sprintf("Leaf Entry %d Descriptor", idx),
		Span:     bboltSpanFromMeta(element.Meta),
		Children: bboltLeafDescriptorFieldBlocks(idx, element),
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

func bboltInlineLeafDescriptorBlock(idx int, element bbolt.LeafElement) HexBlock {
	return HexBlock{
		ID:       fmt.Sprintf("leaf-entry-%d-inline-descriptor", idx),
		Kind:     blockInlineLeafDescriptor,
		Title:    fmt.Sprintf("Leaf Entry %d Inline Descriptor", idx),
		Span:     bboltSpanFromMeta(element.Meta),
		Children: bboltInlineLeafDescriptorFieldBlocks(idx, element),
		Rows: []Field{
			{Label: fmt.Sprintf("Leaf Entry %d Inline Descriptor", idx), Value: ""},
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

func bboltLeafDescriptorFieldBlocks(idx int, element bbolt.LeafElement) []HexBlock {
	return []HexBlock{
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-descriptor-flags", idx),
			blockDescriptorFlags,
			"Flags",
			element.Flags.Meta,
			bboltLeafFlagLabel(element.Flags.Value),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-descriptor-position", idx),
			blockDescriptorPosition,
			"Position",
			element.Pos.Meta,
			strconv.FormatUint(uint64(element.Pos.Value), 10),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-descriptor-key-size", idx),
			blockDescriptorKeySize,
			"Key size",
			element.KeySize.Meta,
			byteCount(int(element.KeySize.Value)),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-descriptor-value-size", idx),
			blockDescriptorValueSize,
			"Value size",
			element.ValueSize.Meta,
			byteCount(int(element.ValueSize.Value)),
		),
	}
}

func bboltInlineLeafDescriptorFieldBlocks(idx int, element bbolt.LeafElement) []HexBlock {
	return []HexBlock{
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-inline-descriptor-flags", idx),
			blockInlineDescriptorFlags,
			"Flags",
			element.Flags.Meta,
			bboltLeafFlagLabel(element.Flags.Value),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-inline-descriptor-position", idx),
			blockInlineDescriptorPosition,
			"Position",
			element.Pos.Meta,
			strconv.FormatUint(uint64(element.Pos.Value), 10),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-inline-descriptor-key-size", idx),
			blockInlineDescriptorKeySize,
			"Key size",
			element.KeySize.Meta,
			byteCount(int(element.KeySize.Value)),
		),
		bboltFieldBlock(
			fmt.Sprintf("leaf-entry-%d-inline-descriptor-value-size", idx),
			blockInlineDescriptorValueSize,
			"Value size",
			element.ValueSize.Meta,
			byteCount(int(element.ValueSize.Value)),
		),
	}
}

func bboltFieldBlock(id string, kind string, title string, meta bbolt.Meta, value string) HexBlock {
	return HexBlock{
		ID:    id,
		Kind:  kind,
		Title: title,
		Span:  bboltSpanFromMeta(meta),
		Rows: []Field{
			{Label: "Field", Value: title},
			{Label: "Offset", Value: bboltOffsetRange(meta)},
			{Label: "Size", Value: byteCount(meta.Size)},
			Blank(),
			Section("FIELD"),
			{Label: title, Value: value, Span: bboltSpanPtr(meta)},
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
