package tui

import (
	"fmt"

	"github.com/nikitazigman/badger/internal/sqlite"
)

type drillState struct {
	active      bool
	parentBlock int
	stack       []drillFrame
}

type drillFrame struct {
	title         string
	children      []drillChild
	selectedChild int
}

type drillChildKind int

const (
	drillChildPayloadSize drillChildKind = iota
	drillChildRowID
	drillChildLeftChildPage
	drillChildRecordPayload
	drillChildRecordHeaderSize
	drillChildSerialType
	drillChildRecordValue
	drillChildOverflowPointer
)

type drillChild struct {
	kind        drillChildKind
	title       string
	parentTitle string
	meta        sqlite.Meta
	parsed      []string
	children    []drillChild
}

func buildDrillChildren(block pageBlock, page *sqlite.PageInspection) []drillChild {
	if page == nil {
		return nil
	}

	switch block.kind {
	case pageBlockTableLeafCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.TableLeafCells) {
			return tableLeafDrillChildren(block, page.BTreePage.TableLeafCells[block.cellIndex])
		}
	case pageBlockTableInteriorCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.TableInteriorCells) {
			return tableInteriorDrillChildren(block, page.BTreePage.TableInteriorCells[block.cellIndex])
		}
	case pageBlockIndexLeafCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.IndexLeafCells) {
			return indexLeafDrillChildren(block, page.BTreePage.IndexLeafCells[block.cellIndex])
		}
	case pageBlockIndexInteriorCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.IndexInteriorCells) {
			return indexInteriorDrillChildren(block, page.BTreePage.IndexInteriorCells[block.cellIndex])
		}
	}
	return nil
}

func tableLeafDrillChildren(block pageBlock, cell sqlite.TableLeafCell) []drillChild {
	parent := block.title()
	children := []drillChild{}
	children = appendDrillChild(children, drillChild{
		kind:        drillChildPayloadSize,
		title:       "Payload Size",
		parentTitle: parent,
		meta:        cell.PayloadSize.Meta,
		parsed: []string{
			fmt.Sprintf("Varint value: %d", cell.PayloadSize.Value),
			"Meaning: record payload bytes",
		},
	})
	children = appendDrillChild(children, drillChild{
		kind:        drillChildRowID,
		title:       "RowID",
		parentTitle: parent,
		meta:        cell.RowID.Meta,
		parsed: []string{
			fmt.Sprintf("Varint value: %d", cell.RowID.Value),
			"Meaning: table row key",
		},
	})
	return appendRecordPayloadDrillChild(children, parent, cell.ParsedPayload)
}

func tableInteriorDrillChildren(block pageBlock, cell sqlite.TableInteriorCell) []drillChild {
	parent := block.title()
	children := []drillChild{}
	children = appendDrillChild(children, drillChild{
		kind:        drillChildLeftChildPage,
		title:       "Left Child Page",
		parentTitle: parent,
		meta:        cell.LeftChildPage.Meta,
		parsed: []string{
			fmt.Sprintf("Page number: %d", cell.LeftChildPage.Value),
			"Meaning: child subtree",
		},
	})
	children = appendDrillChild(children, drillChild{
		kind:        drillChildRowID,
		title:       "RowID",
		parentTitle: parent,
		meta:        cell.RowID.Meta,
		parsed: []string{
			fmt.Sprintf("Varint value: %d", cell.RowID.Value),
			"Meaning: table row separator",
		},
	})
	return children
}

func indexLeafDrillChildren(block pageBlock, cell sqlite.IndexLeafCell) []drillChild {
	parent := block.title()
	children := []drillChild{}
	children = appendDrillChild(children, drillChild{
		kind:        drillChildPayloadSize,
		title:       "Payload Size",
		parentTitle: parent,
		meta:        cell.PayloadSize.Meta,
		parsed: []string{
			fmt.Sprintf("Varint value: %d", cell.PayloadSize.Value),
			"Meaning: record payload bytes",
		},
	})
	return appendRecordPayloadDrillChild(children, parent, cell.ParsedPayload)
}

func indexInteriorDrillChildren(block pageBlock, cell sqlite.IndexInteriorCell) []drillChild {
	parent := block.title()
	children := []drillChild{}
	children = appendDrillChild(children, drillChild{
		kind:        drillChildLeftChildPage,
		title:       "Left Child Page",
		parentTitle: parent,
		meta:        cell.LeftChildPage.Meta,
		parsed: []string{
			fmt.Sprintf("Page number: %d", cell.LeftChildPage.Value),
			"Meaning: child subtree",
		},
	})
	children = appendDrillChild(children, drillChild{
		kind:        drillChildPayloadSize,
		title:       "Payload Size",
		parentTitle: parent,
		meta:        cell.PayloadSize.Meta,
		parsed: []string{
			fmt.Sprintf("Varint value: %d", cell.PayloadSize.Value),
			"Meaning: record payload bytes",
		},
	})
	return appendRecordPayloadDrillChild(children, parent, cell.ParsedPayload)
}

