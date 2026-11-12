package dbtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"runtime"
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
					sqlcNames[fingerprint(text)] = m[1]
				}
				return true
			})
		}
	})
	return sqlcNames
}
