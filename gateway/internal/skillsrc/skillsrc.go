// Package skillsrc finds and fetches skills published on GitHub, the way skills.sh lists them.
//
// skills.sh is a directory: each skill is a SKILL.md in a public GitHub repository, installed with
// "npx skills add owner/repo". Its search API needs a Vercel token, so Cubicle takes what the owner
// pastes (a skills.sh link, the npx command, owner/repo or a GitHub link) and reads the repository
// directly. Only api.github.com and raw.githubusercontent.com are ever contacted, so a pasted link
// cannot point the gateway at anything else.
package skillsrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Limits that keep a hostile repository from costing much.
const (
	MaxSkills     = 60
	maxSkillBytes = 100 * 1024
	maxTreeBytes  = 8 << 20
	fetchWorkers  = 6
)

var (
	namePart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	refPart  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
	// First segments of skills.sh links that are pages, not repositories.
	siteSections = map[string]bool{"site": true, "topic": true, "agent": true, "packs": true, "audits": true,
		"docs": true, "api": true, "trending": true, "hot": true}
)

// Source says where skills come from.
type Source struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref,omitempty"`   // branch, tag or commit; empty means the default branch
	Skill string `json:"skill,omitempty"` // one skill by name, or empty for every skill in the repository
	Dir   string `json:"dir,omitempty"`   // a folder inside the repository to look in
}

// String is the owner/repo form.
func (s Source) String() string { return s.Owner + "/" + s.Repo }

// Parse understands what people paste: "owner/repo", "owner/repo/skill", "owner/repo@skill", a skills.sh or
// GitHub link, or an install command such as "npx skills add owner/repo --skill name".
func Parse(input string) (Source, error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return Source{}, errors.New("paste a skills.sh link, a command like \"npx skills add owner/repo\", or owner/repo")
	}
	var candidate, skill string
	for i := 0; i < len(fields); i++ {
		f := strings.Trim(fields[i], "\"'")
		switch {
		case f == "--skill" || f == "-s":
			if i+1 < len(fields) {
				skill = strings.Trim(fields[i+1], "\"'")
				i++
			}
		case strings.HasPrefix(f, "--skill="):
			skill = strings.Trim(strings.TrimPrefix(f, "--skill="), "\"'")
		case strings.HasPrefix(f, "-"):
		case candidate == "" && (strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") || isRepoLike(f)):
			candidate = f
		}
	}
	if candidate == "" {
		return Source{}, errors.New("could not find a repository in that text; use owner/repo or a skills.sh link")
	}
	src, err := fromCandidate(candidate)
	if err != nil {
		return Source{}, err
	}
	if skill != "" {
		src.Skill = skill
	}
	if err := src.validate(); err != nil {
		return Source{}, err
	}
	return src, nil
}

func isRepoLike(s string) bool {
	if strings.ContainsAny(s, " \\:") {
		return false
	}
	owner, _, ok := strings.Cut(strings.TrimSuffix(s, "/"), "/")
	return ok && namePart.MatchString(owner)
}

func fromCandidate(c string) (Source, error) {
	if !strings.HasPrefix(c, "http://") && !strings.HasPrefix(c, "https://") {
		c = strings.TrimSuffix(c, "/")
		skill := ""
		if repo, sk, ok := strings.Cut(c, "@"); ok { // owner/repo@skill
			c, skill = repo, sk
		}
		parts := strings.Split(c, "/")
		switch {
		case len(parts) == 2:
			return Source{Owner: parts[0], Repo: parts[1], Skill: skill}, nil
		case len(parts) == 3 && skill == "":
			return Source{Owner: parts[0], Repo: parts[1], Skill: parts[2]}, nil
		}
		return Source{}, errors.New("use owner/repo or owner/repo/skill")
	}
	u, err := url.Parse(c)
	if err != nil {
		return Source{}, errors.New("that link is not valid")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	switch host {
	case "skills.sh":
		if len(segs) > 0 && siteSections[segs[0]] {
			if segs[0] == "site" {
				return Source{}, errors.New("skills published on a website (not GitHub) are not supported yet; open its skills.sh page and use the GitHub repository it names")
			}
			return Source{}, errors.New("that skills.sh page is not a skill; open a skill and copy its link or install command")
		}
		switch {
		case len(segs) >= 3:
			return Source{Owner: segs[0], Repo: segs[1], Skill: segs[2]}, nil
		case len(segs) == 2:
			return Source{Owner: segs[0], Repo: segs[1]}, nil
		}
		return Source{}, errors.New("open a skill on skills.sh and copy its link, for example https://skills.sh/owner/repo/skill")
	case "github.com":
		if len(segs) < 2 {
			return Source{}, errors.New("a GitHub link needs an owner and a repository")
		}
		src := Source{Owner: segs[0], Repo: strings.TrimSuffix(segs[1], ".git")}
		if len(segs) >= 4 && (segs[2] == "tree" || segs[2] == "blob") {
			src.Ref = segs[3]
			if rest := segs[4:]; len(rest) > 0 {
				if strings.EqualFold(rest[len(rest)-1], "SKILL.md") {
					rest = rest[:len(rest)-1]
				}
				src.Dir = strings.Join(rest, "/")
			}
		}
		return src, nil
	}
	return Source{}, fmt.Errorf("links from %s are not supported; use skills.sh, GitHub or owner/repo", host)
}

func (s Source) validate() error {
	if !namePart.MatchString(s.Owner) || !namePart.MatchString(strings.TrimSuffix(s.Repo, ".git")) {
		return errors.New("that is not a valid GitHub owner/repo")
	}
	if s.Skill != "" && !namePart.MatchString(s.Skill) {
		return errors.New("that is not a valid skill name")
	}
	if s.Ref != "" && (!refPart.MatchString(s.Ref) || strings.Contains(s.Ref, "..")) {
		return errors.New("that is not a valid branch or tag")
	}
	if strings.Contains(s.Dir, "..") {
		return errors.New("that folder is not valid")
	}
	return nil
}

// Found is one skill in a repository.
type Found struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"` // path of its SKILL.md inside the repository
	Content     string `json:"-"`    // the instructions, without the front matter
}

