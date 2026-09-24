package store

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"ifdgraph/internal/clock"
	"ifdgraph/internal/derive"
	"ifdgraph/internal/model"
	"ifdgraph/internal/tiff"
)

// ArchiveFormat is the export envelope version.
const ArchiveFormat = "ifdgraph-archive-1"

// RevisionBlob is one exported immutable input.
type RevisionBlob struct {
	Revision Revision `json:"revision"`
	DataB64  []byte   `json:"data"` // raw bytes
}

// Archive is the portable counterexample/case package.
type Archive struct {
	Format     string           `json:"format"`
	Project    Project          `json:"project"`
	Revisions  []RevisionBlob   `json:"revisions"`
	Events     []*Event         `json:"events"`
	Derived    []DerivationBlob `json:"derived"`
	ExportedAt string           `json:"exportedAt"`
}

// DerivationBlob is exported so an importer can verify that recomputation
// reproduces identical identities, ordering and provenance.
type DerivationBlob struct {
	Derivation
	GraphJSON      []byte `json:"graphJson"`
	TransformsJSON []byte `json:"transformsJson"`
}

// Export writes the complete project evidence as a zip archive containing
// manifest.json plus raw revision bytes.
func (s *Store) Export(ctx context.Context, c *clock.Clock, projectID string) ([]byte, error) {
	proj, err := s.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	revs, err := s.ListRevisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	arch := Archive{
		Format: ArchiveFormat, Project: *proj, Events: events,
		ExportedAt: c.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, r := range revs {
		_, data, err := s.GetRevisionData(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		arch.Revisions = append(arch.Revisions, RevisionBlob{Revision: *r, DataB64: data})
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,event_id,source_id,parse_fp,transform_fp,graph_json,transforms_json,created_at
		 FROM derivations WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Derivation
		var gj, tj []byte
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.EventID, &d.SourceID, &d.ParseFP,
			&d.XformFP, &gj, &tj, &d.CreatedAt); err != nil {
			return nil, err
		}
		arch.Derived = append(arch.Derived, DerivationBlob{Derivation: d, GraphJSON: gj, TransformsJSON: tj})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mw, err := zw.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	manifest, err := json.MarshalIndent(arch, "", "  ")
	if err != nil {
		return nil, err
	}
	if _, err := mw.Write(manifest); err != nil {
		return nil, err
	}
	for _, rb := range arch.Revisions {
		fw, err := zw.Create("sources/" + rb.Revision.SourceID + ".bin")
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(rb.DataB64); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportReport lists verification results during import.
type ImportReport struct {
	ProjectID         string   `json:"projectId"`
	Revisions         int      `json:"revisions"`
	Events            int      `json:"events"`
	RecomputedMatches int      `json:"recomputedMatches"`
	Errors            []string `json:"errors"`
}

// Import reads an exported archive into a fresh project. Raw inputs are
// inserted verbatim; derived outputs are recomputed from the recorded event
// sequence (parameter revisions included) and must reproduce the archived
// identities bit-for-bit.
func (s *Store) Import(ctx context.Context, c *clock.Clock, r io.Reader) (*Archive, *ImportReport, error) {
	zr, err := zip.NewReader(readerAt(r), sizeOf(r))
	if err != nil {
		return nil, nil, fmt.Errorf("open archive: %w", err)
	}
	var arch Archive
	var dataBySource = map[string][]byte{}
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			if err := json.NewDecoder(rc).Decode(&arch); err != nil {
				rc.Close()
				return nil, nil, err
			}
			rc.Close()
		}
	}
	if arch.Format != ArchiveFormat {
		return nil, nil, fmt.Errorf("unsupported archive format %q", arch.Format)
	}
	for _, f := range zr.File {
		if len(f.Name) > 8 && f.Name[:8] == "sources/" {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, nil, err
			}
			key := f.Name[8 : len(f.Name)-4]
			dataBySource[key] = b
		}
	}
	// Prefer the raw bytes packed in the manifest when present.
	for i := range arch.Revisions {
		if len(arch.Revisions[i].DataB64) > 0 {
			dataBySource[arch.Revisions[i].Revision.SourceID] = arch.Revisions[i].DataB64
		}
	}

	report := &ImportReport{ProjectID: arch.Project.ID, Revisions: len(arch.Revisions)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,created_at) VALUES(?,?,?)`,
		arch.Project.ID, arch.Project.Name, arch.Project.CreatedAt); err != nil {
		return nil, nil, err
	}
	// Verify and insert immutable revisions in declared order.
	for _, rb := range arch.Revisions {
		data := dataBySource[rb.Revision.SourceID]
		if data == nil {
			report.Errors = append(report.Errors, "missing source bytes for "+rb.Revision.SourceID)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != rb.Revision.SHA256 {
			report.Errors = append(report.Errors, "sha256 mismatch on revision "+rb.Revision.ID)
			continue
		}
		wantSource := model.SourceID(arch.Project.ID, rb.Revision.Rev, data)
		if wantSource != rb.Revision.SourceID {
			report.Errors = append(report.Errors, "source identity mismatch on revision "+rb.Revision.ID)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO revisions(id,project_id,rev,parent_revision_id,source_id,filename,note,sha256,size,data,created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			rb.Revision.ID, rb.Revision.ProjectID, rb.Revision.Rev, nullStr(rb.Revision.ParentRevision),
			rb.Revision.SourceID, rb.Revision.Filename, rb.Revision.Note, rb.Revision.SHA256,
			rb.Revision.Size, data, rb.Revision.CreatedAt); err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
	}
	// Replay events in recorded order, verifying the content hash chain.
	var parent string
	for _, e := range arch.Events {
		want := model.EventID(e.ProjectID, e.Seq, e.Type, []byte(e.Payload), parent)
		if want != e.ID {
			report.Errors = append(report.Errors, fmt.Sprintf("event identity/chain mismatch at seq %d", e.Seq))
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO events(id,project_id,seq,parent_id,type,subject_id,payload,created_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			e.ID, e.ProjectID, e.Seq, nullStr(e.ParentID), e.Type, e.SubjectID, []byte(e.Payload), e.CreatedAt); err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
		if e.Type == "decision" {
			var p DecisionPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				tx.ExecContext(ctx,
					`INSERT OR IGNORE INTO decisions(id,project_id,event_id,object_id,action,rationale,created_at)
					 VALUES(?,?,?,?,?,?,?)`, e.ID, e.ProjectID, e.ID, p.ObjectID, p.Action, p.Rationale, e.CreatedAt)
			}
		}
		parent = e.ID
	}
	report.Events = len(arch.Events)
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	// Recompute derivations by replaying parameter revisions + import order.
	params := DefaultParams()
	revBySource := map[string]Revision{}
	for _, rb := range arch.Revisions {
		revBySource[rb.Revision.SourceID] = rb.Revision
	}
	archived := map[string]DerivationBlob{}
	for _, d := range arch.Derived {
		archived[d.ID] = d
	}
	for _, e := range arch.Events {
		switch e.Type {
		case "param_revision":
			var p ParamPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				params = Params{ParseBudget: p.ParseBudget, RequireDeclared: p.RequireDeclared}
			}
		case "derived":
			var p DerivedPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				report.Errors = append(report.Errors, err.Error())
				continue
			}
			rev := revBySource[p.SourceID]
			data := dataBySource[p.SourceID]
			pp := tiff.Params{StepBudget: params.ParseBudget}
			graph := tiff.Parse(p.SourceID, rev.Rev, data, pp)
			gotGraphID := model.GraphID(p.SourceID, tiff.AlgoVersion(), pp.Fingerprint())
			tp := derive.TransformParams{RequireDeclaredCRS: params.RequireDeclared}
			xforms := derive.DeriveTransforms(graph, tp)
			gj, _ := json.Marshal(graph)
			tj, _ := json.Marshal(xforms)
			want, ok := archived[gotGraphID]
			if !ok {
				report.Errors = append(report.Errors, "recomputed derivation identity not present in archive: "+gotGraphID)
			} else if bytes.Equal(want.GraphJSON, gj) && bytes.Equal(want.TransformsJSON, tj) {
				report.RecomputedMatches++
			} else {
				report.Errors = append(report.Errors, "recomputed derivation content mismatch: "+gotGraphID)
			}
			s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO derivations(id,project_id,event_id,source_id,parse_fp,transform_fp,graph_json,transforms_json,created_at)
				 VALUES(?,?,?,?,?,?,?,?,?)`,
				gotGraphID, arch.Project.ID, e.ID, p.SourceID, pp.Fingerprint(), tp.Fingerprint(), gj, tj, e.CreatedAt)
		}
	}
	return &arch, report, nil
}
