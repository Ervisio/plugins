package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// pluginManifest is what regtool reads from a plugin's manifest.json. The
// full validation is the core's plugin-sign; this only extracts fields.
type pluginManifest struct {
	ID       string
	Version  string
	Reviewed Reviewed
}

func readManifest(dir string) (*pluginManifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("manifest.json: %v", err)
	}
	str := func(k string) string {
		var s string
		if v, ok := raw[k]; ok {
			_ = json.Unmarshal(v, &s)
		}
		return s
	}
	obj := func(k string) json.RawMessage {
		if v, ok := raw[k]; ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return compact(v)
		}
		return json.RawMessage("{}")
	}
	m := &pluginManifest{ID: str("id"), Version: str("version")}
	m.Reviewed = Reviewed{
		Name: str("name"), Author: str("author"), Description: str("description"), Homepage: str("homepage"),
		Icon: str("icon"), Color: str("color"), Entry: str("entry"),
		Capabilities: obj("capabilities"), Contributes: obj("contributes"), VisibleTo: obj("visibleTo"),
		MinCore: str("minCore"),
	}
	if v, ok := raw["requires"]; ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		m.Reviewed.Requires = compact(v)
	}
	return m, nil
}

func compact(b []byte) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return json.RawMessage(b)
	}
	return json.RawMessage(buf.Bytes())
}

// sameJSON compares two JSON values in canonical form.
func sameJSON(a, b []byte) bool {
	ca, err1 := Canonical(a)
	cb, err2 := Canonical(b)
	return err1 == nil && err2 == nil && bytes.Equal(ca, cb)
}

// compareReviewed lists the differences between the manifest in dir and the
// reviewed entry. Empty means the package is what was reviewed.
func compareReviewed(e *Entry, m *pluginManifest) []string {
	var d []string
	if m.ID != e.ID {
		d = append(d, fmt.Sprintf("id is %q, the entry says %q", m.ID, e.ID))
	}
	if m.Version != e.Version {
		d = append(d, fmt.Sprintf("version is %q, the entry says %q", m.Version, e.Version))
	}
	r, got := e.Manifest, m.Reviewed
	for _, f := range [][3]string{
		{"name", r.Name, got.Name}, {"author", r.Author, got.Author}, {"description", r.Description, got.Description},
		{"homepage", r.Homepage, got.Homepage}, {"icon", r.Icon, got.Icon}, {"color", r.Color, got.Color}, {"entry", r.Entry, got.Entry},
		{"minCore", r.MinCore, got.MinCore},
	} {
		if f[1] != f[2] {
			d = append(d, fmt.Sprintf("%s is %q, the entry says %q", f[0], f[2], f[1]))
		}
	}
	for _, f := range []struct {
		name      string
		want, got json.RawMessage
	}{{"capabilities", r.Capabilities, got.Capabilities}, {"contributes", r.Contributes, got.Contributes}, {"visibleTo", r.VisibleTo, got.VisibleTo}, {"requires", r.Requires, got.Requires}} {
		if len(f.want) == 0 && len(f.got) == 0 {
			continue
		}
		if !sameJSON(f.want, f.got) {
			d = append(d, f.name+" differ from the reviewed entry")
		}
	}
	return d
}

// shortNotes turns release notes into the short text shown in Browse:
// headings dropped, whitespace folded, at most 500 characters.
func shortNotes(s string) string {
	var parts []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "---") {
			continue
		}
		l = strings.TrimSpace(strings.TrimLeft(l, "-*"))
		parts = append(parts, l)
	}
	out := strings.Join(parts, " ")
	if utf8.RuneCountInString(out) > 500 {
		r := []rune(out)
		out = strings.TrimSpace(string(r[:497])) + "..."
	}
	return out
}

// writeJSON writes v indented, without HTML escaping, with a final newline.
func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
