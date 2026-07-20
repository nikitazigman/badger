package bbolt

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"os"
)

type Inspector struct {
	path          string
	file          *os.File
	config        BboltConfig
	freePages     map[PageID]bool
	pageSummaries []PageSummary
	continuations map[PageID]PageID
}
type BboltConfig struct {
	Version       uint32
	PageSize      uint32
	Root          PageID
	Freelist      PageID
	HighWaterMark PageID
	TransactionID uint64
}

func readMeta(buf []byte, f *os.File, offset int64) (*MetaPayload, error) {
	_, err := f.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}

	metaPage, err := parsePage(buf)
	if err != nil {
		return nil, err
	}
	if metaPage.MetaPayload == nil {
		return nil, fmt.Errorf("page is not a bbolt meta page")
	}
	if metaPage.MetaPayload.Magic.Value != Magic {
		return nil, fmt.Errorf("invalid bbolt magic: 0x%x", metaPage.MetaPayload.Magic.Value)
	}
	if metaPage.MetaPayload.Version.Value != Version {
		return nil, fmt.Errorf("unsupported bbolt version: %d", metaPage.MetaPayload.Version.Value)
	}

	// validate checksum
	h := fnv.New64a()
	_, err = h.Write(buf[16:72])
	if err != nil {
		return nil, err
	}
	if h.Sum64() != metaPage.MetaPayload.CheckSum.Value {
		return nil, fmt.Errorf("Invalid meta page")
	}

	return metaPage.MetaPayload, nil
}

func parsePage(buf []byte) (BTreePage, error) {
	page := BTreePage{
		Raw:            append([]byte(nil), buf...),
		Classification: PageClassUnknown,
	}
	var err error
	page.Header, err = parseHeader(buf)

	if err != nil {
		return page, fmt.Errorf("page header: %w", err)
	}
	page.HasHeader = true
	page.ID = page.Header.ID.Value
	switch page.Header.Flags.Value {
	case MetaPageFlag:
		page.Classification = PageClassMeta
		page.MetaPayload, err = parseMetaPayload(buf, 16)
		if err != nil {
			return page, fmt.Errorf("page %d meta payload: %w", page.Header.ID.Value, err)
		}
	case BranchPageFlag:
		page.Classification = PageClassBranch
		page.BranchPayload, err = parseBranchPayload(buf, page.Header.Count.Value)
		if err != nil {
			return page, fmt.Errorf("page %d branch payload: %w", page.Header.ID.Value, err)
		}
	case LeafPageFlag:
		page.Classification = PageClassLeaf
		page.LeafPayload, err = parseLeafPayload(buf, page.Header.Count.Value)
		if err != nil {
			return page, fmt.Errorf("page %d leaf payload: %w", page.Header.ID.Value, err)
		}
	case FreelistPageFlag:
		page.Classification = PageClassFreelist
		page.FreelistPayload, err = parseFreelistPayload(buf, page.Header.Count.Value)
		if err != nil {
			return page, fmt.Errorf("page %d freelist payload: %w", page.Header.ID.Value, err)
		}
	default:
		return page, fmt.Errorf("page %d has unsupported bbolt page flag 0x%x", page.Header.ID.Value, uint16(page.Header.Flags.Value))
	}
	return page, nil
}

func parseHeader(buf []byte) (PageHeader, error) {
	return parseHeaderAt(buf, 0)
}

func parseHeaderAt(buf []byte, metaBase int) (PageHeader, error) {
	header := PageHeader{}
	if len(buf) < 16 {
		return header, fmt.Errorf("Header size is 16 bytes, got %d bytes", len(buf))
	}

	header.Meta = metaFromOffset(metaBase, 16)
	header.ID = PageIDField{
		Meta:  metaFromOffset(metaBase, 8),
		Value: PageID(binary.LittleEndian.Uint64(buf[0:8])),
	}
	header.Flags = FlagField{
		Meta:  metaFromOffset(metaBase+8, 2),
		Value: FlagType(binary.LittleEndian.Uint16(buf[8:10])),
	}
	header.Count = Uint16Field{
		Meta:  metaFromOffset(metaBase+10, 2),
		Value: binary.LittleEndian.Uint16(buf[10:12]),
	}
	header.Overflow = Uint32Field{
		Meta:  metaFromOffset(metaBase+12, 4),
		Value: binary.LittleEndian.Uint32(buf[12:16]),
	}

	return header, nil
}

