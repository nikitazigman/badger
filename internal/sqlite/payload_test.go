package sqlite

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestLocalPayloadBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    PageKindType
		payload uint64
		usable  uint16
		want    uint64
	}{
		{
			name:    "table_leaf_payload_fits",
			kind:    LeafTableBTreePage,
			payload: 100,
			usable:  4096,
			want:    100,
		},
		{
			name:    "table_leaf_boundary_p_equals_x",
			kind:    LeafTableBTreePage,
			payload: 4061,
			usable:  4096,
			want:    4061,
		},
		{
			name:    "table_leaf_boundary_p_equals_x_plus_1",
			kind:    LeafTableBTreePage,
			payload: 4062,
			usable:  4096,
			want:    489,
		},
		{
			name:    "index_leaf_boundary_p_equals_x",
			kind:    LeafIndexBTreePage,
			payload: 1002,
			usable:  4096,
			want:    1002,
		},
		{
			name:    "index_leaf_boundary_p_equals_x_plus_1",
			kind:    LeafIndexBTreePage,
			payload: 1003,
			usable:  4096,
			want:    489,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := localPayloadBytes(tc.kind, tc.payload, tc.usable)
			if err != nil {
				t.Fatalf("localPayloadBytes returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("localPayloadBytes = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCellLocalPayloadOverflowDetection(t *testing.T) {
	t.Parallel()

	t.Run("without_overflow_pointer", func(t *testing.T) {
		t.Parallel()

		localPayload := []byte{0x03, 0x08, 0x09}
		cell := append(append(encodeVarint(3), encodeVarint(1)...), localPayload...)
		gotPayload, gotOverflow, err := cellLocalPayload(cell, LeafTableBTreePage, 3, 4096)
		if err != nil {
			t.Fatalf("cellLocalPayload returned error: %v", err)
		}
		if string(gotPayload) != string(localPayload) {
			t.Fatalf("payload = %v, want %v", gotPayload, localPayload)
		}
		if gotOverflow != nil {
			t.Fatalf("overflow pointer = %v, want nil", *gotOverflow)
		}
	})

	t.Run("with_overflow_pointer", func(t *testing.T) {
		t.Parallel()

		const payloadSize = uint64(4062)
		localPayload := make([]byte, 489)
		cell := append(append(encodeVarint(payloadSize), encodeVarint(1)...), localPayload...)
		overflowPointer := make([]byte, 4)
		binary.BigEndian.PutUint32(overflowPointer, 17)
		cell = append(cell, overflowPointer...)

		gotPayload, gotOverflow, err := cellLocalPayload(cell, LeafTableBTreePage, payloadSize, 4096)
		if err != nil {
			t.Fatalf("cellLocalPayload returned error: %v", err)
		}
		if len(gotPayload) != len(localPayload) {
			t.Fatalf("len(payload) = %d, want %d", len(gotPayload), len(localPayload))
		}
		if gotOverflow == nil {
			t.Fatal("overflow pointer = nil, want non-nil")
		}
		if *gotOverflow != 17 {
			t.Fatalf("overflow pointer = %d, want 17", *gotOverflow)
		}
	})
}

func TestCellLocalPayloadErrors(t *testing.T) {
	t.Parallel()

	_, _, err := cellLocalPayload([]byte{0x81}, LeafIndexBTreePage, 1, 4096)
	if err == nil {
		t.Fatal("expected error for truncated payload-size varint, got nil")
	}
	if !strings.Contains(err.Error(), "payload size varint") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "payload size varint")
	}
}

func encodeVarint(v uint64) []byte {
	if v <= 0x7f {
		return []byte{byte(v)}
	}

	var chunks [9]byte
	count := 0
	for v > 0 && count < 8 {
		chunks[count] = byte(v & 0x7f)
		v >>= 7
		count++
	}

	result := make([]byte, count)
	for i := 0; i < count; i++ {
		b := chunks[count-1-i]
		if i != count-1 {
			b |= 0x80
		}
		result[i] = b
	}
	return result
}
