package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Registry is registry.json: the categories and the plugin sources.
type Registry struct {
	Categories []Category `json:"categories"`
	Plugins    []Source   `json:"plugins"`
}

// Category is a Browse category, copied to the catalog as is.
type Category struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// Source is one listed plugin: where its releases come from.
type Source struct {
	ID       string `json:"id"`
	Repo     string `json:"repo"`
	Trust    string `json:"trust"`
	Category string `json:"category"`
	Featured bool   `json:"featured,omitempty"`
}

// Entry is plugins/<id>.json: the reviewed version of a plugin.
type Entry struct {
	ID             string   `json:"id"`
	Repo           string   `json:"repo"`
	Version        string   `json:"version"`
	Tag            string   `json:"tag"`
	Asset          string   `json:"asset"`
	UpstreamSHA256 string   `json:"upstreamSha256"`
	Notes          string   `json:"notes"`
	Manifest       Reviewed `json:"manifest"`
}

// Reviewed is the part of the manifest reviewers approve. The JSON objects
// are kept verbatim.
type Reviewed struct {
	Name         string          `json:"name"`
	Author       string          `json:"author"`
	Description  string          `json:"description"`
	Homepage     string          `json:"homepage,omitempty"`
	Icon         string          `json:"icon,omitempty"`
	Color        string          `json:"color,omitempty"`
	Entry        string          `json:"entry"`
	Capabilities json.RawMessage `json:"capabilities"`
	Contributes  json.RawMessage `json:"contributes"`
	VisibleTo    json.RawMessage `json:"visibleTo"`
	// MinCore and Requires say which Ervisio the plugin needs (core 0.5.0).
	MinCore  string          `json:"minCore,omitempty"`
	Requires json.RawMessage `json:"requires,omitempty"`
	// Platforms: the systems the plugin works on (core 0.6.1); empty = Linux.
	Platforms json.RawMessage `json:"platforms,omitempty"`
}

var (
	idRe     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	semverRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	shaRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	colors   = map[string]bool{"ov": true, "term": true, "file": true, "log": true, "svc": true, "sw": true, "usr": true, "plg": true}
	trusts   = map[string]bool{"team": true, "community": true}
)

// decodeStrict decodes JSON and refuses unknown fields and trailing data.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

func loadRegistry(root string) (*Registry, error) {
	b, err := os.ReadFile(filepath.Join(root, "registry.json"))
	if err != nil {
		return nil, err
	}
	var r Registry
	if err := decodeStrict(b, &r); err != nil {
		return nil, fmt.Errorf("registry.json: %v", err)
	}
	return &r, nil
}

func entryPath(root, id string) string { return filepath.Join(root, "plugins", id+".json") }

// loadEntry reads plugins/<id>.json; (nil, nil) when it does not exist.
func loadEntry(root, id string) (*Entry, error) {
	b, err := os.ReadFile(entryPath(root, id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := decodeStrict(b, &e); err != nil {
		return nil, fmt.Errorf("plugins/%s.json: %v", id, err)
	}
	return &e, nil
}

func (r *Registry) source(id string) *Source {
	for i := range r.Plugins {
		if r.Plugins[i].ID == id {
			return &r.Plugins[i]
		}
	}
	return nil
}

// check validates registry.json and every plugins/*.json. It returns all
// problems found.
func check(root string) []string {
	var errs []string
	bad := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	r, err := loadRegistry(root)
	if err != nil {
		return []string{err.Error()}
	}
	cats := map[string]bool{}
	for _, c := range r.Categories {
		if c.ID == "" || c.Name == "" {
			bad("registry.json: a category needs an id and a name")
		}
		if cats[c.ID] {
			bad("registry.json: category %q is listed twice", c.ID)
		}
		if !colors[c.Color] {
			bad("registry.json: category %q: color must be one of ov term file log svc sw usr plg", c.ID)
		}
		cats[c.ID] = true
	}
	seen := map[string]bool{}
	for _, p := range r.Plugins {
		switch {
		case !idRe.MatchString(p.ID):
			bad("registry.json: %q is not a plugin id (^[a-z][a-z0-9-]{1,39}$)", p.ID)
		case seen[p.ID]:
			bad("registry.json: plugin %q is listed twice", p.ID)
		}
		seen[p.ID] = true
		if !repoRe.MatchString(p.Repo) {
			bad("registry.json: plugin %q: repo %q is not owner/name", p.ID, p.Repo)
		}
		if !trusts[p.Trust] {
			bad("registry.json: plugin %q: trust must be team or community", p.ID)
		}
		if !cats[p.Category] {
			bad("registry.json: plugin %q: category %q is not defined", p.ID, p.Category)
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "plugins", "*"))
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasPrefix(name, ".") {
			continue
		}
		id, ok := strings.CutSuffix(name, ".json")
		if !ok {
			bad("plugins/%s: only <id>.json files belong here", name)
			continue
		}
		e, err := loadEntry(root, id)
		if err != nil {
			bad("%v", err)
			continue
		}
		src := r.source(id)
		if src == nil {
			bad("plugins/%s: %q is not listed in registry.json", name, id)
			continue
		}
		if e.ID != id {
			bad("plugins/%s: id is %q, the file name says %q", name, e.ID, id)
		}
		if e.Repo != src.Repo {
			bad("plugins/%s: repo %q differs from registry.json (%q)", name, e.Repo, src.Repo)
		}
		if !semverRe.MatchString(e.Version) {
			bad("plugins/%s: version %q is not semver", name, e.Version)
		}
		if e.Tag != "v"+e.Version {
			bad("plugins/%s: tag must be v%s", name, e.Version)
		}
		if e.Asset != id+"-"+e.Version+".tar.gz" {
			bad("plugins/%s: asset must be %s-%s.tar.gz", name, id, e.Version)
		}
		if !shaRe.MatchString(e.UpstreamSHA256) {
			bad("plugins/%s: upstreamSha256 must be 64 lower-case hex characters", name)
		}
		m := e.Manifest
		if m.Name == "" || m.Entry == "" {
			bad("plugins/%s: manifest.name and manifest.entry are required", name)
		}
		if m.Color != "" && !colors[m.Color] {
			bad("plugins/%s: manifest.color %q is not a section colour", name, m.Color)
		}
		for k, raw := range map[string]json.RawMessage{"capabilities": m.Capabilities, "contributes": m.Contributes, "visibleTo": m.VisibleTo} {
			var obj map[string]any
			if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
				bad("plugins/%s: manifest.%s must be a JSON object", name, k)
			}
		}
	}
	sort.Strings(errs)
	return errs
}

// compareSemver: -1, 0, 1. Pre-releases sort before the release.
func compareSemver(a, b string) int {
	pa, pre1 := splitVer(a)
	pb, pre2 := splitVer(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case pre1 == pre2:
		return 0
	case pre1 == "":
		return 1
	case pre2 == "":
		return -1
	case pre1 < pre2:
		return -1
	}
	return 1
}

func splitVer(v string) ([3]int, string) {
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	var n [3]int
	for i, s := range strings.SplitN(core, ".", 3) {
		n[i], _ = strconv.Atoi(s)
	}
	return n, pre
}
