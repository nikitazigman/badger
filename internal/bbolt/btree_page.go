package bbolt

type BTreePage struct {
	Raw    []byte
	Header PageHeader

	MetaPayload *MetaPayload

	BranchPayload *BranchPayload

	LeafPayload *LeafPayload

	FreelistPayload *FreelistPayload
}

type Meta struct {
	StartOffset int
	Size        int
}

func (m Meta) Valid() bool {
	return m.StartOffset >= 0 && m.Size >= 0
}

func (m Meta) EndOffset() int {
	return m.StartOffset + m.Size
}

func metaFromOffset(start int, size int) Meta {
	return Meta{StartOffset: start, Size: size}
}

type Uint16Field struct {
	Meta  Meta
	Value uint16
}

type Uint32Field struct {
	Meta  Meta
	Value uint32
}

type Uint64Field struct {
	Meta  Meta
	Value uint64
}

type PageIDField struct {
	Meta  Meta
	Value PageID
}

type FlagField struct {
	Meta  Meta
	Value FlagType
}

type FlagType uint16
type PageID uint64

const (
	BranchPageFlag   FlagType = 0x01
	LeafPageFlag     FlagType = 0x02
	MetaPageFlag     FlagType = 0x04
	FreelistPageFlag FlagType = 0x10
)

type PageHeader struct {
	Meta     Meta
	ID       PageIDField
	Flags    FlagField
	Count    Uint16Field
	Overflow Uint32Field
}

const (
	Magic   = 0xED0CDAED
	Version = 0x02
)

type MetaPayload struct {
	Meta          Meta
	Magic         Uint32Field
	Version       Uint32Field
	PageSize      Uint32Field
	Flags         Uint32Field
	Root          PageIDField
	Sequence      Uint64Field
	FreeList      PageIDField
	PageID        PageIDField
	TransactionID Uint64Field
	CheckSum      Uint64Field // FNV-1 checksum
}

type BranchPayload struct {
	Meta           Meta
	BranchElements []BranchElement
	KeyValue       []KeyValue
}

type BranchElement struct {
	Meta    Meta
	Pos     uint32
	KeySize uint32
	PageID  PageID
}

type KeyValue struct {
	Meta  Meta
	Key   Payload
	Value Payload
}

type Payload struct {
	Data []byte
}

// TODO: how to represent inline page
type LeafPayload struct {
	Meta         Meta
	LeafElements []LeafElement
	KeyValue     []KeyValue
	NestedBucket []NestedBucket
	InlineBucket *InlineBucket
}

type LeafFlagType uint32

const (
	OrdinaryKeyValueFlag LeafFlagType = 0x00
	NestedBucketFlag     LeafFlagType = 0x01
)

type LeafElement struct {
	Meta      Meta
	Flags     LeafFlagType
	Pos       uint32
	KeySize   uint32
	ValueSize uint32
}

type NestedBucket struct {
	Meta     Meta
	Root     PageID // 0 for inline bucket
	Sequence uint64
}

type InlineBucket struct {
	Meta         Meta
	Header       PageHeader
	LeafElements []LeafElement
	KeyValue     []KeyValue
}

type FreelistPayload struct {
	Meta         Meta
	actual_count *uint64
	IDs          []PageID
}
