package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The reference of the pego command is generated from what the command itself prints: its usage
// message (pego without arguments) and the flags of each command (pego <command> -h). Reading the
// source instead would have to follow every way a command can define its flags (flag sets made in
// helpers, flag.Var, flags added by a shared function), and would silently miss new ones.

// cliCommand is a command of the pego command.
type cliCommand struct {
	name        string
	synopsis    string // the first lines of its entry in the usage message
	description string
	flags       []cliFlag
}

// cliFlag is a flag as printed by flag.PrintDefaults.
type cliFlag struct {
	name, kind, def, usage string
}

// cliUsage is the usage message of the pego command, split into its parts.
type cliUsage struct {
	synopsis string // "pego <command> [flags] [arguments]"
	commands []cliCommand
	notes    []string // the paragraphs after the commands
}

var (
	flagLineRe = regexp.MustCompile(`^  -(\S+)(?: (\S+))?(?:\t(.*))?$`)
	defaultRe  = regexp.MustCompile(`^(.*) \(default (.+)\)$`)
	commandRe  = regexp.MustCompile(`^  ([a-z][a-z0-9-]*)\b`)
)

// parseUsage splits the usage message of the pego command.
func parseUsage(text string) (cliUsage, error) {
	var u cliUsage
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "usage: ") {
		return u, fmt.Errorf("the usage message does not start with \"usage: \": %.80q", text)
	}
	u.synopsis = strings.TrimPrefix(lines[0], "usage: ")
	i := 1
	for i < len(lines) && strings.TrimSpace(lines[i]) != "Commands:" {
		i++
	}
	if i == len(lines) {
		return u, errors.New("the usage message has no \"Commands:\" section")
	}
	i++
	var cmd *cliCommand
	var note strings.Builder
	for ; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.TrimSpace(l) == "":
			if note.Len() > 0 {
				u.notes = append(u.notes, note.String())
				note.Reset()
			}
			cmd = nil
		case note.Len() > 0 || !strings.HasPrefix(l, " "):
			if note.Len() > 0 {
				note.WriteByte(' ')
			}
			note.WriteString(strings.TrimSpace(l))
		case commandRe.MatchString(l) && !strings.HasPrefix(l, "   "):
			u.commands = append(u.commands, cliCommand{name: commandRe.FindStringSubmatch(l)[1], synopsis: "pego " + strings.TrimSpace(l)})
			cmd = &u.commands[len(u.commands)-1]
		case cmd != nil && strings.HasPrefix(l, "        "):
			cmd.synopsis += "\n     " + strings.TrimSpace(l)
		case cmd != nil:
			if cmd.description != "" {
				cmd.description += " "
			}
			cmd.description += strings.TrimSpace(l)
		}
	}
	if note.Len() > 0 {
		u.notes = append(u.notes, note.String())
	}
	if len(u.commands) == 0 {
		return u, errors.New("the usage message lists no commands")
	}
	return u, nil
}

// parseFlagDefaults reads the flags from the output of flag.PrintDefaults.
func parseFlagDefaults(text string) []cliFlag {
	var flags []cliFlag
	for _, l := range strings.Split(text, "\n") {
		if m := flagLineRe.FindStringSubmatch(l); m != nil {
			flags = append(flags, cliFlag{name: m[1], kind: m[2], usage: m[3]})
			continue
		}
		if len(flags) > 0 && strings.HasPrefix(l, "    \t") {
			f := &flags[len(flags)-1]
			if f.usage != "" {
				f.usage += " "
			}
			f.usage += strings.TrimSpace(l)
		}
	}
	for i := range flags {
		if m := defaultRe.FindStringSubmatch(flags[i].usage); m != nil {
			flags[i].usage, flags[i].def = m[1], m[2]
		}
	}
	return flags
}

// pegoHelp builds the pego command and returns its usage message and the flags of each command.
func pegoHelp(repo string) (cliUsage, error) {
	dir, err := os.MkdirTemp("", "pego-site-")
	if err != nil {
		return cliUsage{}, err
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "pego")
	build := exec.Command("go", "build", "-o", bin, "./cmd/pego")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		return cliUsage{}, fmt.Errorf("building the pego command: %v\n%s", err, out)
	}
	run := func(args ...string) string {
		var stderr bytes.Buffer
		cmd := exec.Command(bin, args...)
		cmd.Dir = repo
		cmd.Stderr = &stderr
		_ = cmd.Run() // the usage and -h exit with status 1
		return stderr.String()
	}
	u, err := parseUsage(strings.TrimPrefix(run(), "pego: "))
	if err != nil {
		return u, err
	}
	for i := range u.commands {
		c := &u.commands[i]
		c.flags = parseFlagDefaults(run(c.name, "-h"))
		if len(c.flags) == 0 {
			return u, fmt.Errorf("pego %s -h printed no flags", c.name)
		}
	}
	return u, nil
}

// renderCLI renders the reference of the pego command: the package comment, and each command of the
// usage message with its flags.
func (s *Site) renderCLI(p *Page) (string, []Heading, error) {
	fset, files, pkg, err := loadPackage(s.cfg.Repo, "cmd/pego", modulePath+"/cmd/pego")
	if err != nil {
		return "", nil, err
	}
	u, err := pegoHelp(s.cfg.Repo)
	if err != nil {
		return "", nil, err
	}
	p.Description = "Parse, format, convert, compile and debug grammars, and generate parsers, from the command line."
	esc := template.HTMLEscapeString
	r := &refRenderer{s: s, page: p, fset: fset, files: files, pkg: pkg, dir: "cmd/pego"}
	r.b.WriteString("<h1 id=\"top\">The pego Command</h1>\n")
	r.b.WriteString(`<pre><code class="language-bash">go install github.com/ornew/pego/cmd/pego@latest</code></pre>` + "\n")
	r.heading(2, "overview", "Overview")
	r.b.WriteString(r.docHTML(pkg.Doc))
	r.heading(2, "usage", "Usage")
	r.b.WriteString("<pre><code class=\"language-text\">" + esc(u.synopsis) + "</code></pre>\n")
	for _, n := range u.notes {
		r.b.WriteString("<p>" + esc(n) + "</p>\n")
	}
	r.b.WriteString("<p>This reference is generated from the output of <code>pego</code> and <code>pego &lt;command&gt; -h</code>.</p>\n")
	r.heading(2, "commands", "Commands")
	for _, c := range u.commands {
		r.heading(3, "cmd-"+c.name, "pego "+c.name)
		r.b.WriteString("<pre><code class=\"language-text\">" + esc(c.synopsis) + "</code></pre>\n")
		if c.description != "" {
			r.b.WriteString("<p>" + esc(c.description) + "</p>\n")
		}
		r.b.WriteString("<table><thead><tr><th>Flag</th><th>Default</th><th>Description</th></tr></thead><tbody>\n")
		for _, f := range c.flags {
			name := "-" + f.name
			if f.kind != "" {
				name += " " + f.kind
			}
			def := ""
			if f.def != "" {
				def = "<code>" + esc(f.def) + "</code>"
			}
			fmt.Fprintf(&r.b, "<tr><td><code>%s</code></td><td>%s</td><td>%s</td></tr>\n", esc(name), def, esc(f.usage))
		}
		r.b.WriteString("</tbody></table>\n")
	}
	return r.b.String(), r.toc, nil
}
