package tiffio

import (
	"encoding/binary"
	"fmt"
)

// Reader performs bounded, order-aware reads against an in-memory TIFF blob.
// Every read is counted against the access budget so that crafted files
// cannot force unbounded work even before an IFD cycle is detected.
type Reader struct {
	data  []byte
	order Order
	big   bool
	bud   *Budget
}

// NewReader wraps data. The order and classic/BigTIFF width may be unknown at
// construction time; the header parse clones the reader once both are known.
func NewReader(data []byte, order Order, bigTIFF bool, bud *Budget) *Reader {
	if bud == nil {
		bud = DefaultBudget()
	}
	return &Reader{data: data, order: order, big: bigTIFF, bud: bud}
}

// Len is the total byte length of the stream.
func (r *Reader) Len() int { return len(r.data) }

// IsBigTIFF reports the parsed header variant.
func (r *Reader) IsBigTIFF() bool { return r.big }

// Order reports the parsed byte order.
func (r *Reader) Order() Order { return r.order }

// Budget exposes the live access counter.
func (r *Reader) Budget() *Budget { return r.bud }

// ErrOutOfBounds marks a read that escapes the stream. Parse structures keep
// going where possible, recording this as evidence rather than failing the
// whole import.
type ErrOutOfBounds struct {
	Offset int64
	Want   int
	Size   int
}

func (e *ErrOutOfBounds) Error() string {
	return fmt.Sprintf("tiff: read of %d bytes at offset %d exceeds stream length %d", e.Want, e.Offset, e.Size)
}

// ErrBudget marks budget exhaustion. Traversal must stop after recording it.
type ErrBudget struct {
	Kind   string
	Used   int64
	Limit  int64
	Offset int64
}

func (e *ErrBudget) Error() string {
	return fmt.Sprintf("tiff: access budget exhausted (%s used %d / limit %d at offset %d)", e.Kind, e.Used, e.Limit, e.Offset)
}

func (r *Reader) sliceAt(off int64, n int) ([]byte, error) {
	if err := r.bud.ChargeBytes(int64(n), off); err != nil {
		return nil, err
	}
	if off < 0 || n < 0 || int64(len(r.data)) < off+int64(n) {
		return nil, &ErrOutOfBounds{Offset: off, Want: n, Size: len(r.data)}
	}
	return r.data[off : off+int64(n)], nil
}

// Bytes returns a copy-free view; callers must not mutate.
func (r *Reader) Bytes(off int64, n int) ([]byte, error) { return r.sliceAt(off, n) }

func (r *Reader) u16(off int64) (uint16, error) {
	b, err := r.sliceAt(off, 2)
	if err != nil {
		return 0, err
	}
	return r.order.bo().Uint16(b), nil
}

func (r *Reader) u32(off int64) (uint32, error) {
	b, err := r.sliceAt(off, 4)
	if err != nil {
		return 0, err
	}
	return r.order.bo().Uint32(b), nil
}

func (r *Reader) u64(off int64) (uint64, error) {
	b, err := r.sliceAt(off, 8)
	if err != nil {
		return 0, err
	}
	return r.order.bo().Uint64(b), nil
}

// Offset reads an IFD-width offset: 32 bits classic, 64 bits BigTIFF.
func (r *Reader) Offset(off int64) (int64, error) {
	if r.big {
		v, err := r.u64(off)
		return int64(v), err
	}
	v, err := r.u32(off)
	return int64(v), err
}

// Decode decodes one element of the given type from b, whose length must be at
// least FieldType.Size. Signed and rational values are returned as int64,
// uint64, float64 or [2]int64 numerator/denominator pairs.
func Decode(t FieldType, order Order, b []byte) (any, error) {
	bo := order.bo()
	if len(b) < t.Size() {
		return nil, fmt.Errorf("tiff: short field value for %s", t)
	}
	switch t {
	case TypeByte, TypeASCII, TypeUndefined:
		return b[0], nil
	case TypeSByte:
		return int8(b[0]), nil
	case TypeShort:
		return bo.Uint16(b), nil
	case TypeSShort:
		return int16(bo.Uint16(b)), nil
	case TypeLong, TypeIFD:
		return bo.Uint32(b), nil
	case TypeSLong:
		return int32(bo.Uint32(b)), nil
	case TypeFloat:
		return float64(mathFloat32(bo, b)), nil
	case TypeDouble:
		return mathFloat64(bo, b), nil
	case TypeLong8, TypeIFD8:
		return bo.Uint64(b), nil
	case TypeSLong8:
		return int64(bo.Uint64(b)), nil
	case TypeRational:
		return [2]uint64{bo.Uint64(b[:8]), bo.Uint64(b[8:16])}, nil
	case TypeSRational:
		return [2]int64{int64(bo.Uint64(b[:8])), int64(bo.Uint64(b[8:16]))}, nil
	}
	return nil, fmt.Errorf("tiff: unsupported field type %s", t)
}

func mathFloat32(bo binary.ByteOrder, b []byte) float64 { return float64From32(bo.Uint32(b)) }
func mathFloat64(bo binary.ByteOrder, b []byte) float64 { return float64From64(bo.Uint64(b)) }
