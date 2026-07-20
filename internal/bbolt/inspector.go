package bbolt

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"os"
)

type Inspector struct {
	path   string
	file   *os.File
	config BboltConfig
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
		Raw: append([]byte(nil), buf...),
	}
	var err error
	page.Header, err = parseHeader(buf)

	if err != nil {
		return page, fmt.Errorf("page header: %w", err)
	}
	switch page.Header.Flags.Value {
	case MetaPageFlag:
		page.MetaPayload, err = parseMetaPayload(buf, 16)
		if err != nil {
			return page, fmt.Errorf("page %d meta payload: %w", page.Header.ID.Value, err)
		}
	case BranchPageFlag, LeafPageFlag, FreelistPageFlag:
	default:
		return page, fmt.Errorf("page %d has unsupported bbolt page flag 0x%x", page.Header.ID.Value, uint16(page.Header.Flags.Value))
	}
	return page, nil
}

func parseHeader(buf []byte) (PageHeader, error) {
	header := PageHeader{}
	if len(buf) < 16 {
		return header, fmt.Errorf("Header size is 16 bytes, got %d bytes", len(buf))
	}

	header.Meta = metaFromOffset(0, 16)
	header.ID = PageIDField{
		Meta:  metaFromOffset(0, 8),
		Value: PageID(binary.LittleEndian.Uint64(buf[0:8])),
	}
	header.Flags = FlagField{
		Meta:  metaFromOffset(8, 2),
		Value: FlagType(binary.LittleEndian.Uint16(buf[8:10])),
	}
	header.Count = Uint16Field{
		Meta:  metaFromOffset(10, 2),
		Value: binary.LittleEndian.Uint16(buf[10:12]),
	}
	header.Overflow = Uint32Field{
		Meta:  metaFromOffset(12, 4),
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

	return &Inspector{
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
	}, nil
}

func (i *Inspector) Close() error {
	return i.file.Close()
}

func (i *Inspector) Config() BboltConfig {
	return i.config
}

func (i *Inspector) InspectPage(id PageID) (*BTreePage, error) {
	if id >= i.config.HighWaterMark {
		return nil, fmt.Errorf("page id %d out of range (high water mark: %d)", id, i.config.HighWaterMark)
	}

	pageSize := int(i.config.PageSize)
	buf := make([]byte, pageSize)
	offset := int64(id) * int64(i.config.PageSize)
	n, err := i.file.ReadAt(buf, offset)
	if n != len(buf) {
		return nil, fmt.Errorf("truncated bbolt page %d at offset %d: read %d of %d bytes", id, offset, n, len(buf))
	}
	if err != nil {
		return nil, err
	}

	page, err := parsePage(buf)
	if err != nil {
		return nil, fmt.Errorf("parse bbolt page %d: %w", id, err)
	}
	return &page, nil
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
