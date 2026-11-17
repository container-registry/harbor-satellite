package advisorylock_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/stretchr/testify/require"
)

// A key declared but missing from IDs or Namespaces would escape the
// uniqueness check.
func TestLockKeys_ListedOnceAndUnique(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "advisorylock.go", nil, 0)
	require.NoError(t, err)
	var declared []string
	for _, decl := range f.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
			for _, spec := range gen.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						declared = append(declared, name.Name)
					}
				}
			}
		}
	}
	ids, namespaces := advisorylock.IDs(), advisorylock.Namespaces()

	require.NotEmpty(t, declared)
	require.Len(t, declared, len(ids)+len(namespaces))
	seenID, seenNamespace := map[int64]string{}, map[int32]string{}
	for _, name := range declared {
		if id, ok := ids[name]; ok {
			require.NotContains(t, seenID, id, "%s and %s share lock ID %d", name, seenID[id], id)
			seenID[id] = name
			continue
		}
		ns, ok := namespaces[name]
		require.True(t, ok, "%s is listed in neither IDs nor Namespaces", name)
		require.NotContains(t, seenNamespace, ns, "%s and %s share namespace %d", name, seenNamespace[ns], ns)
		seenNamespace[ns] = name
	}
}
