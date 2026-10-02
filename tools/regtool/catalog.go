package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SignedBase is where the signed tarballs are released.
const SignedBase = "https://github.com/Ervisio/plugins/releases/download/"

func decodeLoose(b []byte, v any) error { return json.Unmarshal(b, v) }

// prBody renders the pull request text for a new version of a plugin.
func prBody(src *Source, old, cur *Entry, notes string) (string, bool, error) {
	var oldPerms []permItem
	if old != nil {
		var err error
		if oldPerms, err = permissions(old.Manifest.Capabilities, old.Manifest.VisibleTo); err != nil {
			return "", false, fmt.Errorf("plugins/%s.json: %v", old.ID, err)
		}
	}
	curPerms, err := permissions(cur.Manifest.Capabilities, cur.Manifest.VisibleTo)
	if err != nil {
		return "", false, err
	}
	d := diffPermissions(oldPerms, curPerms)
	var b strings.Builder
	prev := "none (first version in the registry)"
	if old != nil {
		prev = old.Version
	}
	fmt.Fprintf(&b, "## %s %s\n\n", cur.Manifest.Name, cur.Version)
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Plugin | `%s` (%s) |\n", cur.ID, src.Trust)
	fmt.Fprintf(&b, "| Previous version | %s |\n", prev)
	fmt.Fprintf(&b, "| Upstream release | https://github.com/%s/releases/tag/%s |\n", cur.Repo, cur.Tag)
	fmt.Fprintf(&b, "| Source at the tag | https://github.com/%s/tree/%s |\n", cur.Repo, cur.Tag)
	fmt.Fprintf(&b, "| Asset | `%s`, sha256 `%s` |\n", cur.Asset, cur.UpstreamSHA256)
	if old != nil {
		fmt.Fprintf(&b, "| Changes since %s | https://github.com/%s/compare/%s...%s |\n", old.Version, cur.Repo, old.Tag, cur.Tag)
	}
	b.WriteString("\n### Permissions\n\n")
	b.WriteString(d.markdown(old == nil))
	if old == nil || !sameJSON(old.Manifest.Contributes, cur.Manifest.Contributes) {
		b.WriteString("\n### UI contributions\n\n")
		b.WriteString(contributesText(cur.Manifest.Contributes))
	}
	if old != nil {
		var meta []string
		for _, f := range [][3]string{{"name", old.Manifest.Name, cur.Manifest.Name}, {"author", old.Manifest.Author, cur.Manifest.Author},
			{"description", old.Manifest.Description, cur.Manifest.Description}, {"entry", old.Manifest.Entry, cur.Manifest.Entry}} {
			if f[1] != f[2] {
				meta = append(meta, fmt.Sprintf("- %s: %q -> %q", f[0], f[1], f[2]))
			}
		}
		if len(meta) > 0 {
			b.WriteString("\n### Other manifest changes\n\n" + strings.Join(meta, "\n") + "\n")
		}
	}
	b.WriteString("\n### Release notes\n\n")
	if strings.TrimSpace(notes) == "" {
		b.WriteString("_The release has no notes._\n")
	} else {
		for _, l := range strings.Split(strings.TrimSpace(strings.ReplaceAll(notes, "\r\n", "\n")), "\n") {
			b.WriteString("> " + l + "\n")
		}
	}
	b.WriteString("\n### Review\n\n")
	b.WriteString("- [ ] The source at the tag builds the released files (the plugin repository's release workflow ran from the tag).\n")
	b.WriteString("- [ ] The code changes since the previous version were read; nothing is obfuscated.\n")
	b.WriteString("- [ ] Every permission above is needed for what the plugin does.\n")
	b.WriteString("\nMerging signs this version with the Ervisio team key and publishes it in the catalog.\n")
	return b.String(), len(d.Added)+len(d.Changed) > 0, nil
}