func parseMetaPayload(page []byte, start int) (*MetaPayload, error) {
	payload := &MetaPayload{}

	if start < 0 || start > len(page) {
		return payload, fmt.Errorf("invalid meta payload offset %d for page size %d", start, len(page))
	}
	if len(page)-start < 64 {
		return payload, fmt.Errorf("Meta payload size is 64 bytes, got %d bytes", len(page)-start)
	}

	buf := page[start : start+64]
	payload.Meta = metaFromOffset(start, 64)
	payload.Magic = Uint32Field{
		Meta:  metaFromOffset(start, 4),
		Value: binary.LittleEndian.Uint32(buf[0:4]),
	}
	payload.Version = Uint32Field{
		Meta:  metaFromOffset(start+4, 4),
		Value: binary.LittleEndian.Uint32(buf[4:8]),
	}
	payload.PageSize = Uint32Field{
		Meta:  metaFromOffset(start+8, 4),
		Value: binary.LittleEndian.Uint32(buf[8:12]),
	}
	payload.Flags = Uint32Field{
		Meta:  metaFromOffset(start+12, 4),
		Value: binary.LittleEndian.Uint32(buf[12:16]),
	}
	payload.Root = PageIDField{
		Meta:  metaFromOffset(start+16, 8),
		Value: PageID(binary.LittleEndian.Uint64(buf[16:24])),
	}
	payload.Sequence = Uint64Field{
		Meta:  metaFromOffset(start+24, 8),
		Value: binary.LittleEndian.Uint64(buf[24:32]),
	}
	payload.FreeList = PageIDField{
		Meta:  metaFromOffset(start+32, 8),
		Value: PageID(binary.LittleEndian.Uint64(buf[32:40])),
	}
	payload.PageID = PageIDField{
		Meta:  metaFromOffset(start+40, 8),
		Value: PageID(binary.LittleEndian.Uint64(buf[40:48])),
	}
	payload.TransactionID = Uint64Field{
		Meta:  metaFromOffset(start+48, 8),
		Value: binary.LittleEndian.Uint64(buf[48:56]),
	}
	payload.CheckSum = Uint64Field{
		Meta:  metaFromOffset(start+56, 8),
		Value: binary.LittleEndian.Uint64(buf[56:64]),
	}

	return payload, nil
}

func parseFreelistPayload(page []byte, count uint16) (*FreelistPayload, error) {
	const start = 16

	payload := &FreelistPayload{}
	if len(page) < start {
		return payload, fmt.Errorf("freelist page size is %d bytes, want at least %d", len(page), start)
	}

	actualCount := uint64(count)
	idsStart := start
	if count == 0xffff {
		if len(page)-start < 8 {
			return payload, fmt.Errorf("freelist actual count size is 8 bytes, got %d bytes", len(page)-start)
		}
		field := Uint64Field{
			Meta:  metaFromOffset(start, 8),
			Value: binary.LittleEndian.Uint64(page[start : start+8]),
		}
		payload.ActualCount = &field
		actualCount = field.Value
		idsStart += 8
	}

	if actualCount > uint64((len(page)-idsStart)/8) {
		return payload, fmt.Errorf("freelist id count %d exceeds available payload bytes %d", actualCount, len(page)-idsStart)
	}

	size := int(actualCount) * 8
	payload.Meta = metaFromOffset(start, idsStart-start+size)
	payload.IDFields = make([]PageIDField, 0, actualCount)
	payload.IDs = make([]PageID, 0, actualCount)
	for idx := uint64(0); idx < actualCount; idx++ {
		offset := idsStart + int(idx)*8
		field := PageIDField{
			Meta:  metaFromOffset(offset, 8),
			Value: PageID(binary.LittleEndian.Uint64(page[offset : offset+8])),
		}
		payload.IDFields = append(payload.IDFields, field)
		payload.IDs = append(payload.IDs, field.Value)
	}

	return payload, nil
}

