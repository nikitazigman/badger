package sqlite

import "fmt"

type GenericRecord struct {
	Row map[string]any
}

func ParseRecord(record *RecordPayload, definition *SchemaDefinition) (*GenericRecord, error) {
	if record == nil {
		return nil, fmt.Errorf("record payload is nil")
	}
	if record.OverflowFirstPage != nil {
		return nil, fmt.Errorf("record payload spills to overflow page %d and is not fully available", record.OverflowFirstPage.Value)
	}
	if definition == nil {
		return nil, fmt.Errorf("schema definition is nil")
	}

	result := &GenericRecord{
		Row: make(map[string]any, len(record.Columns)),
	}

	for idx, column := range record.Columns {
		name := fmt.Sprintf("__extra_%d", idx)
		if idx < len(definition.Fields) {
			name = definition.Fields[idx].Name
		}
		result.Row[name] = column.Value
	}

	return result, nil
}
