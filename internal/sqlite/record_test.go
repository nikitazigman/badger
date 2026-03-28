package sqlite

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRecordPayload(t *testing.T) {
	t.Parallel()

	t.Run("null_int_text_blob", func(t *testing.T) {
		t.Parallel()

		payload := []byte{0x05, 0x00, 0x01, 0x0f, 0x0e, 0x7f, 0x41, 0xff}
		got, err := parseRecordPayload(payload, 1, fixturePageSize, 200)
		if err != nil {
			t.Fatalf("parseRecordPayload returned error: %v", err)
		}

		if got.HeaderSize.Value != 5 {
			t.Fatalf("HeaderSize = %d, want 5", got.HeaderSize.Value)
		}
		wantSerialTypes := []uint64{0, 1, 15, 14}
		if !reflect.DeepEqual(serialTypeValues(got.SerialTypes), wantSerialTypes) {
			t.Fatalf("SerialTypes = %v, want %v", got.SerialTypes, wantSerialTypes)
		}
		if len(got.Columns) != len(wantSerialTypes) {
			t.Fatalf("len(Columns) = %d, want %d", len(got.Columns), len(wantSerialTypes))
		}
		if got.Columns[0].Value != nil {
			t.Fatalf("column 0 value = %#v, want nil", got.Columns[0].Value)
		}
		if got.Columns[1].Value != int64(127) {
			t.Fatalf("column 1 value = %#v, want %d", got.Columns[1].Value, int64(127))
		}
		if got.Columns[2].Value != "A" {
			t.Fatalf("column 2 value = %#v, want %q", got.Columns[2].Value, "A")
		}
		if !reflect.DeepEqual(got.Columns[3].Value, []byte{0xff}) {
			t.Fatalf("column 3 value = %#v, want []byte{0xff}", got.Columns[3].Value)
		}
		if storageClassForSerialType(got.Columns[2].SerialType) != "text" {
			t.Fatalf("column 2 storage class = %q, want %q", storageClassForSerialType(got.Columns[2].SerialType), "text")
		}
		if storageClassForSerialType(got.Columns[3].SerialType) != "blob" {
			t.Fatalf("column 3 storage class = %q, want %q", storageClassForSerialType(got.Columns[3].SerialType), "blob")
		}
		if got.Meta.StartOffset != 200 || got.Meta.EndOffset() != 208 {
			t.Fatalf("record meta = %+v, want offsets 200..208", got.Meta)
		}
	})

	t.Run("serial_types_8_and_9_constants", func(t *testing.T) {
		t.Parallel()

		got, err := parseRecordPayload([]byte{0x03, 0x08, 0x09}, 1, fixturePageSize, 300)
		if err != nil {
			t.Fatalf("parseRecordPayload returned error: %v", err)
		}
		if len(got.Columns) != 2 {
			t.Fatalf("len(Columns) = %d, want 2", len(got.Columns))
		}
		if got.Columns[0].Value != int64(0) {
			t.Fatalf("column 0 value = %#v, want 0", got.Columns[0].Value)
		}
		if got.Columns[1].Value != int64(1) {
			t.Fatalf("column 1 value = %#v, want 1", got.Columns[1].Value)
		}
	})
}

func TestParseRecordPayloadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
		wantErr string
	}{
		{
			name:    "truncated_header_size_varint",
			payload: []byte{0x81},
			wantErr: "record header size",
		},
		{
			name:    "header_size_out_of_bounds",
			payload: []byte{0x05, 0x00},
			wantErr: "exceeds payload bytes",
		},
		{
			name:    "truncated_body",
			payload: []byte{0x03, 0x01, 0x01, 0x7f},
			wantErr: "record body is truncated",
		},
		{
			name:    "reserved_serial_type_10",
			payload: []byte{0x02, 0x0a},
			wantErr: "reserved serial type",
		},
		{
			name:    "reserved_serial_type_11",
			payload: []byte{0x02, 0x0b},
			wantErr: "reserved serial type",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseRecordPayload(tc.payload, 1, fixturePageSize, 0)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func serialTypeValues(entries []VarintField) []uint64 {
	values := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		values = append(values, entry.Value)
	}
	return values
}