func parseBranchPayload(page []byte, count uint16) (*BranchPayload, error) {
	const start = 16
	const descriptorSize = 16

	payload := &BranchPayload{}
	if len(page) < start {
		return payload, fmt.Errorf("branch page size is %d bytes, want at least %d", len(page), start)
	}

	descriptorBytes := int(count) * descriptorSize
	descriptorsEnd := start + descriptorBytes
	if descriptorsEnd > len(page) {
		return payload, fmt.Errorf("branch descriptor bytes %d exceed available payload bytes %d", descriptorBytes, len(page)-start)
	}

	payload.Meta = metaFromOffset(start, descriptorBytes)
	payload.BranchElements = make([]BranchElement, 0, count)
	payload.KeyValue = make([]KeyValue, 0, count)

	maxEnd := descriptorsEnd
	for idx := 0; idx < int(count); idx++ {
		descStart := start + idx*descriptorSize
		descEnd := descStart + descriptorSize
		descriptor := page[descStart:descEnd]

		element := BranchElement{
			Meta: metaFromOffset(descStart, descriptorSize),
			Pos: Uint32Field{
				Meta:  metaFromOffset(descStart, 4),
				Value: binary.LittleEndian.Uint32(descriptor[0:4]),
			},
			KeySize: Uint32Field{
				Meta:  metaFromOffset(descStart+4, 4),
				Value: binary.LittleEndian.Uint32(descriptor[4:8]),
			},
			PageID: PageIDField{
				Meta:  metaFromOffset(descStart+8, 8),
				Value: PageID(binary.LittleEndian.Uint64(descriptor[8:16])),
			},
		}

		keyStart, keyEnd, err := payloadRange(page, descStart, element.Pos.Value, element.KeySize.Value)
		if err != nil {
			return payload, fmt.Errorf("branch element %d key: %w", idx, err)
		}

		kv := KeyValue{
			Meta: metaFromOffset(keyStart, keyEnd-keyStart),
			Key: Payload{
				Meta: metaFromOffset(keyStart, keyEnd-keyStart),
				Data: append([]byte(nil), page[keyStart:keyEnd]...),
			},
		}

		payload.BranchElements = append(payload.BranchElements, element)
		payload.KeyValue = append(payload.KeyValue, kv)
		if keyEnd > maxEnd {
			maxEnd = keyEnd
		}
	}
	payload.Meta = metaFromOffset(start, maxEnd-start)

	return payload, nil
}

func parseLeafPayload(page []byte, count uint16) (*LeafPayload, error) {
	return parseLeafPayloadAt(page, count, 0)
}

func parseLeafPayloadAt(page []byte, count uint16, metaBase int) (*LeafPayload, error) {
	const start = 16
	const descriptorSize = 16

	payload := &LeafPayload{}
	if len(page) < start {
		return payload, fmt.Errorf("leaf page size is %d bytes, want at least %d", len(page), start)
	}

	descriptorBytes := int(count) * descriptorSize
	descriptorsEnd := start + descriptorBytes
	if descriptorsEnd > len(page) {
		return payload, fmt.Errorf("leaf descriptor bytes %d exceed available payload bytes %d", descriptorBytes, len(page)-start)
	}

	payload.Meta = metaFromOffset(start, descriptorBytes)
	payload.LeafElements = make([]LeafElement, 0, count)
	payload.KeyValue = make([]KeyValue, 0, count)
	payload.NestedBucket = make([]NestedBucket, 0)
	payload.InlineBucket = make([]InlineBucket, 0)

	maxEnd := descriptorsEnd
	for idx := 0; idx < int(count); idx++ {
		descStart := start + idx*descriptorSize
		descEnd := descStart + descriptorSize
		descriptor := page[descStart:descEnd]

		element := LeafElement{
			Meta: metaFromOffset(metaBase+descStart, descriptorSize),
			Flags: LeafFlagField{
				Meta:  metaFromOffset(metaBase+descStart, 4),
				Value: LeafFlagType(binary.LittleEndian.Uint32(descriptor[0:4])),
			},
			Pos: Uint32Field{
				Meta:  metaFromOffset(metaBase+descStart+4, 4),
				Value: binary.LittleEndian.Uint32(descriptor[4:8]),
			},
			KeySize: Uint32Field{
				Meta:  metaFromOffset(metaBase+descStart+8, 4),
				Value: binary.LittleEndian.Uint32(descriptor[8:12]),
			},
			ValueSize: Uint32Field{
				Meta:  metaFromOffset(metaBase+descStart+12, 4),
				Value: binary.LittleEndian.Uint32(descriptor[12:16]),
			},
		}

		keyStart, keyEnd, err := payloadRange(page, descStart, element.Pos.Value, element.KeySize.Value)
		if err != nil {
			return payload, fmt.Errorf("leaf element %d key: %w", idx, err)
		}
		valueStart, valueEnd, err := payloadRange(page, keyEnd, 0, element.ValueSize.Value)
		if err != nil {
			return payload, fmt.Errorf("leaf element %d value: %w", idx, err)
		}

		kv := KeyValue{
			Meta: metaFromOffset(metaBase+keyStart, valueEnd-keyStart),
			Key: Payload{
				Meta: metaFromOffset(metaBase+keyStart, keyEnd-keyStart),
				Data: append([]byte(nil), page[keyStart:keyEnd]...),
			},
			Value: Payload{
				Meta: metaFromOffset(metaBase+valueStart, valueEnd-valueStart),
				Data: append([]byte(nil), page[valueStart:valueEnd]...),
			},
		}

		payload.LeafElements = append(payload.LeafElements, element)
		payload.KeyValue = append(payload.KeyValue, kv)
		if element.Flags.Value == BucketLeafFlag {
			bucket, err := parseNestedBucket(kv.Value)
			if err != nil {
				return payload, fmt.Errorf("leaf element %d bucket header: %w", idx, err)
			}
			payload.NestedBucket = append(payload.NestedBucket, bucket)
			if bucket.Root.Value == 0 {
				inline, err := parseInlineBucket(kv.Value)
				if err != nil {
					return payload, fmt.Errorf("leaf element %d inline bucket: %w", idx, err)
				}
				payload.InlineBucket = append(payload.InlineBucket, inline)
			}
		}
		if valueEnd > maxEnd {
			maxEnd = valueEnd
		}

	}
	payload.Meta = metaFromOffset(metaBase+start, maxEnd-start)

	return payload, nil
}