func contributesText(raw json.RawMessage) string {
	var c struct {
		Pages    []struct{ ID, Title string }     `json:"pages"`
		Widgets  []struct{ ID, Title string }     `json:"widgets"`
		Snippets []struct{ Name, Command string } `json:"snippets"`
	}
	_ = json.Unmarshal(raw, &c)
	var b strings.Builder
	for _, p := range c.Pages {
		fmt.Fprintf(&b, "- Page `%s` (%s)\n", p.ID, p.Title)
	}
	for _, w := range c.Widgets {
		fmt.Fprintf(&b, "- Widget `%s` (%s)\n", w.ID, w.Title)
	}
	for _, s := range c.Snippets {
		fmt.Fprintf(&b, "- Terminal snippet `%s`: `%s`\n", s.Name, s.Command)
	}
	if b.Len() == 0 {
		return "None.\n"
	}
	return b.String()
}

// CatalogEntry is one plugin of catalog.json, in the core's format.
type CatalogEntry struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Author       string          `json:"author"`
	Description  string          `json:"description"`
	Icon         string          `json:"icon"`
	Logo         string          `json:"logo,omitempty"`
	Color        string          `json:"color"`
	Category     string          `json:"category"`
	Verified     bool            `json:"verified"`
	Installs     int             `json:"installs"`
	Featured     bool            `json:"featured,omitempty"`
	Notes        string          `json:"notes,omitempty"`
	MinCore      string          `json:"minCore,omitempty"`
	Requires     json.RawMessage `json:"requires,omitempty"`
	Source       string          `json:"source"`
	SHA256       string          `json:"sha256"`
	Capabilities json.RawMessage `json:"capabilities"`
	Contributes  json.RawMessage `json:"contributes"`
	VisibleTo    json.RawMessage `json:"visibleTo"`
	Homepage     string          `json:"homepage,omitempty"`
	Repo         string          `json:"repo"`
	Trust        string          `json:"trust"`
}

// CatalogFile is catalog.json.
type CatalogFile struct {
	Generated  string         `json:"generated"`
	Categories []Category     `json:"categories"`
	Plugins    []CatalogEntry `json:"plugins"`
}

// buildCatalog reads the reviewed entries and the signed packages in
// signedDir (<id>/manifest.json unpacked and <id>-<version>.tar.gz).
func buildCatalog(root, signedDir string, now time.Time) (*CatalogFile, error) {
	r, err := loadRegistry(root)
	if err != nil {
		return nil, err
	}
	c := &CatalogFile{Generated: now.UTC().Format(time.RFC3339), Categories: r.Categories, Plugins: []CatalogEntry{}}
	if c.Categories == nil {
		c.Categories = []Category{}
	}
	for _, src := range r.Plugins {
		e, err := loadEntry(root, src.ID)
		if err != nil {
			return nil, err
		}
		if e == nil {
			continue // listed, no reviewed version yet
		}
		dir := filepath.Join(signedDir, src.ID)
		if _, err := os.Stat(filepath.Join(dir, "manifest.sig")); err != nil {
			return nil, fmt.Errorf("%s: no signed package in %s", src.ID, dir)
		}
		m, err := readManifest(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", src.ID, err)
		}
		if d := compareReviewed(e, m); len(d) > 0 {
			return nil, fmt.Errorf("%s: the signed package is not the reviewed one: %s", src.ID, strings.Join(d, "; "))
		}
		logo, err := packageLogo(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", src.ID, err)
		}
		name := src.ID + "-" + e.Version + ".tar.gz"
		sum, err := fileSHA256(filepath.Join(signedDir, name))
		if err != nil {
			return nil, err
		}
		c.Plugins = append(c.Plugins, CatalogEntry{
			ID: src.ID, Name: m.Reviewed.Name, Version: e.Version, Author: m.Reviewed.Author, Description: m.Reviewed.Description,
			Icon: m.Reviewed.Icon, Logo: logo, Color: m.Reviewed.Color, Category: src.Category, Verified: true, Featured: src.Featured,
			Notes: e.Notes, MinCore: m.Reviewed.MinCore, Requires: m.Reviewed.Requires, Source: SignedBase + src.ID + "-" + e.Version + "/" + name, SHA256: sum,
			Capabilities: m.Reviewed.Capabilities, Contributes: m.Reviewed.Contributes, VisibleTo: m.Reviewed.VisibleTo,
			Homepage: m.Reviewed.Homepage, Repo: src.Repo, Trust: src.Trust,
		})
	}
	return c, nil
}

