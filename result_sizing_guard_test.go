package hdf5

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sizingHelper is the one function allowed to turn an element count into a
// result size (core.DatasetDescriptor.ResultSize, ADR-004 §1).
const sizingHelper = "ResultSize"

// elementSizeIdents are the names the readers give a datatype's element size.
var elementSizeIdents = map[string]bool{"elementSize": true, "elemSize": true}

// TestResultSizingGuard fails when a make([]T, …) in the root or internal/core
// package is sized from an element size anywhere but through
// DatasetDescriptor.ResultSize.
//
// Issue #75 came from seven such allocations in the hyperslab readers that the
// hand-written scope of ADR-003 missed: each sized its buffer as
// selection × datatype.Size before any check. Routing every reader through one
// validated helper fixes the class; this guard keeps it fixed, because the next
// reader would otherwise copy the pattern again.
//
// What it proves: no slice of any element type is allocated with a length or
// capacity computed from an element size, except inside ResultSize. A size
// expression is suspect when it mentions a field named Size (the datatype's
// size), a call of ElementSize(), an identifier named elementSize or elemSize,
// or a local variable assigned from such an expression in the same function.
// The value flows through arithmetic, integer conversions, min/max and the
// overflow-checked arithmetic of internal/utils (SafeMultiply, SafeAdd, …:
// their result is the same product, only checked for overflow).
//
// What it does not prove: allocations sized from an element count alone
// (make([]float64, numElements)). Those are bounded where the count is
// validated — ResultSize for the raw buffer, validateElementStorage before the
// decoded slice — which their own tests cover; a count is not distinguishable
// from any other length by syntax. Values passed through other calls or struct
// fields are not followed either.
func TestResultSizingGuard(t *testing.T) {
	var violations []string

	for _, dir := range []string{".", filepath.Join("internal", "core")} {
		found, err := uncheckedSizedMakes(dir)
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
		violations = append(violations, found...)
	}

	for _, v := range violations {
		t.Errorf("result buffer sized from an element size outside %s: %s", sizingHelper, v)
	}
}

// TestResultSizingGuard_Detects keeps the guard honest: each pattern below is a
// sizing it must report, and each clean one a sizing it must accept. Before
// the review (finding L3) a size laundered through utils.SafeMultiply, or a
// make of a non-byte slice, passed unseen.
func TestResultSizingGuard_Detects(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		suspect    bool
	}{
		{"direct element size", `buf := make([]byte, n*elemSize); _ = buf`, true},
		{"datatype Size field", `buf := make([]byte, int(n)*int(dt.Size)); _ = buf`, true},
		{"descriptor accessor", `buf := make([]byte, n*d.ElementSize()); _ = buf`, true},
		{"through a local", `total := n * elemSize; buf := make([]byte, total); _ = buf`, true},
		{"through SafeMultiply", `total, _ := utils.SafeMultiply(n, elemSize); buf := make([]byte, total); _ = buf`, true},
		{"through SafeAdd", `total, _ := utils.SafeAdd(base, elemSize); buf := make([]byte, total); _ = buf`, true},
		{"non-byte slice", `vals := make([]float64, n*elemSize); _ = vals`, true},
		{"capacity", `buf := make([]byte, 0, n*elemSize); _ = buf`, true},
		{"through ResultSize", `size, _ := d.ResultSize(n, 8, limit); buf := make([]byte, size); _ = buf`, false},
		{"element count only", `vals := make([]float64, numElements); _ = vals`, false},
		{"offset, not a size", `buf := make([]byte, 16); _ = data[i*elemSize]; _ = buf`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\nfunc f() {\n" + tc.body + "\n}\n"
			file, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			body := file.Decls[0].(*ast.FuncDecl).Body

			found := scanFunc(token.NewFileSet(), body)
			if got := len(found) > 0; got != tc.suspect {
				t.Errorf("suspect = %v, want %v for %q", got, tc.suspect, tc.body)
			}
		})
	}
}

// uncheckedSizedMakes returns the position of every suspect make([]T, …) in
// the non-test Go files of dir.
func uncheckedSizedMakes(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	var found []string

	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		scanned++

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name == sizingHelper {
				continue
			}
			found = append(found, scanFunc(fset, fn.Body)...)
		}
	}

	// A scan that read nothing proves nothing (a moved directory would
	// otherwise pass silently).
	if scanned == 0 {
		return nil, os.ErrNotExist
	}

	return found, nil
}