func parseNestedBucket(value Payload) (NestedBucket, error) {
	const size = 16

	bucket := NestedBucket{}
	if len(value.Data) < size {
		return bucket, fmt.Errorf("bucket header size is %d bytes, got %d bytes", size, len(value.Data))
	}
	start := value.Meta.StartOffset
	bucket.Meta = metaFromOffset(start, size)
	bucket.Root = PageIDField{
		Meta:  metaFromOffset(start, 8),
		Value: PageID(binary.LittleEndian.Uint64(value.Data[0:8])),
	}
	bucket.Sequence = Uint64Field{
		Meta:  metaFromOffset(start+8, 8),
		Value: binary.LittleEndian.Uint64(value.Data[8:16]),
	}
	return bucket, nil
}

func parseInlineBucket(value Payload) (InlineBucket, error) {
	const bucketHeaderSize = 16
	const pageHeaderSize = 16

	inline := InlineBucket{}
	if len(value.Data) < bucketHeaderSize+pageHeaderSize {
		return inline, fmt.Errorf("inline bucket size is %d bytes, want at least %d bytes", len(value.Data), bucketHeaderSize+pageHeaderSize)
	}

	pageStart := bucketHeaderSize
	pageBytes := value.Data[pageStart:]
	pageMetaBase := value.Meta.StartOffset + pageStart

	header, err := parseHeaderAt(pageBytes, pageMetaBase)
	if err != nil {
		return inline, err
	}
	if header.Flags.Value != LeafPageFlag {
		return inline, fmt.Errorf("inline bucket page flag is 0x%x, want leaf", uint16(header.Flags.Value))
	}
	if header.Overflow.Value != 0 {
		return inline, fmt.Errorf("inline bucket overflow is %d, want 0", header.Overflow.Value)
	}

	leaf, err := parseLeafPayloadAt(pageBytes, header.Count.Value, pageMetaBase)
	if err != nil {
		return inline, err
	}
	inline.Meta = metaFromOffset(pageMetaBase, leaf.Meta.EndOffset()-pageMetaBase)
	inline.Header = header
	inline.LeafPayload = *leaf
	return inline, nil
}

func payloadRange(page []byte, base int, relative uint32, size uint32) (int, int, error) {
	start := uint64(base) + uint64(relative)
	end := start + uint64(size)
	if start > uint64(len(page)) {
		return 0, 0, fmt.Errorf("start offset %d exceeds page size %d", start, len(page))
	}
	if end < start || end > uint64(len(page)) {
		return 0, 0, fmt.Errorf("end offset %d exceeds page size %d", end, len(page))
	}
	return int(start), int(end), nil
}

