package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"tiff-ifd-evidence/internal/tiffparse"
)

const AlgorithmVersion = "ifd-room-rules-1.0.0"

type SourceInput struct {
	ID string
	EventID string
	Label string
	Bytes []byte
	ParentID string
	Role string
}

func HashParameters(params Parameters) string {
	data, _ := json.Marshal(params)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

func BuildAnalysis(projectID string, version int, parameterRevision int, params Parameters, inputs []SourceInput) (*Analysis, error) {
	sourceSet := make([]string, 0, len(inputs))
	for _, input := range inputs { sourceSet = append(sourceSet, input.ID) }
	sort.Strings(sourceSet)
	analysis := &Analysis{
		ID: fmt.Sprintf("%s/analysis@v%d", projectID, version), ProjectID: projectID,
		Version: version, ParameterRevision: parameterRevision, SourceSet: sourceSet,
		AlgorithmVersion: AlgorithmVersion, ParameterHash: HashParameters(params),
		Status: "current", Current: true,
	}
	for _, input := range inputs {
		result, err := tiffparse.Parse(input.Bytes, tiffparse.Options{SourceID: input.ID, NodeLimit: params.NodeLimit, ExternalLimit: params.ExternalLimit})
		if err != nil {
			analysis.Evidence = append(analysis.Evidence, Evidence{Code: "format_error", Severity: "error", Message: fmt.Sprintf("%s: %v", input.Label, err), ObjectID: input.ID})
			continue
		}
		analysis.Endian = append(analysis.Endian, string(result.Endian))
		analysis.Formats = append(analysis.Formats, string(result.Format))
		prov := Provenance{AlgorithmVersion: AlgorithmVersion, ParameterHash: analysis.ParameterHash, DirectSources: []string{input.ID}, SourceEventID: input.EventID}
		addParseViews(analysis, result, prov)
		addBlocks(analysis, result, prov, input)
		addGeoKeysAndTransforms(analysis, result, prov)
		analysis.Budgets = append(analysis.Budgets, BudgetView{
			SourceID: input.ID, NodeLimit: result.Budget.NodeLimit, NodesVisited: result.Budget.NodesVisited,
			ExternalLimit: result.Budget.ExternalLimit, ExternalReads: result.Budget.ExternalReads,
			Exhausted: result.Budget.Exhausted, Kind: result.Budget.ExhaustedKind, AtLink: result.Budget.ExhaustedAtLink,
		})
		if result.Budget.Exhausted { analysis.Evidence = append(analysis.Evidence, Evidence{Code: "budget_exhausted", Severity: "error", Message: "IFD access budget was exhausted; traversal stopped explicitly", ObjectID: input.ID}) }
	}
	return analysis, nil
}

func addParseViews(analysis *Analysis, result *tiffparse.Result, prov Provenance) {
	shared, cycle, blocked := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, link := range result.Links {
		if link.Shared { shared[link.To] = true }
		if link.Cycle { cycle[link.To] = true }
		if link.BlockedByBudget { blocked[link.To] = true }
	}
	for _, node := range result.IFDs {
		view := IFDView{ID: node.ID, Offset: node.Offset, Kind: node.Kind, Bytes: ByteRange(node.Bytes), EntryCount: node.EntryCount, NextIFD: node.NextIFD, SubIFDs: node.SubIFDs, Raw: map[string]interface{}{"endian": string(result.Endian), "format": string(result.Format), "first_seen_from": node.FirstSeenFrom}, Shared: shared[node.ID], Cycle: cycle[node.ID], Blocked: blocked[node.ID], Provenance: prov}
		entrySize := uint64(14); if result.Format == tiffparse.BigTIFF { entrySize = 20 }
		countSize := uint64(2); if result.Format == tiffparse.BigTIFF { countSize = 8 }
		for index, tag := range node.Tags {
			entryOffset := node.Offset + countSize + uint64(index)*entrySize
			tr := TagByteRange{TagID: tag.ID, Name: tag.Name, Entry: ByteRange{Offset: entryOffset, Length: entrySize}, Inline: tag.Inline, Truncated: tag.Truncated}
			if !tag.Inline && tag.ExternalLength > 0 { tr.Value = &ByteRange{Offset: tag.ExternalOffset, Length: tag.ExternalLength} }
			view.TagRanges = append(view.TagRanges, tr)
		}
		for _, diag := range result.Diagnostics { if diag.IFDID == node.ID { view.Diagnostics = append(view.Diagnostics, diag.Message) } }
		analysis.IFDs = append(analysis.IFDs, view)
	}
	for _, link := range result.Links { analysis.Links = append(analysis.Links, LinkView{ID: link.ID, From: link.From, To: link.To, Kind: link.Kind, Offset: link.Offset, TagID: link.TagID, Index: link.Index, Shared: link.Shared, Cycle: link.Cycle, Blocked: link.BlockedByBudget}) }
	for _, diag := range result.Diagnostics {
		var br *ByteRange
		if diag.Length > 0 { r := ByteRange{Offset: diag.Offset, Length: diag.Length}; br = &r }
		analysis.Evidence = append(analysis.Evidence, Evidence{Code: diag.Code, Severity: diag.Severity, Message: diag.Message, ObjectID: firstNonEmpty(diag.IFDID, result.SourceID), ByteRange: br})
	}
}

func firstNonEmpty(values ...string) string { for _, value := range values { if value != "" { return value } }; return "" }
