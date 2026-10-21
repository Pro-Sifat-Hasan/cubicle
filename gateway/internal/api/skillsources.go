package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/skillsrc"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// discoveredSkill is what the app shows before the owner installs a skill from a repository.
type discoveredSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Installed   bool   `json:"installed"` // a skill with the same name and instructions already exists
}

func (s *Server) lookupSource(w http.ResponseWriter, r *http.Request, input string) (skillsrc.Source, []skillsrc.Found, bool) {
	src, err := skillsrc.Parse(input)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return src, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	found, err := s.SkillSource.Discover(ctx, src)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return src, nil, false
	}
	return src, found, true
}

// handleDiscoverSkills reads a skills.sh link, an "npx skills add" command, owner/repo or a GitHub link and
// lists the skills it names, without installing anything.
func (s *Server) handleDiscoverSkills(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source string `json:"source"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("source", strings.TrimSpace(in.Source), 500, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	src, found, ok := s.lookupSource(w, r, in.Source)
	if !ok {
		return
	}
	have, err := s.store.ListSkills(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	out := make([]discoveredSkill, 0, len(found))
	for _, f := range found {
		out = append(out, discoveredSkill{Name: f.Name, Description: f.Description, Path: f.Path, Installed: hasSkill(have, f)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": src, "skills": out})
}

func hasSkill(have []*store.Skill, f skillsrc.Found) bool {
	for _, sk := range have {
		if sk.Name == store.Slug(f.Name) && strings.TrimSpace(sk.Content) == strings.TrimSpace(f.Content) {
			return true
		}
	}
	return false
}

// handleInstallSkills installs skills from a source. paths picks skills from a discover result; empty means all.
// Installed skills belong to no agent until the owner attaches them, because a skill is instructions an agent follows.
func (s *Server) handleInstallSkills(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source string   `json:"source"`
		Paths  []string `json:"paths"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("source", strings.TrimSpace(in.Source), 500, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, found, ok := s.lookupSource(w, r, in.Source)
	if !ok {
		return
	}
	have, err := s.store.ListSkills(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	installed, skipped := []*store.Skill{}, []string{}
	for _, f := range found {
		if len(in.Paths) > 0 && !slices.Contains(in.Paths, f.Path) {
			continue
		}
		if hasSkill(have, f) {
			skipped = append(skipped, f.Name)
			continue
		}
		sk, err := s.store.CreateSkill(r.Context(), store.Skill{Name: f.Name, Description: f.Description, Content: f.Content, Enabled: true})
		if err != nil {
			skipped = append(skipped, f.Name+" ("+err.Error()+")")
			continue
		}
		installed = append(installed, sk)
	}
	if len(installed) > 0 {
		s.configChanged("skills.updated", installed[0])
		s.logActivity(r.Context(), "", "", "Installed "+strconv.Itoa(len(installed))+" skill(s) from "+strings.TrimSpace(in.Source)+".")
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": installed, "skipped": skipped})
}
