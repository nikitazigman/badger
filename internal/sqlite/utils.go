package sqlite

import "fmt"

func decodeVarint(buf []byte) (uint64, int, error) {
	if len(buf) == 0 {
		return 0, 0, fmt.Errorf("varint is truncated: expected at least 1 byte, got 0")
	}

	var value uint64
	for idx := 0; idx < len(buf) && idx < 8; idx++ {
		b := buf[idx]
		value = (value << 7) | uint64(b&0x7f)
		if b&0x80 == 0 {
			return value, idx + 1, nil
		}
	}

	if len(buf) < 9 {
		return 0, 0, fmt.Errorf("varint is truncated: expected up to 9 bytes, got %d", len(buf))
	}
	value = (value << 8) | uint64(buf[8])
	return value, 9, nil
}

func usablePageSize(page []byte, reservedBytes uint8) (uint16, error) {
	if int(reservedBytes) >= len(page) {
		return 0, fmt.Errorf("reserved bytes per page %d are invalid for page size %d", reservedBytes, len(page))
	}

	usable := len(page) - int(reservedBytes)
	if usable > 65535 {
		return 0, fmt.Errorf("usable page size %d exceeds uint16", usable)
	}
	return uint16(usable), nil
}
