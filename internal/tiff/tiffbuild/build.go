// Package tiffbuild 以确定性方式构造 TIFF/BigTIFF 字节,
// 供测试与离线案例包使用。支持显式偏移以构造循环、共享子目录、
// tile 重叠等边界形态。
package tiffbuild

import (
	"encoding/binary"
	"math"
	"sort"

	"tiffgraph/internal/tiff"
)

// E 是一个 IFD 条目。
type E struct {
	Tag           uint16
	Type          uint16
	Values        []uint64
	Doubles       []float64
	Text          string
	Raw           []byte // 直接给值字节,优先于 Values/Doubles/Text
	ForceExternal bool   // 即使可内联也放外部值区
	At            *uint64 // 外部值的显式偏移
}

// IFD 是一个目录。
type IFD struct {
	Entries []E
	Next    *uint64 // 显式 next 偏移;nil 表示链到列表中的下一个
	At      *uint64 // 显式 IFD 偏移
}

// File 描述一个待构造的 TIFF 文件。
type File struct {
	Big     bool
	Order   tiff.ByteOrder
	IFDs    []*IFD
	Tail    map[uint64][]byte // 显式偏移处的原始字节(如 tile 数据)
	MinSize int
}

func u16(v int) *uint64 { x := uint64(v); return &x }

// Short 构造 SHORT 条目。
func Short(tag uint16, vals ...uint16) E {
	u := make([]uint64, len(vals))
	for i, v := range vals {
		u[i] = uint64(v)
	}
	return E{Tag: tag, Type: 3, Values: u}
}

// Long 构造 LONG 条目。
func Long(tag uint16, vals ...uint32) E {
	u := make([]uint64, len(vals))
	for i, v := range vals {
		u[i] = uint64(v)
	}
	return E{Tag: tag, Type: 4, Values: u}
}

// Double 构造 DOUBLE 条目。
func Double(tag uint16, vals ...float64) E { return E{Tag: tag, Type: 12, Doubles: vals} }

// Ascii 构造 ASCII 条目。
func Ascii(tag uint16, s string) E { return E{Tag: tag, Type: 2, Text: s} }

