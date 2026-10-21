package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProjects(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleOwnerSaveProject(w http.ResponseWriter, r *http.Request) {
	s.saveProject(w, r, false)
}

func (s *Server) handleWorkerSaveProject(w http.ResponseWriter, r *http.Request) {
	s.saveProject(w, r, true)
}

// saveProject adds a project or updates the one with the same name. A path must be an existing folder.
func (s *Server) saveProject(w http.ResponseWriter, r *http.Request, fromWorker bool) {
	var in struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Notes string `json:"notes"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name, in.Path, in.Notes = strings.TrimSpace(in.Name), strings.TrimSpace(in.Path), strings.TrimSpace(in.Notes)
	for _, c := range []struct {
		field, v string
		max      int
		required bool
	}{{"name", in.Name, 60, true}, {"path", in.Path, 500, false}, {"notes", in.Notes, 4000, false}} {
		if err := checkLen(c.field, c.v, c.max, c.required); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if in.Path != "" {
		if !filepath.IsAbs(in.Path) {
			writeErr(w, http.StatusBadRequest, "path must be an absolute folder path")
			return
		}
		if info, err := os.Stat(in.Path); err != nil || !info.IsDir() {
			writeErr(w, http.StatusBadRequest, "path is not an existing folder")
			return
		}
		in.Path = filepath.Clean(in.Path)
	}
	p, err := s.store.UpsertProject(r.Context(), in.Name, in.Path, in.Notes)
	if errors.Is(err, store.ErrTooManyProjects) {
		writeErr(w, http.StatusConflict, "too many projects; remove one first")
		return
	}
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("project.saved", p)
	if fromWorker {
		s.logActivity(r.Context(), "", "", "Noted project: "+truncate(p.Name, 100))
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("project.deleted", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}
