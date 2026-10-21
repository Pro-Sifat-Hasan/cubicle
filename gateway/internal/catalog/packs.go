package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// Packs are data, not code: a passion sets the main agent's voice and look, a
// profession sets the team, routines and starter missions. Adding one is a JSON
// file in packs/passions or packs/professions.
//
//go:embed packs/passions/*.json packs/professions/*.json
var packFS embed.FS

// Profession pack ids with special meaning.
const OtherProfession = "other"

// Teammate is an agent a profession pack hires.
type Teammate struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Avatar       string   `json:"avatar"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
}

// PackRoutine is a recurring assignment a profession pack sets up. Only routines
// with Enabled true are switched on; the rest are ready for the owner to enable.
type PackRoutine struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Schedule    string `json:"schedule"`
	Agent       string `json:"agent"` // a teammate id from the same pack
	Enabled     bool   `json:"enabled"`
}

// Profession decides the team, routines and starter missions.
type Profession struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	Emoji            string        `json:"emoji"`
	Description      string        `json:"description"`
	Team             []Teammate    `json:"team"`
	Routines         []PackRoutine `json:"routines"`
	StarterMissions  []string      `json:"starter_missions"`
	StackSuggestions []string      `json:"stack_suggestions"`
}

var (
	passions    []store.Persona
	professions []Profession
)

func init() {
	passions = loadPacks[store.Persona]("packs/passions")
	professions = loadPacks[Profession]("packs/professions")
	for i := range passions {
		if err := store.NormalizePersona(&passions[i]); err != nil {
			panic(fmt.Sprintf("passion pack %q: %v", passions[i].ID, err))
		}
	}
	for _, p := range professions {
		if err := validateProfession(p); err != nil {
			panic(fmt.Sprintf("profession pack %q: %v", p.ID, err))
		}
	}
}

func loadPacks[T any](dir string) []T {
	entries, err := fs.ReadDir(packFS, dir)
	if err != nil {
		panic(err)
	}
	var out []T
	for _, e := range entries {
		raw, err := packFS.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			panic(err)
		}
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			panic(fmt.Sprintf("%s: %v", e.Name(), err))
		}
		out = append(out, v)
	}
	return out
}

func validateProfession(p Profession) error {
	if p.ID == "" || p.Title == "" {
		return fmt.Errorf("id and title are required")
	}
	ids := map[string]bool{}
	for _, t := range p.Team {
		a := store.Agent{ID: t.ID, Name: t.Name, Title: t.Title, Avatar: t.Avatar, Description: t.Description,
			Instructions: t.Instructions, Tools: t.Tools}
		if err := store.NormalizeAgent(&a); err != nil {
			return fmt.Errorf("teammate %q: %w", t.ID, err)
		}
		if ids[t.ID] {
			return fmt.Errorf("duplicate teammate %q", t.ID)
		}
		ids[t.ID] = true
	}
	for _, r := range p.Routines {
		if !ids[r.Agent] {
			return fmt.Errorf("routine %q is assigned to unknown teammate %q", r.Title, r.Agent)
		}
		if _, err := store.ParseSchedule(r.Schedule); err != nil {
			return fmt.Errorf("routine %q: %w", r.Title, err)
		}
	}
	return nil
}

// Passions returns every built-in passion, sorted by title.
func Passions() []store.Persona {
	out := append([]store.Persona(nil), passions...)
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func PassionByID(id string) (store.Persona, bool) {
	for _, p := range passions {
		if p.ID == id {
			return p, true
		}
	}
	return store.Persona{}, false
}

// Professions returns every profession pack with "other" last.
func Professions() []Profession {
	out := append([]Profession(nil), professions...)
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ID == OtherProfession) != (out[j].ID == OtherProfession) {
			return out[j].ID == OtherProfession
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func ProfessionByID(id string) (Profession, bool) {
	for _, p := range professions {
		if p.ID == id {
			return p, true
		}
	}
	return Profession{}, false
}
