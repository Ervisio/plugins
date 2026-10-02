package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// permItem is one permission in human terms. Key identifies it across
// versions; Text describes it; Risks are short warnings for reviewers.
type permItem struct {
	Key   string
	Text  string
	Risks []string
}

func (p permItem) line() string {
	s := p.Text
	if len(p.Risks) > 0 {
		s += " **[" + strings.Join(p.Risks, "; ") + "]**"
	}
	return s
}

type capsDoc struct {
	Commands []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Argv        []string `json:"argv"`
		Args        []struct {
			Pattern   string `json:"pattern"`
			AllowDash bool   `json:"allowDash"`
		} `json:"args"`
		Admin            bool   `json:"admin"`
		AdminUnlessGroup string `json:"adminUnlessGroup"`
		Pty              bool   `json:"pty"`
		TimeoutSec       int    `json:"timeoutSec"`
	} `json:"commands"`
	HTTP []struct {
		Name             string   `json:"name"`
		Socket           string   `json:"socket"`
		Admin            bool     `json:"admin"`
		AdminUnlessGroup string   `json:"adminUnlessGroup"`
		Headers          []string `json:"headers"`
		Rules            []struct {
			Methods []string `json:"methods"`
			Path    string   `json:"path"`
		} `json:"rules"`
	} `json:"http"`
	Files struct {
		Read  []json.RawMessage `json:"read"`
		Write []json.RawMessage `json:"write"`
	} `json:"files"`
	Sockets []string `json:"sockets"`
	Network []string `json:"network"`
}

type folder struct {
	Path             string `json:"path"`
	Admin            bool   `json:"admin"`
	AdminUnlessGroup string `json:"adminUnlessGroup"`
	Create           bool   `json:"create"`
}

func parseFolder(raw json.RawMessage) folder {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return folder{Path: s}
	}
	var f folder
	_ = json.Unmarshal(raw, &f)
	return f
}

func adminText(admin bool, group string) (string, []string) {
	switch {
	case admin && group != "":
		return fmt.Sprintf(", as root (members of group `%s` run it as themselves)", group), []string{"administrator rights", "group " + group}
	case admin:
		return ", as root", []string{"administrator rights"}
	}
	return ", as the signed-in user", nil
}

func isDockerSocket(p string) bool {
	return strings.HasSuffix(p, "/docker.sock")
}

