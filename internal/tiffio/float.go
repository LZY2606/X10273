package tiffio

import "math"

func float64From32(bits uint32) float64 { return float64(math.Float32frombits(bits)) }
func float64From64(bits uint64) float64 { return math.Float64frombits(bits) }
