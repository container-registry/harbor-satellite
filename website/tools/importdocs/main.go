// Command importdocs renders the repository's Markdown docs from a git ref
// into the Hugo content tree under website/content/docs.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type page struct {
	Source      string   `json:"source"`
	Target      string   `json:"target"`
	Title       string   `json:"title"`
	Weight      int      `json:"weight"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
	// Workdir marks guides whose commands use paths relative to the
	// source file's directory in a repository checkout.
	Workdir bool `json:"workdir"`
	// Ref overrides -ref for pages that live on another branch than the
	// imported docs, such as pages maintained alongside the website.
	Ref string `json:"ref"`
}

type manifest struct {
	Pages []page `json:"pages"`
	// Rewrites replace external URLs that are dead in the sources until the
	// sources themselves are fixed.
	Rewrites map[string]string `json:"rewrites"`
}

type importer struct {
	repo      string
	ref       string
	content   string
	static    string
	github    string
	githubRef string
	urls      map[string]string
	rewrites  map[string]string
	images    map[string]string
	problems  []string
}

func main() {
	imp := &importer{}
	manifestPath := flag.String("manifest", "importdocs/manifest.json", "page manifest")
	flag.StringVar(&imp.repo, "repo", "../..", "repository root")
	flag.StringVar(&imp.ref, "ref", "", "git ref to import from (required)")
	flag.StringVar(&imp.content, "content", "../content/docs", "Hugo docs content directory, replaced on every run")
	flag.StringVar(&imp.static, "static", "../static/images/docs", "directory for images referenced by the docs, replaced on every run")
	flag.StringVar(&imp.github, "github", "https://github.com/container-registry/harbor-satellite", "repository URL for links outside the imported pages")
	flag.StringVar(&imp.githubRef, "github-ref", "main", "branch that GitHub links point at")
	flag.Parse()
	if imp.ref == "" {
		fmt.Fprintln(os.Stderr, "importdocs: -ref is required")
		os.Exit(2)
	}
	if err := imp.run(*manifestPath); err != nil {
		fmt.Fprintln(os.Stderr, "importdocs:", err)
		os.Exit(1)
	}
}

func (imp *importer) run(manifestPath string) error {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%s: %w", manifestPath, err)
	}
	imp.urls = map[string]string{}
	imp.rewrites = m.Rewrites
	imp.images = map[string]string{}
	for _, p := range m.Pages {
		if p.Source != "" {
			imp.urls[p.Source] = pageURL(p.Target)
		}
	}
	for _, dir := range []string{imp.content, imp.static} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	for _, p := range m.Pages {
		if err := imp.write(p); err != nil {
			return fmt.Errorf("%s: %w", p.Target, err)
		}
	}
	if err := imp.copyImages(); err != nil {
		return err
	}
	if len(imp.problems) > 0 {
		sort.Strings(imp.problems)
		return errors.New("unresolved links:\n  " + strings.Join(imp.problems, "\n  "))
	}
	fmt.Printf("imported %d pages and %d images from %s\n", len(m.Pages), len(imp.images), imp.ref)
	return nil
}

func pageURL(target string) string {
	t := strings.TrimSuffix(target, ".md")
	t = strings.TrimSuffix(t, "_index")
	t = strings.TrimSuffix(t, "/")
	if t == "" {
		return "/docs/"
	}
	return "/docs/" + t + "/"
}

func (imp *importer) git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", imp.repo}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (imp *importer) objectType(p string) string {
	out, err := imp.git("cat-file", "-t", imp.ref+":"+p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (imp *importer) write(p page) error {
	var body string
	params := map[string]string{}
	title := p.Title
	if p.Source != "" {
		raw, err := imp.git("show", firstNonEmpty(p.Ref, imp.ref)+":"+p.Source)
		if err != nil {
			return err
		}
		var h1 string
		params, body = splitFrontMatter(strings.ReplaceAll(string(raw), "\r\n", "\n"))
		h1, body = takeTitle(body)
		if title == "" {
			title = firstNonEmpty(h1, params["title"])
		}
		body = imp.rewriteLinks(p.Source, body)
		if p.Workdir {
			dir := path.Dir(p.Source)
			body = fmt.Sprintf("\n> Run the commands on this page from [`%s`](%s/tree/%s/%s) in a clone of the [repository](%s).\n%s",
				dir, imp.github, imp.githubRef, dir, imp.github, body)
		}
	}
	if title == "" {
		return errors.New("no title in manifest, front matter or first heading")
	}
	desc := firstNonEmpty(p.Description, summary(body))

	var fm strings.Builder
	fm.WriteString("---\n")
	fmt.Fprintf(&fm, "title: %s\n", strconv.Quote(title))
	fmt.Fprintf(&fm, "weight: %d\n", p.Weight)
	if desc != "" {
		fmt.Fprintf(&fm, "description: %s\n", strconv.Quote(desc))
	}
	if p.Source != "" {
		fmt.Fprintf(&fm, "source: %s\n", strconv.Quote(p.Source))
	}
	for _, k := range []string{"status", "date"} {
		if v := params[k]; v != "" {
			fmt.Fprintf(&fm, "%s: %s\n", k, strconv.Quote(v))
		}
	}
	if len(p.Aliases) > 0 {
		fm.WriteString("aliases:\n")
		for _, a := range p.Aliases {
			fmt.Fprintf(&fm, "  - %s\n", strconv.Quote(a))
		}
	}
	fm.WriteString("---\n\n")

	out := filepath.Join(imp.content, filepath.FromSlash(p.Target))
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(fm.String()+strings.TrimLeft(body, "\n")), 0o600)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// splitFrontMatter keeps scalar "key: value" lines only; nested YAML in the
// sources (ADR deciders lists) is not needed on the site.
func splitFrontMatter(s string) (map[string]string, string) {
	params := map[string]string{}
	if !strings.HasPrefix(s, "---\n") {
		return params, s
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		return params, s
	}
	for _, line := range strings.Split(s[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v != "" && !strings.HasPrefix(v, "[") {
			params[strings.TrimSpace(k)] = v
		}
	}
	return params, s[4+end+5:]
}

// takeTitle removes the first level-one heading, which the layout renders from
// the front matter title.
func takeTitle(body string) (string, string) {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, l := range lines {
		if isFence(l) {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(l, "# ") {
			return strings.TrimSpace(l[2:]), strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
		}
	}
	return "", body
}

func isFence(line string) bool {
	t := strings.TrimLeft(line, " ")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

var (
	mdLink   = regexp.MustCompile(`\]\(([^)\s]+)((?:\s+"[^"]*")?)\)`)
	refLink  = regexp.MustCompile(`^(\s*\[[^\]]+\]:\s*)(\S+)(.*)$`)
	htmlAttr = regexp.MustCompile(`\b(src|href)="([^"]+)"`)
	plainMd  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
)

