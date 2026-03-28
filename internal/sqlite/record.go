package sqlite

import (
	"encoding/binary"
	"fmt"
	"math"
)

type RecordPayload struct {
	HeaderSize        uint64
	SerialTypes       []uint64
	Columns           []RecordColumn
	UsesOverflow      bool
	OverflowFirstPage *uint32
}

type RecordColumn struct {
	SerialType uint64
	Value      any
}

func parseRecordPayload(payload []byte) (*RecordPayload, error) {
	headerSize, headerSizeVarintBytes, err := decodeVarint(payload)
	if err != nil {
		return nil, fmt.Errorf("record header size: %w", err)
	}
	if headerSize == 0 {
		return nil, fmt.Errorf("record header size is invalid: got 0")
	}
	if headerSize > uint64(len(payload)) {
		return nil, fmt.Errorf("record header size %d exceeds payload bytes %d", headerSize, len(payload))
	}

	headerEnd := int(headerSize)
	headerIdx := headerSizeVarintBytes
	serialTypes := make([]uint64, 0, 8)
	for headerIdx < headerEnd {
		serialType, varintBytes, err := decodeVarint(payload[headerIdx:headerEnd])
		if err != nil {
			return nil, fmt.Errorf("record serial type at header offset %d: %w", headerIdx, err)
		}
		serialTypes = append(serialTypes, serialType)
		headerIdx += varintBytes
	}
	if headerIdx != headerEnd {
		return nil, fmt.Errorf("record serial types do not align to header boundary: stopped at %d, header ends at %d", headerIdx, headerEnd)
	}

	bodyIdx := headerEnd
	columns := make([]RecordColumn, 0, len(serialTypes))
	for idx, serialType := range serialTypes {
		valueLen, err := serialTypeByteLength(serialType)
		if err != nil {
			return nil, fmt.Errorf("record serial type %d at column %d: %w", serialType, idx, err)
		}
		if bodyIdx+valueLen > len(payload) {
			return nil, fmt.Errorf("record body is truncated at column %d: need %d bytes, have %d", idx, valueLen, len(payload)-bodyIdx)
		}

		value, err := decodeRecordValue(serialType, payload[bodyIdx:bodyIdx+valueLen])
		if err != nil {
			return nil, fmt.Errorf("record value decode failed at column %d: %w", idx, err)
		}
		columns = append(columns, RecordColumn{
			SerialType: serialType,
			Value:      value,
		})
		bodyIdx += valueLen
	}

	return &RecordPayload{
		HeaderSize:  headerSize,
		SerialTypes: serialTypes,
		Columns:     columns,
	}, nil
}

func serialTypeByteLength(serialType uint64) (int, error) {
	switch serialType {
	case 0:
		return 0, nil
	case 1:
		return 1, nil
	case 2:
		return 2, nil
	case 3:
		return 3, nil
	case 4:
		return 4, nil
	case 5:
		return 6, nil
	case 6, 7:
		return 8, nil
	case 8, 9:
		return 0, nil
	case 10, 11:
		return 0, fmt.Errorf("reserved serial type")
	default:
		if serialType%2 == 0 {
			return int((serialType - 12) / 2), nil
		}
		return int((serialType - 13) / 2), nil
	}
}

func decodeRecordValue(serialType uint64, raw []byte) (any, error) {
	switch serialType {
	case 0:
		return nil, nil
	case 1, 2, 3, 4, 5, 6:
		return decodeSignedBigEndianInt(raw), nil
	case 7:
		if len(raw) != 8 {
			return nil, fmt.Errorf("float64 expects 8 bytes, got %d", len(raw))
		}
		return math.Float64frombits(binary.BigEndian.Uint64(raw)), nil
	case 8:
		return int64(0), nil
	case 9:
		return int64(1), nil
	case 10, 11:
		return nil, fmt.Errorf("reserved serial type")
	default:
		buf := make([]byte, len(raw))
		copy(buf, raw)
		return buf, nil
	}
}

func decodeSignedBigEndianInt(raw []byte) int64 {
	if len(raw) == 0 {
		return 0
	}

	var unsigned uint64
	for _, b := range raw {
		unsigned = (unsigned << 8) | uint64(b)
	}

	bits := uint(len(raw) * 8)
	signMask := uint64(1) << (bits - 1)
	if unsigned&signMask == 0 {
		return int64(unsigned)
	}

	return int64(unsigned - (uint64(1) << bits))
}