func appendRecordPayloadDrillChild(children []drillChild, parent string, payload *sqlite.RecordPayload) []drillChild {
	if payload == nil {
		return children
	}

	payloadLines := []string{
		fmt.Sprintf("Header size: %d", payload.HeaderSize.Value),
		fmt.Sprintf("Serial types: %d", len(payload.SerialTypes)),
		fmt.Sprintf("Values: %d", len(payload.Columns)),
		"Overflow: " + yesNo(payload.OverflowFirstPage != nil),
	}
	if payload.HeaderSize.Meta.Size == 0 {
		payloadLines[0] = "Header size: unavailable"
	}

	children = appendDrillChild(children, drillChild{
		kind:        drillChildRecordPayload,
		title:       "Record Payload",
		parentTitle: parent,
		meta:        payload.Meta,
		parsed:      payloadLines,
		children:    recordPayloadDrillChildren(payload),
	})

	if payload.OverflowFirstPage != nil {
		children = appendDrillChild(children, drillChild{
			kind:        drillChildOverflowPointer,
			title:       "Overflow Pointer",
			parentTitle: parent,
			meta:        payload.OverflowFirstPage.Meta,
			parsed: []string{
				fmt.Sprintf("First overflow page: %d", payload.OverflowFirstPage.Value),
				"Meaning: payload continuation",
			},
		})
	}

	return children
}

func recordPayloadDrillChildren(payload *sqlite.RecordPayload) []drillChild {
	children := []drillChild{}
	if payload.HeaderSize.Meta.Size > 0 {
		headerEnd := payload.Meta.StartOffset + int(payload.HeaderSize.Value) - 1
		children = appendDrillChild(children, drillChild{
			kind:        drillChildRecordHeaderSize,
			title:       "Record Header Size",
			parentTitle: "Record Payload",
			meta:        payload.HeaderSize.Meta,
			parsed: []string{
				fmt.Sprintf("Header size: %d", payload.HeaderSize.Value),
				fmt.Sprintf("Header range: %d..%d", payload.Meta.StartOffset, headerEnd),
				fmt.Sprintf("Value area starts: %d", headerEnd+1),
			},
		})
	}

	for idx, serialType := range payload.SerialTypes {
		valueSize := 0
		if idx < len(payload.Columns) {
			valueSize = payload.Columns[idx].Meta.Size
		}
		children = appendDrillChild(children, drillChild{
			kind:        drillChildSerialType,
			title:       fmt.Sprintf("Serial Type %d", idx+1),
			parentTitle: "Record Payload",
			meta:        serialType.Meta,
			parsed: []string{
				fmt.Sprintf("Serial type: %d", serialType.Value),
				"Storage class: " + storageClassLabel(serialType.Value),
				fmt.Sprintf("Value size: %s", byteCount(valueSize)),
				fmt.Sprintf("Value block: Value %d", idx+1),
			},
		})
	}

	for idx, column := range payload.Columns {
		children = appendDrillChild(children, drillChild{
			kind:        drillChildRecordValue,
			title:       fmt.Sprintf("Value %d", idx+1),
			parentTitle: "Record Payload",
			meta:        column.Meta,
			parsed: []string{
				"Storage class: " + storageClassLabel(column.SerialType),
				fmt.Sprintf("Serial type: %d", column.SerialType),
				"Value: " + recordValueLabel(column.Value),
			},
		})
	}

	return children
}

func appendDrillChild(children []drillChild, child drillChild) []drillChild {
	if !child.meta.Valid() || child.meta.Size <= 0 {
		return children
	}
	return append(children, child)
}

func drillChildMetaLines(child drillChild) []string {
	lines := []string{
		child.title,
		"Parent: " + child.parentTitle,
		"Offset: " + offsetRange(child.meta),
		"Size: " + byteCount(child.meta.Size),
		"",
		sectionStyle.Render("PARSED"),
	}
	if len(child.parsed) == 0 {
		return append(lines, "Parsed structure: none")
	}
	return append(lines, child.parsed...)
}

func storageClassLabel(serialType uint64) string {
	switch serialType {
	case 0:
		return "null"
	case 1, 2, 3, 4, 5, 6:
		return "int"
	case 7:
		return "float"
	case 8:
		return "const-0"
	case 9:
		return "const-1"
	default:
		if serialType%2 == 1 {
			return "text"
		}
		return "blob"
	}
}

func byteCount(size int) string {
	if size == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", size)
}
