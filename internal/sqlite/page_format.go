package sqlite

import (
	"encoding/binary"
	"fmt"
)

type PageFormat string

const (
	PageFormatBTree         PageFormat = "btree"
	PageFormatOverflow      PageFormat = "overflow"
	PageFormatFreelistTrunk PageFormat = "freelist_trunk"
	PageFormatFreelistLeaf  PageFormat = "freelist_leaf"
	PageFormatUnknown       PageFormat = "unknown"
)

type OverflowPage struct {
	Raw             []byte
	UsablePageBytes uint16
	NextPage        Uint32Field
	Payload         Meta
}

type OverflowPageOwner struct {
	ParentPage      uint32
	CellIndex       int
	CellKind        string
	FirstPage       uint32
	PartIndex       int
	PartCount       int
	OverflowPointer Meta
	ParentCell      Meta
	ParentPayload   Meta
}

type FreelistTrunkPage struct {
	Raw             []byte
	UsablePageBytes uint16
	NextTrunkPage   Uint32Field
	LeafPageCount   Uint32Field
	LeafPages       []Uint32Field
	Payload         Meta
}

type FreelistLeafPage struct {
	Raw             []byte
	UsablePageBytes uint16
	Payload         Meta
}

type UnknownPage struct {
	Raw     []byte
	Payload Meta
}

func parseOverflowPage(page []byte, pageNumber uint32, reservedBytesPerPage uint8) (*OverflowPage, error) {
	usable, err := usablePageSize(page, reservedBytesPerPage)
	if err != nil {
		return nil, err
	}
	if usable < 4 {
		return nil, fmt.Errorf("overflow page usable size %d is too small", usable)
	}

	return &OverflowPage{
		Raw:             append([]byte(nil), page...),
		UsablePageBytes: usable,
		NextPage: Uint32Field{
			Meta:  metaFromPage(pageNumber, uint32(len(page)), 0, 4),
			Value: binary.BigEndian.Uint32(page[0:4]),
		},
		Payload: metaFromPage(pageNumber, uint32(len(page)), 4, int(usable)-4),
	}, nil
}

func parseFreelistTrunkPage(page []byte, pageNumber uint32, reservedBytesPerPage uint8) (*FreelistTrunkPage, error) {
	usable, err := usablePageSize(page, reservedBytesPerPage)
	if err != nil {
		return nil, err
	}
	if usable < 8 {
		return nil, fmt.Errorf("freelist trunk page usable size %d is too small", usable)
	}

	leafCount := binary.BigEndian.Uint32(page[4:8])
	maxLeafCount := (uint32(usable) - 8) / 4
	if leafCount > maxLeafCount {
		return nil, fmt.Errorf("freelist trunk leaf count %d exceeds capacity %d", leafCount, maxLeafCount)
	}

	leafPages := make([]Uint32Field, 0, leafCount)
	for idx := uint32(0); idx < leafCount; idx++ {
		offset := 8 + int(idx)*4
		leafPages = append(leafPages, Uint32Field{
			Meta:  metaFromPage(pageNumber, uint32(len(page)), offset, 4),
			Value: binary.BigEndian.Uint32(page[offset : offset+4]),
		})
	}

	return &FreelistTrunkPage{
		Raw:             append([]byte(nil), page...),
		UsablePageBytes: usable,
		NextTrunkPage: Uint32Field{
			Meta:  metaFromPage(pageNumber, uint32(len(page)), 0, 4),
			Value: binary.BigEndian.Uint32(page[0:4]),
		},
		LeafPageCount: Uint32Field{
			Meta:  metaFromPage(pageNumber, uint32(len(page)), 4, 4),
			Value: leafCount,
		},
		LeafPages: leafPages,
		Payload:   metaFromPage(pageNumber, uint32(len(page)), 8, int(leafCount)*4),
	}, nil
}

func parseFreelistLeafPage(page []byte, pageNumber uint32, reservedBytesPerPage uint8) (*FreelistLeafPage, error) {
	usable, err := usablePageSize(page, reservedBytesPerPage)
	if err != nil {
		return nil, err
	}
	return &FreelistLeafPage{
		Raw:             append([]byte(nil), page...),
		UsablePageBytes: usable,
		Payload:         metaFromPage(pageNumber, uint32(len(page)), 0, int(usable)),
	}, nil
}

func parseUnknownPage(page []byte, pageNumber uint32) *UnknownPage {
	return &UnknownPage{
		Raw:     append([]byte(nil), page...),
		Payload: metaFromPage(pageNumber, uint32(len(page)), 0, len(page)),
	}
}
