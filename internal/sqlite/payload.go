package sqlite

import (
	"encoding/binary"
	"fmt"
)

func cellLocalPayload(cell []byte, pageKind PageKindType, payloadSize uint64, usablePageBytes uint16) ([]byte, *uint32, error) {
	localBytes, err := localPayloadBytes(pageKind, payloadSize, usablePageBytes)
	if err != nil {
		return nil, nil, err
	}

	payloadOffset, err := payloadStartOffset(cell, pageKind)
	if err != nil {
		return nil, nil, err
	}
	if payloadOffset > len(cell) {
		return nil, nil, fmt.Errorf("payload start offset %d exceeds cell bytes %d", payloadOffset, len(cell))
	}
	if uint64(payloadOffset)+localBytes > uint64(len(cell)) {
		return nil, nil, fmt.Errorf("cell payload is truncated: need %d local payload bytes, have %d", localBytes, uint64(len(cell))-uint64(payloadOffset))
	}

	start := payloadOffset
	end := payloadOffset + int(localBytes)
	payload := make([]byte, int(localBytes))
	copy(payload, cell[start:end])

	if localBytes < payloadSize {
		if end+4 > len(cell) {
			return nil, nil, fmt.Errorf("overflow pointer is truncated: need 4 bytes, have %d", len(cell)-end)
		}
		firstOverflow := binary.BigEndian.Uint32(cell[end : end+4])
		return payload, &firstOverflow, nil
	}

	return payload, nil, nil
}

func localPayloadBytes(pageKind PageKindType, payloadSize uint64, usablePageBytes uint16) (uint64, error) {
	usable := int64(usablePageBytes)
	if usable < 12 {
		return 0, fmt.Errorf("usable page bytes %d are invalid for payload distribution", usablePageBytes)
	}

	m := ((usable - 12) * 32 / 255) - 23
	if m < 0 {
		return 0, fmt.Errorf("computed minimum local payload %d is invalid", m)
	}

	var x int64
	switch pageKind {
	case LeafTableBTreePage:
		x = usable - 35
	case LeafIndexBTreePage, InteriorIndexBTreePage:
		x = ((usable - 12) * 64 / 255) - 23
	default:
		return 0, fmt.Errorf("page kind 0x%02x does not carry record payload", pageKind)
	}

	if x < 0 {
		return 0, fmt.Errorf("computed maximum local payload %d is invalid", x)
	}

	if payloadSize <= uint64(x) {
		return payloadSize, nil
	}

	k := m + (int64((payloadSize - uint64(m)) % uint64(usable-4)))
	if k <= x {
		return uint64(k), nil
	}
	return uint64(m), nil
}

func payloadStartOffset(cell []byte, pageKind PageKindType) (int, error) {
	switch pageKind {
	case LeafTableBTreePage:
		_, payloadVarintBytes, err := decodeVarint(cell)
		if err != nil {
			return 0, fmt.Errorf("table leaf payload size varint: %w", err)
		}
		_, rowIDVarintBytes, err := decodeVarint(cell[payloadVarintBytes:])
		if err != nil {
			return 0, fmt.Errorf("table leaf rowid varint: %w", err)
		}
		return payloadVarintBytes + rowIDVarintBytes, nil
	case LeafIndexBTreePage:
		_, payloadVarintBytes, err := decodeVarint(cell)
		if err != nil {
			return 0, fmt.Errorf("index leaf payload size varint: %w", err)
		}
		return payloadVarintBytes, nil
	case InteriorIndexBTreePage:
		if len(cell) < 4 {
			return 0, fmt.Errorf("index interior cell is truncated: expected at least 4 bytes, got %d", len(cell))
		}
		_, payloadVarintBytes, err := decodeVarint(cell[4:])
		if err != nil {
			return 0, fmt.Errorf("index interior payload size varint: %w", err)
		}
		return 4 + payloadVarintBytes, nil
	default:
		return 0, fmt.Errorf("page kind 0x%02x does not carry record payload", pageKind)
	}
}