func Open(path string) (*Inspector, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 80)
	var meta *MetaPayload

	// Meta page 0 is always at offset 0. Meta page 1 is one bbolt page after it,
	// so use meta0's page size when meta0 can be read.
	if meta0, err := readMeta(buf, f, 0); err == nil {
		meta = newerMeta(meta, meta0)
		if meta1, err := readMeta(buf, f, int64(meta0.PageSize.Value)); err == nil {
			meta = newerMeta(meta, meta1)
		}
	}

	// If meta0 is invalid, bbolt scans likely page sizes to find meta1.
	for i := range 15 {
		if meta1, err := readMeta(buf, f, int64(1024<<i)); err == nil {
			meta = newerMeta(meta, meta1)
		}
	}
	if meta == nil {
		_ = f.Close()
		return nil, fmt.Errorf("no valid bbolt meta page found")
	}

	inspector := &Inspector{
		path: path,
		file: f,
		config: BboltConfig{
			Version:       meta.Version.Value,
			PageSize:      meta.PageSize.Value,
			Root:          meta.Root.Value,
			Freelist:      meta.FreeList.Value,
			HighWaterMark: meta.PageID.Value,
			TransactionID: meta.TransactionID.Value,
		},
		freePages:     map[PageID]bool{},
		continuations: map[PageID]PageID{},
	}
	if err := inspector.loadPageState(); err != nil {
		_ = f.Close()
		return nil, err
	}

	return inspector, nil
}

func (i *Inspector) Close() error {
	return i.file.Close()
}

func (i *Inspector) Config() BboltConfig {
	return i.config
}

func (i *Inspector) PageSummaries() []PageSummary {
	return append([]PageSummary(nil), i.pageSummaries...)
}

func (i *Inspector) InspectPage(id PageID) (*BTreePage, error) {
	if id >= i.config.HighWaterMark {
		return nil, fmt.Errorf("page id %d out of range (high water mark: %d)", id, i.config.HighWaterMark)
	}

	page, err := i.readPhysicalPage(id)
	if err != nil {
		return nil, err
	}

	page.Classification = i.classificationFor(id, page)
	if owner, ok := i.continuations[id]; ok {
		page.ContinuationOf = &owner
	}

	if page.Classification == PageClassTruncated || !page.HasHeader {
		return page, nil
	}
	switch page.Classification {
	case PageClassMeta:
		page.MetaPayload, err = parseMetaPayload(page.Raw, 16)
		if err != nil {
			return page, fmt.Errorf("page %d meta payload: %w", id, err)
		}
	case PageClassFreelist:
		logical, err := i.readLogicalPageBytes(id)
		if err != nil {
			return page, err
		}
		page.FreelistPayload, err = parseFreelistPayload(logical, page.Header.Count.Value)
		if err != nil {
			return page, fmt.Errorf("page %d freelist payload: %w", id, err)
		}
	case PageClassBranch:
		logical, err := i.readLogicalPageBytes(id)
		if err != nil {
			return page, err
		}
		page.BranchPayload, err = parseBranchPayload(logical, page.Header.Count.Value)
		if err != nil {
			return page, fmt.Errorf("page %d branch payload: %w", id, err)
		}
	case PageClassLeaf:
		logical, err := i.readLogicalPageBytes(id)
		if err != nil {
			return page, err
		}
		page.LeafPayload, err = parseLeafPayload(logical, page.Header.Count.Value)
		if err != nil {
			return page, fmt.Errorf("page %d leaf payload: %w", id, err)
		}
	}
	return page, nil
}

func newerMeta(current *MetaPayload, candidate *MetaPayload) *MetaPayload {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.TransactionID.Value > current.TransactionID.Value {
		return candidate
	}
	return current
}

func (i *Inspector) loadPageState() error {
	if err := i.loadFreelist(); err != nil {
		return err
	}
	i.pageSummaries = i.buildPageSummaries()
	return nil
}

func (i *Inspector) loadFreelist() error {
	if i.config.Freelist == PgidNoFreelist {
		return nil
	}
	if i.config.Freelist >= i.config.HighWaterMark {
		return fmt.Errorf("freelist page id %d out of range (high water mark: %d)", i.config.Freelist, i.config.HighWaterMark)
	}

	page, err := i.readPhysicalPage(i.config.Freelist)
	if err != nil {
		return err
	}
	if page.Classification == PageClassTruncated || !page.HasHeader {
		return nil
	}
	if page.Header.Flags.Value != FreelistPageFlag {
		return nil
	}

	logical, err := i.readLogicalPageBytes(i.config.Freelist)
	if err != nil {
		return err
	}
	freelist, err := parseFreelistPayload(logical, page.Header.Count.Value)
	if err != nil {
		return fmt.Errorf("parse freelist page %d: %w", i.config.Freelist, err)
	}
	for _, id := range freelist.IDs {
		if id < i.config.HighWaterMark {
			i.freePages[id] = true
		}
	}
	return nil
}

