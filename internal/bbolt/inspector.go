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
	Version  uint32
	PageSize uint32
	Root     PageID
	Freelist PageID
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
	if metaPage.MetaPayload.Magic != Magic {
		return nil, fmt.Errorf("invalid bbolt magic: 0x%x", metaPage.MetaPayload.Magic)
	}
	if metaPage.MetaPayload.Version != Version {
		return nil, fmt.Errorf("unsupported bbolt version: %d", metaPage.MetaPayload.Version)
	}

	// validate checksum
	h := fnv.New64a()
	_, err = h.Write(buf[16:72])
	if err != nil {
		return nil, err
	}
	if h.Sum64() != metaPage.MetaPayload.CheckSum {
		return nil, fmt.Errorf("Invalid meta page")
	}

	return metaPage.MetaPayload, nil
}

func parsePage(buf []byte) (BTreePage, error) {
	page := BTreePage{}
	var err error
	page.Header, err = parseHeader(buf)
	page.Header.Meta.StartOffset = 0
	page.Header.Meta.Size = 16

	if err != nil {
		return page, err
	}
	switch page.Header.Flags {
	case MetaPageFlag:
		page.MetaPayload, err = parseMetaPayload(buf[16:])
		page.MetaPayload.Meta.Size = 64
		page.MetaPayload.Meta.StartOffset = 16
		if err != nil {
			return page, err
		}
		break
	default:
		return page, fmt.Errorf("Page type %d is not supported yet", page.Header.Flags)
	}
	return page, nil
}

func parseHeader(buf []byte) (PageHeader, error) {
	header := PageHeader{}
	if len(buf) < 16 {
		return header, fmt.Errorf("Header size is 16 bytes, got %d bytes", len(buf))
	}

	header.ID = PageID(binary.LittleEndian.Uint64(buf[0:8]))
	header.Flags = FlagType(binary.LittleEndian.Uint16(buf[8:10]))
	header.Count = binary.LittleEndian.Uint16(buf[10:12])
	header.Overflow = binary.LittleEndian.Uint32(buf[12:16])

	return header, nil
}

func parseMetaPayload(buf []byte) (*MetaPayload, error) {
	payload := &MetaPayload{}

	if len(buf) < 64 {
		return payload, fmt.Errorf("Meta payload size is 64 bytes, got %d bytes", len(buf))
	}

	payload.Magic = binary.LittleEndian.Uint32(buf[0:4])
	payload.Version = binary.LittleEndian.Uint32(buf[4:8])
	payload.PageSize = binary.LittleEndian.Uint32(buf[8:12])
	payload.Flags = binary.LittleEndian.Uint32(buf[12:16])
	payload.Root = PageID(binary.LittleEndian.Uint64(buf[16:24]))
	payload.Sequence = binary.LittleEndian.Uint64(buf[24:32])
	payload.FreeList = PageID(binary.LittleEndian.Uint64(buf[32:40]))
	payload.PageID = PageID(binary.LittleEndian.Uint64(buf[40:48]))
	payload.TransactionID = binary.LittleEndian.Uint64(buf[48:56])
	payload.CheckSum = binary.LittleEndian.Uint64(buf[56:64])

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
		if meta1, err := readMeta(buf, f, int64(meta0.PageSize)); err == nil {
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
			Version:  meta.Version,
			PageSize: meta.PageSize,
			Root:     meta.Root,
			Freelist: meta.FreeList,
		},
	}, nil
}

func (i *Inspector) Close() error {
	return i.file.Close()
}

func newerMeta(current *MetaPayload, candidate *MetaPayload) *MetaPayload {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.TransactionID > current.TransactionID {
		return candidate
	}
	return current
}