func (imp *importer) rewriteLinks(source, body string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, l := range lines {
		if isFence(l) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		l = mdLink.ReplaceAllStringFunc(l, func(m string) string {
			sub := mdLink.FindStringSubmatch(m)
			return "](" + imp.resolve(source, sub[1]) + sub[2] + ")"
		})
		l = htmlAttr.ReplaceAllStringFunc(l, func(m string) string {
			sub := htmlAttr.FindStringSubmatch(m)
			return sub[1] + `="` + imp.resolve(source, sub[2]) + `"`
		})
		if sub := refLink.FindStringSubmatch(l); sub != nil {
			l = sub[1] + imp.resolve(source, sub[2]) + sub[3]
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

var imageExt = map[string]bool{".png": true, ".svg": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

func (imp *importer) resolve(source, target string) string {
	if r, ok := imp.rewrites[target]; ok {
		return r
	}
	if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return target
	}
	p, frag, _ := strings.Cut(target, "#")
	if frag != "" {
		frag = "#" + frag
	}
	resolved := path.Clean(path.Join(path.Dir(source), p))
	if strings.HasPrefix(resolved, "../") || resolved == ".." {
		imp.problems = append(imp.problems, fmt.Sprintf("%s: %s leaves the repository", source, target))
		return target
	}
	if u, ok := imp.urls[resolved]; ok {
		return u + frag
	}
	switch imp.objectType(resolved) {
	case "tree":
		if u, ok := imp.urls[path.Join(resolved, "README.md")]; ok {
			return u + frag
		}
		return fmt.Sprintf("%s/tree/%s/%s", imp.github, imp.githubRef, resolved)
	case "blob":
		if imageExt[strings.ToLower(path.Ext(resolved))] {
			u := "/images/docs/" + strings.TrimPrefix(resolved, "docs/")
			imp.images[resolved] = u
			return u
		}
		return fmt.Sprintf("%s/blob/%s/%s%s", imp.github, imp.githubRef, resolved, frag)
	default:
		imp.problems = append(imp.problems, fmt.Sprintf("%s: %s (%s) does not exist in %s", source, target, resolved, imp.ref))
		return target
	}
}

func (imp *importer) copyImages() error {
	for src, u := range imp.images {
		data, err := imp.git("show", imp.ref+":"+src)
		if err != nil {
			return err
		}
		out := filepath.Join(imp.static, filepath.FromSlash(strings.TrimPrefix(u, "/images/docs/")))
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// summary is the first prose paragraph, used as the meta description.
func summary(body string) string {
	inFence := false
	var para []string
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if isFence(l) {
			inFence = !inFence
			if len(para) > 0 {
				break
			}
			continue
		}
		if inFence {
			continue
		}
		if t == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if len(para) == 0 && strings.ContainsAny(t[:1], "#>|-*<!`[0123456789") {
			continue
		}
		para = append(para, t)
	}
	s := plainMd.ReplaceAllString(strings.Join(para, " "), "$1")
	s = strings.NewReplacer("`", "", "**", "").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if strings.HasSuffix(s, ":") {
		s = strings.TrimSuffix(s, ":") + "."
	}
	if len(s) <= 200 {
		return s
	}
	if i := strings.Index(s, ". "); i > 40 && i < 200 {
		return s[:i+1]
	}
	cut := strings.LastIndex(s[:200], " ")
	return s[:cut] + "..."
}
