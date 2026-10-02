// Command regtool maintains the Ervisio plugin registry: it validates
// registry.json and plugins/*.json, turns a plugin release into a reviewed
// entry with a permission summary, builds catalog.json and signs it.
//
// Manifest validation and plugin signing are not done here: they use the
// core's plugin-sign (built from Ervisio/ervisio at CORE_REF), so the
// registry applies exactly the rules the consoles apply.
//
//	regtool check [-root DIR]
//	regtool list [-root DIR]                        "id repo trust" per line
//	regtool field FILE KEY                          a top-level string of a JSON file
//	regtool semver-gt A B                           exit 0 when A > B
//	regtool sha256 FILE
//	regtool compare -root DIR -id ID -dir PLUGINDIR
//	regtool sync-entry -root DIR -id ID -tag TAG -asset NAME -sha256 HEX -notes FILE -dir PLUGINDIR -body OUT [-labels OUT]
//	regtool catalog -root DIR -signed DIR -out catalog.json
//	regtool sign-catalog -key FILE catalog.json     writes catalog.sig next to it
//	regtool verify-catalog -pub B64|-pubfile FILE catalog.json catalog.sig
//	regtool site -catalog catalog.json -out index.html
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "check":
		err = cmdCheck(args)
	case "list":
		err = cmdList(args)
	case "field":
		err = cmdField(args)
	case "semver-gt":
		if len(args) != 2 {
			usage()
		}
		if compareSemver(args[0], args[1]) > 0 {
			os.Exit(0)
		}
		os.Exit(1)
	case "sha256":
		if len(args) != 1 {
			usage()
		}
		var s string
		s, err = fileSHA256(args[0])
		if err == nil {
			fmt.Println(s)
		}
	case "compare":
		err = cmdCompare(args)
	case "sync-entry":
		err = cmdSyncEntry(args)
	case "catalog":
		err = cmdCatalog(args)
	case "sign-catalog":
		err = cmdSignCatalog(args)
	case "verify-catalog":
		err = cmdVerifyCatalog(args)
	case "site":
		err = cmdSite(args)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "regtool:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: regtool check|list|field|semver-gt|sha256|compare|sync-entry|catalog|sign-catalog|verify-catalog|site ... (see main.go)")
	os.Exit(2)
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	root := fs.String("root", ".", "registry checkout")
	fs.Parse(args)
	errs := check(*root)
	for _, e := range errs {
		fmt.Println("::error::" + e)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d problem(s)", len(errs))
	}
	fmt.Println("registry.json and plugins/*.json are valid")
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	root := fs.String("root", ".", "registry checkout")
	fs.Parse(args)
	r, err := loadRegistry(*root)
	if err != nil {
		return err
	}
	for _, p := range r.Plugins {
		fmt.Println(p.ID, p.Repo, p.Trust)
	}
	return nil
}

func cmdField(args []string) error {
	if len(args) != 2 {
		usage()
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var m map[string]any
	if err := decodeLoose(b, &m); err != nil {
		return err
	}
	v, ok := m[args[1]]
	if !ok {
		return fmt.Errorf("%s has no field %q", args[0], args[1])
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("%s: field %q is not a string", args[0], args[1])
	}
	fmt.Println(s)
	return nil
}

func cmdCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	root := fs.String("root", ".", "registry checkout")
	id := fs.String("id", "", "plugin id")
	dir := fs.String("dir", "", "unpacked plugin folder")
	fs.Parse(args)
	e, err := loadEntry(*root, *id)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("plugins/%s.json does not exist", *id)
	}
	m, err := readManifest(*dir)
	if err != nil {
		return err
	}
	if d := compareReviewed(e, m); len(d) > 0 {
		return fmt.Errorf("the package of %s %s is not the reviewed one: %s", *id, e.Version, strings.Join(d, "; "))
	}
	fmt.Printf("%s %s matches plugins/%s.json\n", *id, e.Version, *id)
	return nil
}

