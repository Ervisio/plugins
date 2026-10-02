package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The canonical form must match the core's plugins.Canonical byte for byte:
// sorted keys, no whitespace, no HTML escaping, numbers kept as written,
// U+2028/U+2029 escaped (encoding/json always does).
func TestCanonicalGolden(t *testing.T) {
	in := "{\n  \"z\": 1e3, \"a\": [0.10, -0, 12345678901234567890],\n  \"html\": \"<b>&amp;</b>\", \"uni\": \"caff\u00e8 \u2028 \\u00e9\",\n  \"nested\": {\"b\": true, \"a\": null}\n}\n"
	want := `{"a":[0.10,-0,12345678901234567890],"html":"<b>&amp;</b>","nested":{"a":null,"b":true},"uni":"caffè \u2028 é","z":1e3}`
	got, err := Canonical([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("canonical:\n got %s\nwant %s", got, want)
	}
}

func TestSignVerifyCatalog(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cat := []byte(`{"categories":[],"plugins":[{"id":"x","version":"1.0.0"}]}`)
	sig, err := SignCatalog(cat, priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(sig), "\n") {
		t.Fatal("catalog.sig must end with a newline")
	}
	// Formatting does not matter, content does.
	pretty := []byte("{\n \"plugins\": [ {\"version\": \"1.0.0\", \"id\": \"x\"} ],\n \"categories\": []\n}\n")
	if err := VerifyCatalog(pretty, sig, pub); err != nil {
		t.Fatalf("verify reformatted: %v", err)
	}
	if err := VerifyCatalog([]byte(`{"categories":[],"plugins":[{"id":"x","version":"1.0.1"}]}`), sig, pub); err == nil {
		t.Fatal("a changed catalog verified")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifyCatalog(cat, sig, other); err == nil {
		t.Fatal("verified with another key")
	}
	// The domain prefix is part of the message: a plain signature of the
	// canonical JSON (or of a plugin manifest) must not verify.
	c, _ := Canonical(cat)
	plain := ed25519.Sign(priv, append([]byte("linuxadmin-plugin-v1\n"), c...))
	if err := VerifyCatalog(cat, []byte(b64(plain)), pub); err == nil {
		t.Fatal("a manifest-domain signature verified as a catalog signature")
	}
}

func TestKeyFile(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := filepath.Join(t.TempDir(), "k")
	os.WriteFile(p, []byte(b64(priv)+"\n"), 0o644)
	if _, err := LoadPrivate(p); err == nil {
		t.Fatal("a world-readable key file was accepted")
	}
	os.Chmod(p, 0o600)
	k, err := LoadPrivate(p)
	if err != nil || !k.Equal(priv) {
		t.Fatalf("load: %v", err)
	}
	seed := filepath.Join(t.TempDir(), "s")
	os.WriteFile(seed, []byte(b64(priv.Seed())), 0o600)
	if k, err := LoadPrivate(seed); err != nil || !k.Equal(priv) {
		t.Fatalf("seed: %v", err)
	}
}

const capsV1 = `{"commands":[{"name":"ps","argv":["docker","ps"],"admin":false}],
 "http":[{"name":"docker","socket":"/var/run/docker.sock","admin":true,"adminUnlessGroup":"docker","headers":["Content-Type"],
          "rules":[{"methods":["GET"],"path":"/v1\\.[0-9]+/containers/json"}]}],
 "files":{"read":[],"write":["~/.config/x"]},"sockets":["/var/run/docker.sock"],"network":[]}`

const capsV2 = `{"commands":[{"name":"ps","argv":["docker","ps","-a"],"admin":false}],
 "http":[{"name":"docker","socket":"/var/run/docker.sock","admin":true,"adminUnlessGroup":"docker","headers":["Content-Type"],
          "rules":[{"methods":["GET"],"path":"/v1\\.[0-9]+/containers/json"},{"methods":["POST"],"path":"/v1\\.[0-9]+/containers/create"}]}],
 "files":{"read":[],"write":[{"path":"/opt/stacks","admin":true}]},"sockets":["/var/run/docker.sock"],"network":["hub.docker.com"]}`

func TestDiff(t *testing.T) {
	v1, err := permissions(json.RawMessage(capsV1), json.RawMessage(`{"groups":["docker"]}`))
	if err != nil {
		t.Fatal(err)
	}
	first := diffPermissions(nil, v1)
	if len(first.Added) != len(v1) || first.Empty() {
		t.Fatal("first version: every permission is new")
	}
	md := first.markdown(true)
	for _, s := range []string{"first version", "Docker API: equivalent to root", "group docker", "members of docker"} {
		if !strings.Contains(md, s) {
			t.Errorf("first diff lacks %q:\n%s", s, md)
		}
	}
	v2, _ := permissions(json.RawMessage(capsV2), json.RawMessage(`{"groups":["docker"]}`))
	d := diffPermissions(v1, v2)
	md = d.markdown(false)
	for _, s := range []string{"**New permissions** (3)", "/opt/stacks", "hub.docker.com", "containers/create", "**Changed permissions** (1)", "docker ps -a", "**Removed permissions** (1)", "~/.config/x", "system folder"} {
		if !strings.Contains(md, s) {
			t.Errorf("diff lacks %q:\n%s", s, md)
		}
	}
	if same := diffPermissions(v2, v2); !same.Empty() || same.markdown(false) != "No permission changes.\n" {
		t.Error("identical permissions must give no changes")
	}
}

func TestSemver(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"2.0.1", "2.0.0", 1}, {"2.0.0", "2.0.0", 0}, {"2.0.0-rc.1", "2.0.0", -1}, {"10.0.0", "9.9.9", 1}, {"v1.2.3", "1.2.3", 0}} {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestShortNotes(t *testing.T) {
	got := shortNotes("## 2.0.1\n\n- Settings moved.\r\n- Watchtower renamed.\n")
	if got != "Settings moved. Watchtower renamed." {
		t.Fatalf("got %q", got)
	}
	if n := len([]rune(shortNotes(strings.Repeat("a ", 400)))); n > 500 {
		t.Fatalf("notes not shortened: %d", n)
	}
}

// End to end: a registry with one plugin, sync-entry, check, compare, catalog.
func TestRegistryFlow(t *testing.T) {
	root := t.TempDir()
	reg := `{"categories":[{"id":"Containers","name":"Containers","icon":"server","color":"file"}],
	"plugins":[{"id":"demo","repo":"Example/plugin-demo","trust":"community","category":"Containers"}]}`
	os.WriteFile(filepath.Join(root, "registry.json"), []byte(reg), 0o644)
	pkg := filepath.Join(t.TempDir(), "demo")
	os.MkdirAll(pkg, 0o755)
	man := `{"id":"demo","name":"Demo","version":"1.0.0","author":"Someone","description":"A demo.","entry":"index.js","color":"plg",
	"capabilities":` + capsV1 + `,"contributes":{"pages":[{"id":"demo","title":"Demo"}]},"visibleTo":{"groups":[]}}`
	os.WriteFile(filepath.Join(pkg, "manifest.json"), []byte(man), 0o644)
	sum := strings.Repeat("ab", 32)
	notes := filepath.Join(t.TempDir(), "notes")
	os.WriteFile(notes, []byte("First release."), 0o644)
	body := filepath.Join(t.TempDir(), "body")
	labels := filepath.Join(t.TempDir(), "labels")
	if err := cmdSyncEntry([]string{"-root", root, "-id", "demo", "-tag", "v1.0.0", "-asset", "demo-1.0.0.tar.gz", "-sha256", sum, "-notes", notes, "-dir", pkg, "-body", body, "-labels", labels}); err != nil {
		t.Fatal(err)
	}
	if errs := check(root); len(errs) > 0 {
		t.Fatalf("check: %v", errs)
	}
	b, _ := os.ReadFile(body)
	if !strings.Contains(string(b), "first version in the registry") || !strings.Contains(string(b), "> First release.") {
		t.Errorf("body:\n%s", b)
	}
	if l, _ := os.ReadFile(labels); !strings.Contains(string(l), "new-permissions") {
		t.Errorf("labels: %q", l)
	}
	if err := cmdCompare([]string{"-root", root, "-id", "demo", "-dir", pkg}); err != nil {
		t.Fatalf("compare: %v", err)
	}
	// A package asking for more than was reviewed is refused.
	evil := filepath.Join(t.TempDir(), "demo")
	os.MkdirAll(evil, 0o755)
	os.WriteFile(filepath.Join(evil, "manifest.json"), []byte(strings.Replace(man, `"network":[]`, `"network":["evil.example"]`, 1)), 0o644)
	if err := cmdCompare([]string{"-root", root, "-id", "demo", "-dir", evil}); err == nil {
		t.Fatal("compare accepted different capabilities")
	}
	// Catalog from a "signed" folder.
	signed := t.TempDir()
	os.MkdirAll(filepath.Join(signed, "demo"), 0o755)
	os.WriteFile(filepath.Join(signed, "demo", "manifest.json"), []byte(man), 0o644)
	os.WriteFile(filepath.Join(signed, "demo", "manifest.sig"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(signed, "demo-1.0.0.tar.gz"), []byte("tarball"), 0o644)
	c, err := buildCatalog(root, signed, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Plugins) != 1 {
		t.Fatalf("plugins: %d", len(c.Plugins))
	}
	p := c.Plugins[0]
	if !p.Verified || p.Source != "https://github.com/Ervisio/plugins/releases/download/demo-1.0.0/demo-1.0.0.tar.gz" || len(p.SHA256) != 64 {
		t.Errorf("entry: %+v", p)
	}
	if !sameJSON(p.Capabilities, json.RawMessage(capsV1)) {
		t.Error("catalog capabilities differ from the manifest")
	}
	if p.Logo != "" {
		t.Errorf("logo without one in the package: %q", p.Logo)
	}
	// A logo listed in the signed manifest is embedded; one that does not match it fails the build.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	sumSVG := sha256.Sum256(svg)
	withFiles := strings.Replace(man, `"entry":"index.js",`, `"entry":"index.js","files":{"index.js":"`+strings.Repeat("0", 64)+`","logo.svg":"`+hex.EncodeToString(sumSVG[:])+`"},`, 1)
	os.WriteFile(filepath.Join(signed, "demo", "manifest.json"), []byte(withFiles), 0o644)
	os.WriteFile(filepath.Join(signed, "demo", "logo.svg"), svg, 0o644)
	if c, err = buildCatalog(root, signed, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if want := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg); c.Plugins[0].Logo != want {
		t.Errorf("logo: %q", c.Plugins[0].Logo)
	}
	os.WriteFile(filepath.Join(signed, "demo", "logo.svg"), []byte("<svg/>"), 0o644)
	if _, err := buildCatalog(root, signed, time.Unix(0, 0)); err == nil {
		t.Error("a logo that differs from the signed manifest was accepted")
	}
	// Bad registry entries are reported.
	os.WriteFile(filepath.Join(root, "registry.json"), []byte(`{"categories":[],"plugins":[{"id":"Bad","repo":"x","trust":"maybe","category":"none"}]}`), 0o644)
	if errs := check(root); len(errs) < 4 {
		t.Fatalf("check found only %v", errs)
	}
}
