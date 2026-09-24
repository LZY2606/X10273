package store

import (
	"database/sql"
	"encoding/json"
	"ifdlantern/internal/tiff"
)

// derive parses one blob and records all parser facts. Facts are
// content-addressed; a replacement blob writes new facts for the changed
// graph while unchanged nodes keep identical ids.
func (s *Store) derive(tx *sql.Tx, project, branch, label, blobID string,
	rule RuleVersion, birthSeq int) (*tiff.ParseReport, error) {
	data, region, wideStart, _, err := s.blobRowsQ(tx, blobID)
	if err != nil {
		return nil, err
	}
	var src tiff.Source
	if len(region) > 0 {
		src = &tiff.WideSource{Normal: data, RegionStart: wideStart, Region: region, IDv: blobID}
	} else {
		src = &tiff.BytesSource{B: data, IDv: blobID}
	}
	cfg := tiff.Config{Budget: rule.Budget}
	rep, err := tiff.Parse(src, cfg)
	if err != nil {
		// A format error is itself evidence; record a parse-error fact.
		pe := map[string]any{"error": err.Error(), "source": blobID}
		raw, _ := json.Marshal(pe)
		_ = insertFact(tx, project, branch, rule.Scope, label, "parse-error",
			"pe-"+short(blobID), string(raw), blobID, rule.Fingerprint, birthSeq)
		return nil, nil
	}
	if err := s.recordReport(tx, project, branch, label, blobID, rule, birthSeq, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

func (s *Store) recordReport(tx *sql.Tx, project, branch, label, blobID string,
	rule RuleVersion, birthSeq int, rep *tiff.ParseReport) error {
	write := func(kind, key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return insertFact(tx, project, branch, rule.Scope, label, kind, key,
			string(raw), blobID, rule.Fingerprint, birthSeq)
	}
	if err := write("header", "header", rep.Header); err != nil {
		return err
	}
	for _, node := range rep.IFDs {
		if err := write("ifd", node.ID, node); err != nil {
			return err
		}
		if l, ok := rep.Layouts[node.ID]; ok {
			if err := write("blocks", node.ID, l); err != nil {
				return err
			}
		}
		if g, ok := rep.Geos[node.ID]; ok {
			if err := write("geo", node.ID, g); err != nil {
				return err
			}
		}
	}
	if len(rep.Diags) > 0 || rep.Budget.Exhausted {
		if err := write("parse-diagnostics", "report", map[string]any{
			"diags": rep.Diags, "budget": rep.Budget,
		}); err != nil {
			return err
		}
	}
	return nil
}

func insertFact(tx *sql.Tx, project, branch, scope, label, kind, key, data, blob, fp string,
	birthSeq int) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO facts
		 (project,branch,scope,label,kind,key_id,data,source_blob,rule_fp,birth_seq)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		project, branch, scope, label, kind, key, data, blob, fp, birthSeq)
	return err
}

func short(s string) string {
	if len(s) > 16 {
		return s[len(s)-16:]
	}
	return s
}
