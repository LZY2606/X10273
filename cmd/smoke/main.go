package main

import (
	"fmt"

	"ifdgraph/internal/fixture"
	"ifdgraph/internal/tiff"
)

func main() {
	for _, name := range fixture.AllSampleNames() {
		data, err := fixture.Samples(name)
		if err != nil {
			panic(err)
		}
		g := tiff.Parse("src", 1, data, tiff.Params{StepBudget: 4096})
		fmt.Printf("%-28s size=%-8d bo=%-2s ver=%-7s ifds=%d tags=%d blocks=%d geo=%d diags=%d exhausted=%v",
			name, g.FileSize, string(g.ByteOrder), g.Version, len(g.IFDs), len(g.Tags), len(g.Blocks), len(g.Geo), len(g.Diags), g.BudgetExhausted)
		if g.Fatal != "" {
			fmt.Printf(" FATAL=%s", g.Fatal)
		}
		cycles, shared := 0, 0
		for _, ifd := range g.IFDs {
			if ifd.Cycle {
				cycles++
			}
			if ifd.Shared {
				shared++
			}
		}
		st := map[string]int{}
		for _, b := range g.Blocks {
			st[string(b.Status)]++
		}
		fmt.Printf(" cycles=%d shared=%d status=%v\n", cycles, shared, st)
	}
	data, _ := fixture.Samples(fixture.SampleBudgetChain)
	g := tiff.Parse("src", 1, data, tiff.Params{StepBudget: 30})
	fmt.Printf("low-budget chain: ifds=%d exhausted=%v used=%d diags=%d\n", len(g.IFDs), g.BudgetExhausted, g.StepsUsed, len(g.Diags))
}
