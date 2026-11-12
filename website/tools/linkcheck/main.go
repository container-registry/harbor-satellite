// Command linkcheck verifies every site-internal link and fragment in a built
// Hugo site. External links are listed with -external but not fetched.
package main

import (
	"flag"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	attr = regexp.MustCompile(`\s(href|src|id|name)=("[^"]*"|'[^']*'|[^\s>]+)`)
	// Code blocks and Alpine attributes can carry href-looking text; links
	// are only read from tags.
	tag = regexp.MustCompile(`<(a|img|link|script|source|iframe)\s[^>]*>`)
	ids = regexp.MustCompile(`<[a-zA-Z][^>]*\s(?:id|name)=("[^"]*"|'[^']*'|[^\s>]+)`)
)

func main() {
	os.Exit(run())
}

func run() int {
	root := flag.String("root", "../public", "built site directory")
	base := flag.String("base", "https://satellite.container-registry.com/", "site base URL; links to it count as internal")
	external := flag.Bool("external", false, "print external link targets")
	flag.Parse()

	baseURL, err := url.Parse(*base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "linkcheck:", err)
		return 2
	}
	site, err := os.OpenRoot(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "linkcheck:", err)
		return 2
	}
	defer site.Close()
	pages := map[string]string{}
	err = fs.WalkDir(site.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		b, err := site.ReadFile(p)
		if err != nil {
			return err
		}
		pages["/"+p] = string(b)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "linkcheck:", err)
		return 2
	}

	anchors := map[string]map[string]bool{}
	for p, body := range pages {
		set := map[string]bool{}
		for _, m := range ids.FindAllStringSubmatch(body, -1) {
			set[html.UnescapeString(unquote(m[1]))] = true
		}
		anchors[p] = set
	}

	var broken []string
	ext := map[string]bool{}
	checked := 0
	for p, body := range pages {
		for _, t := range tag.FindAllString(body, -1) {
			for _, m := range attr.FindAllStringSubmatch(t, -1) {
				if m[1] != "href" && m[1] != "src" {
					continue
				}
				raw := html.UnescapeString(unquote(m[2]))
				u, err := url.Parse(raw)
				if err != nil {
					broken = append(broken, fmt.Sprintf("%s: unparsable %q", p, raw))
					continue
				}
				if u.Scheme == "mailto" || u.Scheme == "javascript" || u.Scheme == "data" {
					continue
				}
				if u.Host != "" && u.Host != baseURL.Host {
					ext[u.Scheme+"://"+u.Host+u.Path] = true
					continue
				}
				checked++
				if msg := check(site, p, u, anchors); msg != "" {
					broken = append(broken, fmt.Sprintf("%s: %s (%s)", p, raw, msg))
				}
			}
		}
	}

	if *external {
		list := make([]string, 0, len(ext))
		for e := range ext {
			list = append(list, e)
		}
		sort.Strings(list)
		for _, e := range list {
			fmt.Println("external:", e)
		}
	}
	sort.Strings(broken)
	for _, b := range broken {
		fmt.Println("BROKEN", b)
	}
	fmt.Printf("%d pages, %d internal links checked, %d broken, %d external targets not fetched\n", len(pages), checked, len(broken), len(ext))
	if len(broken) > 0 {
		return 1
	}
	return 0
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func check(site *os.Root, from string, u *url.URL, anchors map[string]map[string]bool) string {
	target := u.Path
	if target == "" {
		target = from
	} else if !strings.HasPrefix(target, "/") {
		target = path.Join(path.Dir(from), target)
		if strings.HasSuffix(u.Path, "/") {
			target += "/"
		}
	}
	file := target
	if strings.HasSuffix(file, "/") {
		file += "index.html"
	}
	info, err := site.Stat(strings.TrimPrefix(file, "/"))
	if err == nil && info.IsDir() {
		file = strings.TrimSuffix(file, "/") + "/index.html"
		_, err = site.Stat(strings.TrimPrefix(file, "/"))
	}
	if err != nil {
		return "missing " + file
	}
	if u.Fragment == "" {
		return ""
	}
	set, ok := anchors[file]
	if !ok {
		return ""
	}
	if !set[u.Fragment] {
		return "no anchor #" + u.Fragment
	}
	return ""
}