// permissions flattens a manifest's capabilities and visibleTo into items.
func permissions(caps, visible json.RawMessage) ([]permItem, error) {
	var c capsDoc
	if len(caps) > 0 {
		if err := json.Unmarshal(caps, &c); err != nil {
			return nil, fmt.Errorf("capabilities: %v", err)
		}
	}
	var out []permItem
	for _, cmd := range c.Commands {
		as, risks := adminText(cmd.Admin, cmd.AdminUnlessGroup)
		kind := "Command"
		if cmd.Pty {
			kind = "Terminal command"
			risks = append(risks, "interactive terminal")
		}
		var pats []string
		for _, a := range cmd.Args {
			p := "`" + a.Pattern + "`"
			if a.AllowDash {
				p += " (may start with -)"
			}
			pats = append(pats, p)
		}
		text := fmt.Sprintf("%s `%s`: runs `%s`%s", kind, cmd.Name, strings.Join(cmd.Argv, " "), as)
		if len(pats) > 0 {
			text += "; arguments " + strings.Join(pats, ", ")
		}
		out = append(out, permItem{Key: "command:" + cmd.Name, Text: text, Risks: risks})
	}
	for _, h := range c.HTTP {
		as, risks := adminText(h.Admin, h.AdminUnlessGroup)
		if isDockerSocket(h.Socket) {
			risks = append(risks, "Docker API: equivalent to root")
		}
		text := fmt.Sprintf("HTTP API `%s` on socket `%s`%s", h.Name, h.Socket, as)
		if len(h.Headers) > 0 {
			text += "; headers " + strings.Join(h.Headers, ", ")
		}
		out = append(out, permItem{Key: "http:" + h.Name, Text: text, Risks: risks})
		for _, r := range h.Rules {
			m := strings.Join(r.Methods, ",")
			var rr []string
			for _, x := range r.Methods {
				if x != "GET" && x != "HEAD" && x != "OPTIONS" {
					rr = []string{"writes"}
					break
				}
			}
			out = append(out, permItem{Key: "http:" + h.Name + ":" + m + " " + r.Path, Text: fmt.Sprintf("HTTP API `%s`: %s `%s`", h.Name, m, r.Path), Risks: rr})
		}
	}
	for _, mode := range []struct {
		name string
		list []json.RawMessage
	}{{"read", c.Files.Read}, {"write", c.Files.Write}} {
		for _, raw := range mode.list {
			f := parseFolder(raw)
			as, risks := adminText(f.Admin, f.AdminUnlessGroup)
			verb := "Read files in"
			if mode.name == "write" {
				verb = "Read and write files in"
				if !strings.HasPrefix(f.Path, "~") {
					risks = append(risks, "system folder")
				}
			}
			text := fmt.Sprintf("%s `%s`%s", verb, f.Path, as)
			if f.Create {
				text += "; created when missing"
			}
			out = append(out, permItem{Key: "files:" + mode.name + ":" + f.Path, Text: text, Risks: risks})
		}
	}
	for _, s := range c.Sockets {
		var risks []string
		if isDockerSocket(s) {
			risks = []string{"Docker socket"}
		}
		out = append(out, permItem{Key: "socket:" + s, Text: fmt.Sprintf("Talks to socket `%s` (informational)", s), Risks: risks})
	}
	for _, n := range c.Network {
		out = append(out, permItem{Key: "network:" + n, Text: fmt.Sprintf("The plugin page may connect to `%s` over https/wss", n), Risks: []string{"network"}})
	}
	var v struct {
		Groups []string `json:"groups"`
	}
	_ = json.Unmarshal(visible, &v)
	who := "Visible to every user who can sign in"
	if len(v.Groups) > 0 {
		g := append([]string(nil), v.Groups...)
		sort.Strings(g)
		who = "Visible to administrators and members of " + strings.Join(g, ", ")
	}
	out = append(out, permItem{Key: "visibleTo", Text: who})
	return out, nil
}

// permDiff compares two permission lists. With old == nil every item is new.
type permDiff struct {
	Added, Removed []permItem
	Changed        [][2]permItem
}

func (d permDiff) Empty() bool { return len(d.Added)+len(d.Removed)+len(d.Changed) == 0 }

func diffPermissions(old, cur []permItem) permDiff {
	om := map[string]permItem{}
	for _, p := range old {
		om[p.Key] = p
	}
	nm := map[string]bool{}
	var d permDiff
	for _, p := range cur {
		nm[p.Key] = true
		o, ok := om[p.Key]
		switch {
		case !ok:
			d.Added = append(d.Added, p)
		case o.line() != p.line():
			d.Changed = append(d.Changed, [2]permItem{o, p})
		}
	}
	for _, p := range old {
		if !nm[p.Key] {
			d.Removed = append(d.Removed, p)
		}
	}
	return d
}

// markdown renders the diff for a pull request.
func (d permDiff) markdown(first bool) string {
	var b strings.Builder
	if d.Empty() {
		b.WriteString("No permission changes.\n")
		return b.String()
	}
	if len(d.Added) > 0 {
		if first {
			fmt.Fprintf(&b, "**New permissions** (first version in the registry, %d):\n\n", len(d.Added))
		} else {
			fmt.Fprintf(&b, "**New permissions** (%d):\n\n", len(d.Added))
		}
		for _, p := range d.Added {
			b.WriteString("- " + p.line() + "\n")
		}
		b.WriteString("\n")
	}
	if len(d.Changed) > 0 {
		fmt.Fprintf(&b, "**Changed permissions** (%d):\n\n", len(d.Changed))
		for _, c := range d.Changed {
			b.WriteString("- " + c[1].line() + "\n  - was: " + c[0].line() + "\n")
		}
		b.WriteString("\n")
	}
	if len(d.Removed) > 0 {
		fmt.Fprintf(&b, "**Removed permissions** (%d):\n\n", len(d.Removed))
		for _, p := range d.Removed {
			b.WriteString("- " + p.line() + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
