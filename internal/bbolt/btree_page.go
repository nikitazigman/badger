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
	ID       PageID
	Flags    FlagType
	Count    uint16
	Overflow uint32
}

const (
	Magic   = 0xED0CDAED
	Version = 0x02
)

type MetaPayload struct {
	Meta          Meta
	Magic         uint32
	Version       uint32
	PageSize      uint32
	Flags         uint32
	Root          PageID
	Sequence      uint64
	FreeList      PageID
	PageID        PageID
	TransactionID uint64
	CheckSum      uint64 // FNV-1 checksum
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
