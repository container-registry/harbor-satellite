package fleet_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

// EventKinds feeds the event catalogue and the webhook types, so a kind
// added without it would never be published.
func TestEventKinds_ListsEveryKindConstant(t *testing.T) {
	declared := eventKindConstants(t)
	listed := map[fleet.EventKind]bool{}
	for _, k := range fleet.EventKinds() {
		require.NotEmpty(t, k)
		require.False(t, listed[k], "%s listed twice", k)
		listed[k] = true
	}

	require.Equal(t, declared, listed)
}

// eventKindConstants parses the package for constants of type EventKind.
func eventKindConstants(t *testing.T) map[fleet.EventKind]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	out := map[fleet.EventKind]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if ident, ok := vs.Type.(*ast.Ident); !ok || ident.Name != "EventKind" {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					require.True(t, ok, "%s: EventKind constant is not a literal", name)
					s, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					out[fleet.EventKind(s)] = true
				}
			}
		}
	}
	require.NotEmpty(t, out)
	return out
}

func TestLogOnlyKinds_AreEventKinds(t *testing.T) {
	for _, k := range fleet.LogOnlyKinds() {
		require.Contains(t, fleet.EventKinds(), k)
		require.True(t, k.LogOnly())
	}
	for _, k := range []fleet.EventKind{fleet.EventSatelliteCreated, fleet.EventGroupChanged, fleet.EventSatelliteDeleted} {
		require.False(t, k.LogOnly(), "%s has a handler", k)
	}
}