// Client reads skills from GitHub. The zero value is not usable; use New.
type Client struct {
	HTTP    *http.Client
	APIBase string // https://api.github.com
	RawBase string // https://raw.githubusercontent.com
}

// New returns a client for the real GitHub.
func New() *Client {
	c := &Client{APIBase: "https://api.github.com", RawBase: "https://raw.githubusercontent.com"}
	c.HTTP = &http.Client{Timeout: 20 * time.Second}
	api, raw := hostOf(c.APIBase), hostOf(c.RawBase)
	c.HTTP.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("too many redirects")
		}
		if h := req.URL.Hostname(); h != api && h != raw {
			return fmt.Errorf("redirect to %s is not allowed", h)
		}
		return nil
	}
	return c
}

func hostOf(base string) string {
	u, _ := url.Parse(base)
	return u.Hostname()
}

// Discover lists the skills a source names, with their descriptions and instructions.
func (c *Client) Discover(ctx context.Context, src Source) ([]Found, error) {
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	var tree struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", c.APIBase, src.Owner, src.Repo, url.PathEscape(ref))
	body, status, err := c.get(ctx, treeURL, maxTreeBytes)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s was not found on GitHub (is it public?)", src)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("GitHub is limiting requests right now; try again in a few minutes")
	default:
		return nil, fmt.Errorf("GitHub answered %d", status)
	}
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, errors.New("GitHub sent something unexpected")
	}
	if tree.Truncated {
		return nil, errors.New("that repository is too large to search; link to the skill's folder instead")
	}

	var paths []string
	for _, e := range tree.Tree {
		if e.Type != "blob" || path.Base(e.Path) != "SKILL.md" || strings.Contains(e.Path, "node_modules/") {
			continue
		}
		dir := path.Dir(e.Path)
		if src.Dir != "" && dir != src.Dir && !strings.HasPrefix(dir, src.Dir+"/") {
			continue
		}
		if src.Skill != "" && !strings.EqualFold(path.Base(dir), src.Skill) {
			continue
		}
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		if src.Skill != "" {
			return nil, fmt.Errorf("no skill named %q in %s", src.Skill, src)
		}
		return nil, fmt.Errorf("no SKILL.md found in %s", src)
	}
	if len(paths) > MaxSkills {
		paths = paths[:MaxSkills]
	}

	out := make([]Found, len(paths))
	errs := make([]error, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fetchWorkers)
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, st, err := c.get(ctx, fmt.Sprintf("%s/%s/%s/%s/%s", c.RawBase, src.Owner, src.Repo, url.PathEscape(ref), escapePath(p)), maxSkillBytes+1)
			switch {
			case err != nil:
				errs[i] = err
			case st != http.StatusOK:
				errs[i] = fmt.Errorf("could not read %s (GitHub answered %d)", p, st)
			case len(raw) > maxSkillBytes:
				errs[i] = fmt.Errorf("%s is larger than %d KB", p, maxSkillBytes/1024)
			default:
				out[i] = parseSkill(string(raw), path.Base(path.Dir(p)), p)
			}
		}()
	}
	wg.Wait()

	var found []Found
	var firstErr error
	for i, f := range out {
		if errs[i] != nil {
			firstErr = errors.Join(firstErr, errs[i])
			continue
		}
		if f.Name != "" && f.Description != "" && strings.TrimSpace(f.Content) != "" {
			found = append(found, f)
		}
	}
	if len(found) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("the SKILL.md files in %s have no name or description", src)
	}
	return found, nil
}

func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func (c *Client) get(ctx context.Context, u string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Cubicle-Gateway")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, errors.New("could not reach GitHub; check your internet connection")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return body, resp.StatusCode, err
}

// parseSkill reads a SKILL.md: optional front matter with name and description, then the instructions.
func parseSkill(text, fallbackName, filePath string) Found {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	meta := map[string]string{}
	body := text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if head, after, found := strings.Cut(rest, "\n---"); found {
			meta = parseFrontMatter(head)
			body = strings.TrimPrefix(after, "\n")
		}
	}
	name := meta["name"]
	if name == "" {
		name = fallbackName
	}
	return Found{Name: name, Description: meta["description"], Path: filePath, Content: strings.TrimSpace(body)}
}

// parseFrontMatter handles the simple YAML skills use: "key: value", quoted values, and folded (>) or literal (|) blocks.
func parseFrontMatter(head string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(head, "\n")
	for i := 0; i < len(lines); i++ {
		key, val, ok := strings.Cut(lines[i], ":")
		if !ok || strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t") {
			continue
		}
		key, val = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(val)
		if val == ">" || val == "|" || val == ">-" || val == "|-" {
			var block []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t") || strings.TrimSpace(lines[i+1]) == "") {
				i++
				block = append(block, strings.TrimSpace(lines[i]))
			}
			sep := " "
			if strings.HasPrefix(val, "|") {
				sep = "\n"
			}
			val = strings.TrimSpace(strings.Join(block, sep))
		}
		out[key] = strings.Trim(val, "\"'")
	}
	return out
}
