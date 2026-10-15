package arch_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

var (
	secretName = regexp.MustCompile(`(?i)(password|secret|token)$`)
	// zerologValueMethods take values that end up in the log line.
	zerologValueMethods = map[string]bool{"Msg": true, "Msgf": true, "Str": true, "Strs": true, "Interface": true, "Any": true, "Bytes": true, "RawJSON": true}
	stdlogFuncs         = map[string]bool{"Print": true, "Printf": true, "Println": true, "Fatal": true, "Fatalf": true, "Fatalln": true, "Panic": true, "Panicf": true, "Panicln": true}
	// redactors make a secret safe to log.
	redactors = map[string]bool{"maskToken": true, "len": true}
)

// TestNoSecretsInLogArguments rejects log calls whose arguments name a
// password, secret or token, or that log a raw response body.
func TestNoSecretsInLogArguments(t *testing.T) {
	pending.Plan(t, "D4", "the status report logs the robot password and the credential refresh logs the response body with the new secret")
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var findings []string
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			findings = append(findings, secretLogArguments(fset, file)...)
			return nil
		})
		require.NoError(t, err)
	}

	require.Empty(t, findings, "log arguments that may carry secrets")
}

func secretLogArguments(fset *token.FileSet, file *ast.File) []string {
	stdlog := stdlogName(file)
	var findings []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, isIdent := sel.X.(*ast.Ident)
		isStdlog := isIdent && stdlog != "" && recv.Name == stdlog && stdlogFuncs[sel.Sel.Name]
		if !isStdlog && !zerologValueMethods[sel.Sel.Name] {
			return true
		}
		if sel.Sel.Name == "Str" && len(call.Args) == 2 && isBodyKey(call.Args[0]) {
			findings = append(findings, fmt.Sprintf("%s: Str(\"body\", ...)", fset.Position(call.Pos())))
		}
		for _, arg := range call.Args {
			if what := secretIn(arg); what != "" {
				findings = append(findings, fmt.Sprintf("%s: %s(%s)", fset.Position(arg.Pos()), sel.Sel.Name, what))
			}
		}
		return true
	})
	return findings
}

// secretIn returns the name in expr that looks like a secret, or "".
func secretIn(expr ast.Expr) string {
	var found string
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			if fn, ok := n.Fun.(*ast.Ident); ok {
				if redactors[fn.Name] {
					return false
				}
				if fn.Name == "string" && len(n.Args) == 1 {
					if arg, ok := n.Args[0].(*ast.Ident); ok && arg.Name == "body" {
						found = "string(body)"
						return false
					}
				}
			}
		case *ast.SelectorExpr:
			if secretName.MatchString(n.Sel.Name) {
				found = n.Sel.Name
				return false
			}
		case *ast.Ident:
			if secretName.MatchString(n.Name) {
				found = n.Name
				return false
			}
		}
		return true
	})
	return found
}

// stdlogName is the name the file uses for the standard log package, or "".
func stdlogName(file *ast.File) string {
	for _, imp := range file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err != nil || path != "log" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "log"
	}
	return ""
}

func isBodyKey(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == "body"
}