func (i *Inspector) buildPageSummaries() []PageSummary {
	summaries := make([]PageSummary, 0, i.config.HighWaterMark)
	i.continuations = map[PageID]PageID{}

	for id := PageID(0); id < i.config.HighWaterMark; id++ {
		if i.freePages[id] {
			summaries = append(summaries, PageSummary{ID: id, Classification: PageClassFree})
			continue
		}
		if _, ok := i.continuations[id]; ok {
			summaries = append(summaries, PageSummary{ID: id, Classification: PageClassContinuation})
			continue
		}

		page, err := i.readPhysicalPage(id)
		classification := PageClassUnknown
		if err != nil || page.Classification == PageClassTruncated {
			classification = PageClassTruncated
		} else if page.HasHeader {
			classification = classificationForFlag(page.Header.Flags.Value)
			if isActiveLogicalClassification(classification) {
				for n := uint32(1); n <= page.Header.Overflow.Value; n++ {
					continuationID := id + PageID(n)
					if continuationID >= i.config.HighWaterMark {
						break
					}
					if i.freePages[continuationID] {
						continue
					}
					i.continuations[continuationID] = id
				}
			}
		}
		summaries = append(summaries, PageSummary{ID: id, Classification: classification})
	}

	return summaries
}

func (i *Inspector) readPhysicalPage(id PageID) (*BTreePage, error) {
	pageSize := int(i.config.PageSize)
	buf := make([]byte, pageSize)
	offset := int64(id) * int64(i.config.PageSize)
	n, err := i.file.ReadAt(buf, offset)
	if n != len(buf) {
		if err == nil || err == io.EOF {
			page := &BTreePage{
				ID:             id,
				Raw:            append([]byte(nil), buf[:n]...),
				Classification: PageClassTruncated,
			}
			if header, headerErr := parseHeader(page.Raw); headerErr == nil {
				page.Header = header
				page.HasHeader = true
			}
			return page, nil
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	page := &BTreePage{
		ID:             id,
		Raw:            append([]byte(nil), buf...),
		Classification: PageClassUnknown,
	}
	header, err := parseHeader(buf)
	if err != nil {
		return page, nil
	}
	page.Header = header
	page.HasHeader = true
	page.Classification = classificationForFlag(header.Flags.Value)
	return page, nil
}

func (i *Inspector) readLogicalPageBytes(id PageID) ([]byte, error) {
	page, err := i.readPhysicalPage(id)
	if err != nil {
		return nil, err
	}
	if page.Classification == PageClassTruncated {
		return nil, fmt.Errorf("truncated bbolt page %d", id)
	}
	pageSize := int(i.config.PageSize)
	logicalSize := pageSize
	if page.HasHeader {
		logicalSize *= int(page.Header.Overflow.Value) + 1
	}
	buf := make([]byte, logicalSize)
	offset := int64(id) * int64(i.config.PageSize)
	n, err := i.file.ReadAt(buf, offset)
	if n != len(buf) {
		return nil, fmt.Errorf("truncated bbolt logical page %d at offset %d: read %d of %d bytes", id, offset, n, len(buf))
	}
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (i *Inspector) classificationFor(id PageID, page *BTreePage) PageClassification {
	if i.freePages[id] {
		return PageClassFree
	}
	if _, ok := i.continuations[id]; ok {
		return PageClassContinuation
	}
	if page == nil {
		return PageClassUnknown
	}
	if page.Classification == PageClassTruncated {
		return PageClassTruncated
	}
	if !page.HasHeader {
		return PageClassUnknown
	}
	return classificationForFlag(page.Header.Flags.Value)
}

func classificationForFlag(flag FlagType) PageClassification {
	switch flag {
	case MetaPageFlag:
		return PageClassMeta
	case BranchPageFlag:
		return PageClassBranch
	case LeafPageFlag:
		return PageClassLeaf
	case FreelistPageFlag:
		return PageClassFreelist
	default:
		return PageClassUnknown
	}
}

func isActiveLogicalClassification(classification PageClassification) bool {
	switch classification {
	case PageClassMeta, PageClassBranch, PageClassLeaf, PageClassFreelist:
		return true
	default:
		return false
	}
}
