// Package web serves the local-first review UI: IFD graph, tag byte
// ranges, pixel block map, geo transform candidates and out-of-bounds
// evidence, all rendered as HTML+SVG with stable identities on every
// value. Every review action appends events to the project log.
package web

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ifdmap/internal/archive"
	"ifdmap/internal/casepack"
	"ifdmap/internal/project"
	"ifdmap/internal/store"
	"ifdmap/internal/tiff"
)

// Server wires HTTP handlers to a project.
type Server struct {
	Proj  *project.Project
	mux   *http.ServeMux
	actor string
}

// New builds the HTTP handler tree.
func New(p *project.Project) *Server {
	s := &Server{Proj: p, mux: http.NewServeMux(), actor: "reviewer"}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/case", s.handleCase)
	s.mux.HandleFunc("/import", s.handleImport)
	s.mux.HandleFunc("/import-sample", s.handleImportSample)
	s.mux.HandleFunc("/import-archive", s.handleImportArchive)
	s.mux.HandleFunc("/source/", s.handleSource)
	s.mux.HandleFunc("/events", s.handleEvents)
	s.mux.HandleFunc("/decide", s.handleDecide)
	s.mux.HandleFunc("/undo", s.handleUndo)
	s.mux.HandleFunc("/params", s.handleParams)
	s.mux.HandleFunc("/verify", s.handleVerify)
	s.mux.HandleFunc("/export", s.handleExport)
	return s
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) view(r *http.Request) (*project.View, error) {
	asOf := int64(0)
	if q := r.URL.Query().Get("asof"); q != "" {
		n, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			return nil, err
		}
		asOf = n
	}
	return s.Proj.ViewAsOf(asOf)
}

func redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	v, err := s.view(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.renderIndex(w, v)
}

func (s *Server) handleCase(w http.ResponseWriter, r *http.Request) {
	s.renderCase(w, casepack.CaseManifest())
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	hexIn := strings.TrimSpace(r.FormValue("hex"))
	parent := strings.TrimSpace(r.FormValue("parent"))
	if name == "" || hexIn == "" {
		http.Error(w, "name and hex are required", 400)
		return
	}
	clean := strings.Map(func(c rune) rune {
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			return -1
		}
		return c
	}, hexIn)
	data, err := hex.DecodeString(clean)
	if err != nil {
		http.Error(w, fmt.Sprintf("bad hex input: %v", err), 400)
		return
	}
	src, _, err := s.Proj.ImportSource(s.actor, name, data, parent)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	redirect(w, r, "/source/"+src.ID)
}

func (s *Server) handleImportSample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	name := r.FormValue("name")
	parent := r.FormValue("parent")
	var data []byte
	if name == "baseline-landmark-v2" {
		data = casepack.BaselineRevision()
	} else {
		data = casepack.SampleBytes(name)
	}
	if data == nil {
		http.Error(w, "unknown sample "+name, 404)
		return
	}
	src, _, err := s.Proj.ImportSource(s.actor, name, data, parent)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	redirect(w, r, "/source/"+src.ID)
}

func (s *Server) handleImportArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	payload := r.FormValue("archive")
	if payload == "" {
		http.Error(w, "archive payload required", 400)
		return
	}
	a, err := archive.Import(s.Proj.Store, []byte(payload))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.Proj.Store.AppendEvent(store.EvImportArchive, s.actor, time.Now(), map[string]any{
		"events": len(a.Events), "sources": len(a.Sources), "derived": len(a.Derived),
	})
	redirect(w, r, "/")
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	a, err := archive.Export(s.Proj.Store, time.Now())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.Proj.Store.AppendEvent(store.EvExport, s.actor, time.Now(), map[string]any{
		"events": len(a.Events), "sources": len(a.Sources),
	})
	raw, err := a.Marshal()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="ifdmap-archive.json"`)
	w.Write(raw)
}

func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	cand := r.FormValue("candidate")
	action := r.FormValue("action")
	reason := r.FormValue("reason")
	back := r.FormValue("back")
	if _, _, err := s.Proj.Decide(s.actor, cand, action, reason); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if back == "" {
		back = "/"
	}
	redirect(w, r, back)
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	seq, err := strconv.ParseInt(r.FormValue("seq"), 10, 64)
	if err != nil {
		http.Error(w, "bad seq", 400)
		return
	}
	if _, err := s.Proj.Undo(s.actor, seq, r.FormValue("reason")); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	redirect(w, r, "/events")
}

func (s *Server) handleParams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	maxIFD, _ := strconv.Atoi(r.FormValue("maxIfds"))
	maxTags, _ := strconv.Atoi(r.FormValue("maxTags"))
	if maxIFD <= 0 || maxTags <= 0 {
		http.Error(w, "budgets must be positive", 400)
		return
	}
	if _, _, err := s.Proj.ReviseParams(s.actor, tiff.Budget{MaxIFDs: maxIFD, MaxTags: maxTags}, r.FormValue("note")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	redirect(w, r, "/")
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseForm()
	srcID := r.FormValue("source")
	if _, err := s.Proj.VerifyBaseline(s.actor, srcID); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	redirect(w, r, "/source/"+srcID)
}

// handleSource routes /source/{id}[/view].
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/source/")
	parts := strings.SplitN(rest, "/", 2)
	srcID := parts[0]
	view := "summary"
	if len(parts) == 2 && parts[1] != "" {
		view = parts[1]
	}
	v, err := s.view(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	src, err := s.Proj.Store.Source(srcID)
	if err != nil {
		http.Error(w, "unknown source "+srcID, 404)
		return
	}
	switch view {
	case "summary":
		s.renderSource(w, v, src)
	case "ifdgraph":
		s.renderIFDGraph(w, v, src)
	case "tags":
		s.renderTags(w, v, src)
	case "blocks":
		s.renderBlocks(w, v, src)
	case "geo":
		s.renderGeo(w, v, src)
	case "evidence":
		s.renderEvidence(w, v, src)
	default:
		http.NotFound(w, r)
	}
}

// parsePayload unmarshals the derived parse row for a source, falling
// back to a fresh parse when no derived row exists in this view.
func (s *Server) parsePayload(v *project.View, src *store.Source) (*tiff.File, bool) {
	if d := v.DerivedFor("parse", src.ID); d != nil {
		var f tiff.File
		if json.Unmarshal(d.Payload, &f) == nil {
			return &f, true
		}
	}
	prm, _ := s.Proj.CurrentParams()
	return tiff.Parse(src.Bytes, prm.Budget), false
}

func (s *Server) blocksPayload(v *project.View, src *store.Source) []*tiff.BlockMap {
	if d := v.DerivedFor("blocks", src.ID); d != nil {
		var maps []*tiff.BlockMap
		if json.Unmarshal(d.Payload, &maps) == nil {
			return maps
		}
	}
	return nil
}
