package tiff

import "math"

func putF32(f *float32, bits uint32) { *f = math.Float32frombits(bits) }
func f64from(bits uint64) float64   { return math.Float64frombits(bits) }