// logoFiles and maxLogo follow the core (plugins.LogoFiles, plugins.MaxLogoSize).
var logoFiles = []struct{ name, mime string }{{"logo.svg", "image/svg+xml"}, {"logo.png", "image/png"}}

const maxLogo = 64 << 10

// packageLogo returns the logo of a signed, unpacked package as a data: URL
// for the catalog, or "" when it has none. Only a file that the signed
// manifest lists is used, and its sha256 must be the listed one.
func packageLogo(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", err
	}
	var m struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "", fmt.Errorf("manifest.json: %v", err)
	}
	for _, l := range logoFiles {
		want, ok := m.Files[l.name]
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, l.name))
		if err != nil {
			return "", err
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
			return "", fmt.Errorf("%s does not match the signed manifest", l.name)
		}
		if len(data) == 0 || len(data) > maxLogo {
			continue // shown by no console: the catalog keeps the icon
		}
		return "data:" + l.mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return "", nil
}

func cmdCatalog(args []string) error {
	fs := flag.NewFlagSet("catalog", flag.ExitOnError)
	root := fs.String("root", ".", "registry checkout")
	signed := fs.String("signed", "", "folder with the signed packages (unpacked <id>/ and <id>-<version>.tar.gz)")
	out := fs.String("out", "catalog.json", "output file")
	fs.Parse(args)
	c, err := buildCatalog(*root, *signed, time.Now())
	if err != nil {
		return err
	}
	if err := writeJSON(*out, c); err != nil {
		return err
	}
	fmt.Printf("%s: %d plugin(s)\n", *out, len(c.Plugins))
	return nil
}

// cmdSite writes a small self-contained index.html for the Pages site.
func cmdSite(args []string) error {
	fs := flag.NewFlagSet("site", flag.ExitOnError)
	in := fs.String("catalog", "catalog.json", "catalog file")
	out := fs.String("out", "index.html", "output file")
	fs.Parse(args)
	b, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	var c CatalogFile
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	e := html.EscapeString
	var rows strings.Builder
	for _, p := range c.Plugins {
		trust := "Ervisio team"
		if p.Trust == "community" {
			trust = "Community, reviewed"
		}
		fmt.Fprintf(&rows, "<tr><td><b>%s</b><br><small>%s</small></td><td>%s</td><td>%s</td><td><a href=\"https://github.com/%s\">source</a> &middot; <a href=\"%s\">signed package</a></td></tr>\n",
			e(p.Name), e(p.Description), e(p.Version), e(trust), e(p.Repo), e(p.Source))
	}
	page := `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Ervisio plugins</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--ink:#16161a;--ink2:#55555f;--row:#f3f3f7;--acc:#4b53d6}
@media (prefers-color-scheme:dark){:root{--bg:#000;--ink:#f4f4f6;--ink2:#a3a3ad;--row:#0e0e10;--acc:#8b93ff}}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.5 system-ui,sans-serif}
main{max-width:860px;margin:0 auto;padding:32px 16px}
h1{font-size:28px;margin:0 0 8px}p{color:var(--ink2)}a{color:var(--acc)}
table{width:100%;border-collapse:separate;border-spacing:0 6px}td{padding:10px 12px;background:var(--row);vertical-align:top}
td:first-child{border-radius:12px 0 0 12px}td:last-child{border-radius:0 12px 12px 0}small{color:var(--ink2)}
code{font-size:13px}
</style></head><body><main>
<h1>Ervisio plugins</h1>
<p>The signed plugin catalog of <a href="https://github.com/Ervisio/ervisio">Ervisio</a>. Consoles read
<a href="catalog.json"><code>catalog.json</code></a> and verify <a href="catalog.sig"><code>catalog.sig</code></a> with the Ervisio team key;
every package is signed with the same key. Install plugins from Plugins &rsaquo; Browse in your console.</p>
<table>
` + rows.String() + `</table>
<p><small>Generated ` + e(c.Generated) + `. Registry: <a href="https://github.com/Ervisio/plugins">Ervisio/plugins</a>.</small></p>
</main></body></html>
`
	return os.WriteFile(*out, []byte(page), 0o644)
}
