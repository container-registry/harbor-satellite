package dbtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// PostgreSQL 19 drops comments from pg_stat_statements texts, so the
// "-- name:" header of a sqlc query is gone. The names are recovered by
// matching the normalised text against the queries sqlc generated.
var (
	sqlcNamesOnce sync.Once
	sqlcNames     map[string]string
)

var (
	nameHeader  = regexp.MustCompile(`^-- name: (\w+) `)
	lineComment = regexp.MustCompile(`--[^\n]*`)
	literal     = regexp.MustCompile(`\$\d+|'(?:[^']|'')*'|\b\d+\b`)
)

// fingerprint makes a sqlc query and its pg_stat_statements text equal:
// comments go, parameters and the constants PostgreSQL replaces become ?,
// whitespace collapses and a trailing semicolon is dropped.
func fingerprint(query string) string {
	q := lineComment.ReplaceAllString(query, " ")
	q = literal.ReplaceAllString(q, "?")
	q = strings.Join(strings.Fields(q), " ")
	return strings.TrimSuffix(q, ";")
}

// addName records name for fp. Queries that differ only in what
// PostgreSQL normalises away (LIMIT 1 against LIMIT $2) share a
// fingerprint, so its key lists every name, sorted and joined by "|".
func addName(fp, name string) {
	names := []string{name}
	if prev, ok := sqlcNames[fp]; ok {
		names = append(strings.Split(prev, "|"), name)
	}
	slices.Sort(names)
	sqlcNames[fp] = strings.Join(slices.Compact(names), "|")
}

func sqlcQueryNames() map[string]string {
	sqlcNamesOnce.Do(func() {
		sqlcNames = map[string]string{}
		_, self, _, ok := runtime.Caller(0)
		if !ok {
			return
		}
		files, err := filepath.Glob(filepath.Join(filepath.Dir(self), "..", "database", "*.sql.go"))
		if err != nil {
			return
		}
		for _, f := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				continue
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				if m := nameHeader.FindStringSubmatch(text); m != nil {
					addName(fingerprint(text), m[1])
				}
				return true
			})
		}
	})
	return sqlcNames
}
