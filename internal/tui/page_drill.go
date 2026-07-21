package tui

import (
	"github.com/nikitazigman/badger/internal/storage"
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

const (
	drillChildPayloadSize      = "payload_size"
	drillChildRowID            = "rowid"
	drillChildLeftChildPage    = "left_child_page"
	drillChildRecordPayload    = "record_payload"
	drillChildRecordHeaderSize = "record_header_size"
	drillChildSerialType       = "serial_type"
	drillChildRecordValue      = "record_value"
	drillChildOverflowPointer  = "overflow_pointer"
	drillChildBranchEntry      = "branch_entry"
	drillChildBranchDescriptor = "branch_descriptor"
	drillChildLeafEntry        = "leaf_entry"
	drillChildLeafDescriptor   = "leaf_descriptor"
	drillChildLeafKey          = "leaf_key"
	drillChildLeafValue        = "leaf_value"
	drillChildDescriptorFlags  = "descriptor_flags"
	drillChildDescriptorPos    = "descriptor_position"
	drillChildDescriptorKeySz  = "descriptor_key_size"
	drillChildDescriptorValSz  = "descriptor_value_size"
	drillChildDescriptorChild  = "descriptor_child_page"
	drillChildBucketRootPage   = "bucket_root_page"
	drillChildBucketSequence   = "bucket_sequence"
)

type drillChild struct {
	kind     string
	title    string
	meta     storage.ByteSpan
	rows     []storage.Field
	children []drillChild
}

func buildDrillChildren(block pageBlock, page *storage.PageInspection) []drillChild {
	if page == nil {
		return nil
	}
	return append([]drillChild(nil), block.children...)
}

func drillChildFromStorage(block storage.HexBlock) drillChild {
	children := make([]drillChild, 0, len(block.Children))
	for _, child := range block.Children {
		children = append(children, drillChildFromStorage(child))
	}
	return drillChild{
		kind:     block.Kind,
		title:    block.Title,
		meta:     block.Span,
		rows:     block.Rows,
		children: children,
	}
}

func drillChildMetaLines(child drillChild) []string {
	if len(child.rows) == 0 {
		return []string{
			child.title,
			"Offset: " + spanRange(child.meta),
		}
	}
	return fieldLines(child.rows)
}