func (f *File) order() binary.ByteOrder {
	if f.Order == tiff.BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func typeSize(t uint16) uint64 {
	switch t {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11:
		return 4
	case 5, 10, 12, 16, 17, 18:
		return 8
	}
	return 1
}

func (f *File) valueBytes(e E) []byte {
	if e.Raw != nil {
		return e.Raw
	}
	o := f.order()
	if e.Text != "" || e.Type == 2 {
		return append([]byte(e.Text), 0)
	}
	if e.Doubles != nil {
		b := make([]byte, 8*len(e.Doubles))
		for i, v := range e.Doubles {
			o.PutUint64(b[8*i:], math.Float64bits(v))
		}
		return b
	}
	sz := typeSize(e.Type)
	b := make([]byte, sz*uint64(len(e.Values)))
	for i, v := range e.Values {
		switch sz {
		case 1:
			b[i] = byte(v)
		case 2:
			o.PutUint16(b[2*i:], uint16(v))
		case 4:
			o.PutUint32(b[4*i:], uint32(v))
		case 8:
			o.PutUint64(b[8*i:], v)
		}
	}
	return b
}

// Build 渲染文件字节。布局确定性:头部、IFD 区、外部值区、Tail 段。
func Build(f *File) []byte {
	o := f.order()
	entrySize := uint64(12)
	countSize := uint64(2)
	nextSize := uint64(4)
	headerSize := uint64(8)
	inlineCap := uint64(4)
	if f.Big {
		entrySize, countSize, nextSize, headerSize, inlineCap = 20, 8, 8, 16, 8
	}
	// 1. 分配 IFD 偏移
	ifdOff := make([]uint64, len(f.IFDs))
	cursor := headerSize
	for i, d := range f.IFDs {
		if d.At != nil {
			ifdOff[i] = *d.At
		} else {
			ifdOff[i] = cursor
		}
		end := ifdOff[i] + countSize + uint64(len(d.Entries))*entrySize + nextSize
		if end > cursor {
			cursor = end
		}
	}
	// 2. 分配外部值偏移
	type ext struct {
		data []byte
		at   uint64
	}
	exts := map[int]map[int]ext{} // ifd idx -> entry idx -> ext
	for i, d := range f.IFDs {
		for j, e := range d.Entries {
			vb := f.valueBytes(e)
			if uint64(len(vb)) <= inlineCap && !e.ForceExternal && e.At == nil {
				continue
			}
			at := cursor
			if e.At != nil {
				at = *e.At
			} else {
				cursor += uint64(len(vb))
				if cursor%2 == 1 {
					cursor++
				}
			}
			if exts[i] == nil {
				exts[i] = map[int]ext{}
			}
			exts[i][j] = ext{vb, at}
		}
	}
	// 3. 计算文件大小
	size := cursor
	for off, b := range f.Tail {
		if off+uint64(len(b)) > size {
			size = off + uint64(len(b))
		}
	}
	if uint64(f.MinSize) > size {
		size = uint64(f.MinSize)
	}
	buf := make([]byte, size)
	// 4. 头部
	if f.Order == tiff.BigEndian {
		copy(buf, "MM")
	} else {
		copy(buf, "II")
	}
	if f.Big {
		o.PutUint16(buf[2:], 43)
		o.PutUint16(buf[4:], 8)
		o.PutUint16(buf[6:], 0)
		o.PutUint64(buf[8:], ifdOff[0])
	} else {
		o.PutUint16(buf[2:], 42)
		o.PutUint32(buf[4:], uint32(ifdOff[0]))
	}
	// 5. IFD 区
	for i, d := range f.IFDs {
		base := ifdOff[i]
		if f.Big {
			o.PutUint64(buf[base:], uint64(len(d.Entries)))
		} else {
			o.PutUint16(buf[base:], uint16(len(d.Entries)))
		}
		ep := base + countSize
		for j, e := range d.Entries {
			vb := f.valueBytes(e)
			o.PutUint16(buf[ep:], e.Tag)
			o.PutUint16(buf[ep+2:], e.Type)
			var count uint64
			if e.Raw != nil {
				count = uint64(len(vb)) / typeSize(e.Type)
			} else if e.Doubles != nil {
				count = uint64(len(e.Doubles))
			} else if e.Text != "" || e.Type == 2 {
				count = uint64(len(vb))
			} else {
				count = uint64(len(e.Values))
			}
			var vf uint64
			if x, ok := exts[i][j]; ok {
				vf = x.at
				copy(buf[x.at:], x.data)
			}
			if f.Big {
				o.PutUint64(buf[ep+4:], count)
				if _, ok := exts[i][j]; ok {
					o.PutUint64(buf[ep+12:], vf)
				} else {
					copy(buf[ep+12:], vb)
				}
			} else {
				o.PutUint32(buf[ep+4:], uint32(count))
				if _, ok := exts[i][j]; ok {
					o.PutUint32(buf[ep+8:], uint32(vf))
				} else {
					copy(buf[ep+8:], vb)
				}
			}
			ep += entrySize
		}
		// next 指针
		var next uint64
		if d.Next != nil {
			next = *d.Next
		} else if i+1 < len(f.IFDs) {
			next = ifdOff[i+1]
		}
		if f.Big {
			o.PutUint64(buf[ep:], next)
		} else {
			o.PutUint32(buf[ep:], uint32(next))
		}
	}
	// 6. Tail 段
	for off, b := range f.Tail {
		copy(buf[off:], b)
	}
	return buf
}

// Ptr 返回 uint64 指针,便于显式偏移。
func Ptr(v uint64) *uint64 { return &v }

// SortedOffsets 返回 Tail 的排序偏移(调试用)。
func SortedOffsets(t map[uint64][]byte) []uint64 {
	var out []uint64
	for k := range t {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