func cmdSyncEntry(args []string) error {
	fs := flag.NewFlagSet("sync-entry", flag.ExitOnError)
	root := fs.String("root", ".", "registry checkout")
	id := fs.String("id", "", "plugin id")
	tag := fs.String("tag", "", "upstream release tag (vX.Y.Z)")
	asset := fs.String("asset", "", "upstream asset name")
	sum := fs.String("sha256", "", "sha256 of the upstream asset")
	notesFile := fs.String("notes", "", "file with the upstream release notes")
	dir := fs.String("dir", "", "unpacked plugin folder")
	bodyOut := fs.String("body", "", "write the pull request body here")
	labelsOut := fs.String("labels", "", "write extra labels here (one per line)")
	write := fs.Bool("write", true, "write plugins/<id>.json")
	fs.Parse(args)
	r, err := loadRegistry(*root)
	if err != nil {
		return err
	}
	src := r.source(*id)
	if src == nil {
		return fmt.Errorf("%q is not listed in registry.json", *id)
	}
	m, err := readManifest(*dir)
	if err != nil {
		return err
	}
	if m.ID != *id {
		return fmt.Errorf("the manifest id is %q, expected %q", m.ID, *id)
	}
	if "v"+m.Version != *tag {
		return fmt.Errorf("the manifest version is %q but the release tag is %q", m.Version, *tag)
	}
	if *asset != *id+"-"+m.Version+".tar.gz" {
		return fmt.Errorf("the asset must be named %s-%s.tar.gz", *id, m.Version)
	}
	if !shaRe.MatchString(*sum) {
		return fmt.Errorf("-sha256 must be 64 lower-case hex characters")
	}
	notes := ""
	if *notesFile != "" {
		b, err := os.ReadFile(*notesFile)
		if err != nil {
			return err
		}
		notes = string(b)
	}
	old, err := loadEntry(*root, *id)
	if err != nil {
		return err
	}
	e := &Entry{ID: *id, Repo: src.Repo, Version: m.Version, Tag: *tag, Asset: *asset, UpstreamSHA256: *sum, Notes: shortNotes(notes), Manifest: m.Reviewed}
	body, newPerms, err := prBody(src, old, e, notes)
	if err != nil {
		return err
	}
	if *write {
		if err := writeJSON(entryPath(*root, *id), e); err != nil {
			return err
		}
	}
	if *bodyOut != "" {
		if err := os.WriteFile(*bodyOut, []byte(body), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Print(body)
	}
	if *labelsOut != "" {
		l := "plugin-update\n"
		if newPerms {
			l += "new-permissions\n"
		}
		if err := os.WriteFile(*labelsOut, []byte(l), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func cmdSignCatalog(args []string) error {
	fs := flag.NewFlagSet("sign-catalog", flag.ExitOnError)
	key := fs.String("key", "", "private key file (base64, mode 0600)")
	fs.Parse(args)
	if *key == "" || fs.NArg() != 1 {
		usage()
	}
	k, err := LoadPrivate(*key)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	sig, err := SignCatalog(b, k)
	if err != nil {
		return err
	}
	out := filepath.Join(filepath.Dir(fs.Arg(0)), "catalog.sig")
	if err := os.WriteFile(out, sig, 0o644); err != nil {
		return err
	}
	fmt.Println("signed", fs.Arg(0), "->", out)
	return nil
}

func cmdVerifyCatalog(args []string) error {
	fs := flag.NewFlagSet("verify-catalog", flag.ExitOnError)
	pub := fs.String("pub", "", "base64 public key")
	pubFile := fs.String("pubfile", "", "file holding the base64 public key")
	fs.Parse(args)
	if fs.NArg() != 2 || (*pub == "") == (*pubFile == "") {
		usage()
	}
	text := *pub
	if *pubFile != "" {
		b, err := os.ReadFile(*pubFile)
		if err != nil {
			return err
		}
		text = string(b)
	}
	k, err := ParsePublic(text)
	if err != nil {
		return err
	}
	c, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	s, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		return err
	}
	if err := VerifyCatalog(c, s, k); err != nil {
		return err
	}
	fmt.Println("catalog signature ok")
	return nil
}