// scanFunc reports the suspect slice makes in one function body.
func scanFunc(fset *token.FileSet, body *ast.BlockStmt) []string {
	tainted := taintedLocals(body)

	var found []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isSliceMake(call) || len(call.Args) < 2 {
			return true
		}
		for _, arg := range call.Args[1:] {
			if mentionsElementSize(arg, tainted) {
				found = append(found, fset.Position(call.Pos()).String())
				break
			}
		}
		return true
	})

	return found
}

// taintedLocals returns the local names assigned, directly or through other
// locals, from an expression that mentions an element size.
func taintedLocals(body *ast.BlockStmt) map[string]bool {
	tainted := map[string]bool{}

	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(n ast.Node) bool {
			lhs, rhs, ok := assignment(n)
			if ok && taintAssigned(lhs, rhs, tainted) {
				changed = true
			}
			return true
		})
	}

	return tainted
}

// assignment returns the two sides of an assignment or a var declaration.
func assignment(n ast.Node) (lhs, rhs []ast.Expr, ok bool) {
	switch s := n.(type) {
	case *ast.AssignStmt:
		return s.Lhs, s.Rhs, true
	case *ast.ValueSpec:
		for _, id := range s.Names {
			lhs = append(lhs, id)
		}
		return lhs, s.Values, true
	default:
		return nil, nil, false
	}
}

// taintAssigned marks the identifiers of lhs whose value comes from an element
// size and reports whether it marked any.
func taintAssigned(lhs, rhs []ast.Expr, tainted map[string]bool) bool {
	marked := false
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok || id.Name == "_" || tainted[id.Name] {
			continue
		}
		if r := rhsFor(rhs, i); r != nil && mentionsElementSize(r, tainted) {
			tainted[id.Name] = true
			marked = true
		}
	}

	return marked
}

// rhsFor returns the expression assigned to the i-th left-hand side: the
// matching value, or the single multi-value call.
func rhsFor(rhs []ast.Expr, i int) ast.Expr {
	switch {
	case i < len(rhs) && len(rhs) > 1:
		return rhs[i]
	case len(rhs) == 1:
		return rhs[0]
	default:
		return nil
	}
}

// mentionsElementSize reports whether the value of expr is computed from an
// element size. The value flows through arithmetic, parentheses, integer
// conversions, min/max and the overflow-checked arithmetic helpers; it does
// not flow through other calls (their result is a different quantity, and
// ResultSize is the sanitizer), nor through index or slice expressions (an
// offset into a buffer does not size the buffer).
func mentionsElementSize(expr ast.Expr, tainted map[string]bool) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return elementSizeIdents[x.Name] || tainted[x.Name]
	case *ast.SelectorExpr:
		// A field named Size: the datatype's element size.
		return x.Sel.Name == "Size"
	case *ast.ParenExpr:
		return mentionsElementSize(x.X, tainted)
	case *ast.UnaryExpr:
		return mentionsElementSize(x.X, tainted)
	case *ast.BinaryExpr:
		return mentionsElementSize(x.X, tainted) || mentionsElementSize(x.Y, tainted)
	case *ast.CallExpr:
		// The descriptor's accessor returns the element size itself.
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ElementSize" {
			return true
		}
		if !isValuePreservingCall(x.Fun) {
			return false
		}
		for _, arg := range x.Args {
			if mentionsElementSize(arg, tainted) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// valuePreservingFuncs are the calls through which a size flows unchanged or
// combined: integer conversions and the min/max builtins.
var valuePreservingFuncs = map[string]bool{
	"int": true, "int32": true, "int64": true, "uint": true, "uint32": true, "uint64": true, "uintptr": true,
	"min": true, "max": true,
}

// checkedArithmetic are the internal/utils helpers that return their operands'
// sum or product, checked for overflow but otherwise the same value.
var checkedArithmetic = map[string]bool{
	"SafeMultiply": true, "SafeAdd": true, "CalculateBufferSize": true,
}

func isValuePreservingCall(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.Ident:
		return valuePreservingFuncs[f.Name]
	case *ast.SelectorExpr:
		pkg, ok := f.X.(*ast.Ident)
		return ok && pkg.Name == "utils" && checkedArithmetic[f.Sel.Name]
	default:
		return false
	}
}

// isSliceMake reports whether call is make([]T, …) for any element type T.
func isSliceMake(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "make" || len(call.Args) == 0 {
		return false
	}
	arr, ok := call.Args[0].(*ast.ArrayType)

	return ok && arr.Len == nil
}
