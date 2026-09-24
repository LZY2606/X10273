package store

import (
	"bytes"
	"io"
)

// zip needs ReaderAt; import buffers uploads locally (local-first, no network).
func readerAt(r io.Reader) io.ReaderAt {
	if ra, ok := r.(io.ReaderAt); ok {
		if sr, ok := r.(interface {
			Seek(int64, int) (int64, error)
		}); ok {
			sr.Seek(0, io.SeekStart)
		}
		return ra
	}
	b, _ := io.ReadAll(r)
	return bytes.NewReader(b)
}

func sizeOf(r io.Reader) int64 {
	switch v := r.(type) {
	case *bytes.Reader:
		return v.Size()
	case interface{ Len() int }:
		return int64(v.Len())
	default:
		b, _ := io.ReadAll(r)
		return int64(len(b))
	}
}
