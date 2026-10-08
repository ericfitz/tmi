// Command check-error-codes rejects string-literal error codes in non-generated,
// non-test Go files. Error codes must be internal/errcode constants so the
// server can only emit the documented vocabulary (ADR 2026-10-08, issue #1048).
//
// Usage: go run ./scripts/check-error-codes <root>
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// finding is one string-literal error code.
type finding struct {
	pos  token.Position
	lit  string
	kind string
}

// errorTypes are the struct types whose Error/Code fields carry an error code.
var errorTypes = map[string]bool{"Error": true, "OAuthError": true, "RequestError": true}

// skipDirs are directory names never scanned.
var skipDirs = map[string]bool{
	"testdata": true, "vendor": true, "node_modules": true, "wstest": true,
	"bin": true, "graphify-out": true,
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: return the first string literal inside an expression, ignoring function literals (pure)
func firstStringLit(e ast.Expr) *ast.BasicLit {
	var found *ast.BasicLit
	ast.Inspect(e, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				found = x
				return false
			}
		}
		return true
	})
	return found
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: return the unqualified name of a type expression (pure)
func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether a composite literal type is a gin.H or string-keyed map (pure)
func isStringMapType(e ast.Expr) bool {
	if se, ok := e.(*ast.SelectorExpr); ok {
		return se.Sel.Name == "H"
	}
	if mt, ok := e.(*ast.MapType); ok {
		k, ok := mt.Key.(*ast.Ident)
		return ok && k.Name == "string"
	}
	return false
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: collect string-literal error codes in one parsed file (pure)
func checkFile(fset *token.FileSet, file *ast.File) []finding {
	var out []finding
	add := func(e ast.Expr, kind string) {
		if lit := firstStringLit(e); lit != nil {
			out = append(out, finding{pos: fset.Position(lit.Pos()), lit: lit.Value, kind: kind})
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type == nil {
				return true
			}
			for _, el := range x.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				switch {
				case errorTypes[typeName(x.Type)]:
					if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "Error" || k.Name == "Code") {
						add(kv.Value, typeName(x.Type)+"."+k.Name)
					}
				case isStringMapType(x.Type):
					if k, ok := kv.Key.(*ast.BasicLit); ok && k.Kind == token.STRING && k.Value == `"error"` {
						add(kv.Value, `map "error"`)
					}
				}
			}
		case *ast.CallExpr:
			if typeName(x.Fun) == "RespondWithError" && len(x.Args) >= 3 {
				add(x.Args[2], "RespondWithError code")
			}
		}
		return true
	})
	return out
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether a file path is excluded from the scan (pure)
func skipFile(path string) bool {
	base := filepath.Base(path)
	return !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") || path == filepath.Join("api", "api.go")
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: scan a directory tree for string-literal error codes (reads filesystem)
func run(root string) ([]finding, error) {
	var all []finding
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error { //nolint:gosec // G703 - root is the developer-supplied repo root of a lint tool
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile(rel) {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		all = append(all, checkFile(fset, f)...)
		return nil
	})
	sort.Slice(all, func(i, j int) bool {
		if all[i].pos.Filename != all[j].pos.Filename {
			return all[i].pos.Filename < all[j].pos.Filename
		}
		return all[i].pos.Line < all[j].pos.Line
	})
	return all, err
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-error-codes <root>")
		os.Exit(2)
	}
	findings, err := run(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-error-codes:", err)
		os.Exit(2)
	}
	for _, f := range findings {
		fmt.Printf("%s:%d: string literal error code %s; use an internal/errcode constant\n", f.pos.Filename, f.pos.Line, f.lit)
	}
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "check-error-codes: %d violation(s)\n", len(findings))
		os.Exit(1)
	}
}
