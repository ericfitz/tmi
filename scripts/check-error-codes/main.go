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
var errorTypes = map[string]bool{"Error": true, "OAuthError": true, "RequestError": true, "AuthError": true}

// skipDirs are directory names never scanned.
var skipDirs = map[string]bool{
	"testdata": true, "vendor": true, "node_modules": true, "wstest": true,
	"bin": true, "graphify-out": true,
}

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: return the first string literal inside an expression, ignoring function literals (pure)
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

// isTypedCode reports whether e is an errcode constant or a string() conversion of
// a typed value, the shapes allowed as a gin.H "error" value.
// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: report whether an expression is an errcode constant or a string conversion of one (pure)
func isTypedCode(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == "errcode"
	case *ast.CallExpr:
		f, ok := x.Fun.(*ast.Ident)
		if !ok || f.Name != "string" || len(x.Args) != 1 {
			return false
		}
		switch x.Args[0].(type) {
		case *ast.Ident, *ast.SelectorExpr:
			return true
		}
	}
	return false
}

// stringCodeParams flags string-typed parameters that a function writes into an
// error body ("error" map key or Error/Code field of an error type), because a
// string parameter lets callers pass any literal past the check.
// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: find string parameters used as the error code of an error body (pure)
func stringCodeParams(fset *token.FileSet, fn *ast.FuncDecl) []finding {
	if fn.Body == nil {
		return nil
	}
	params := map[string]*ast.Ident{}
	for _, f := range fn.Type.Params.List {
		if id, ok := f.Type.(*ast.Ident); ok && id.Name == "string" {
			for _, n := range f.Names {
				params[n.Name] = n
			}
		}
	}
	var out []finding
	seen := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok || cl.Type == nil {
			return true
		}
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			isCodeKey := false
			if k, ok := kv.Key.(*ast.BasicLit); ok && k.Kind == token.STRING && k.Value == `"error"` && isStringMapType(cl.Type) {
				isCodeKey = true
			}
			if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "Error" || k.Name == "Code") && errorTypes[typeName(cl.Type)] {
				isCodeKey = true
			}
			if !isCodeKey {
				continue
			}
			ast.Inspect(kv.Value, func(v ast.Node) bool {
				if id, ok := v.(*ast.Ident); ok {
					if p, ok := params[id.Name]; ok && !seen[id.Name] {
						seen[id.Name] = true
						out = append(out, finding{pos: fset.Position(p.Pos()), lit: `"` + id.Name + ` string"`, kind: "string code parameter"})
					}
				}
				return true
			})
		}
		return true
	})
	return out
}

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: return the unqualified name of a type expression (pure)
func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: report whether a composite literal type is a gin.H or string-keyed map (pure)
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

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: collect string-literal error codes in one parsed file (pure)
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
						if firstStringLit(kv.Value) != nil {
							add(kv.Value, `map "error"`)
						} else if !isTypedCode(kv.Value) {
							out = append(out, finding{pos: fset.Position(kv.Value.Pos()), lit: "<non-constant>", kind: `map "error" value`})
						}
					}
				}
			}
		case *ast.FuncDecl:
			out = append(out, stringCodeParams(fset, x)...)
		case *ast.CallExpr:
			if typeName(x.Fun) == "RespondWithError" && len(x.Args) >= 3 {
				add(x.Args[2], "RespondWithError code")
			}
		}
		return true
	})
	return out
}

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: report whether a file path is excluded from the scan (pure)
func skipFile(path string) bool {
	base := filepath.Base(path)
	// WebSocket message codes are out of scope (ADR decision 4).
	return !strings.HasSuffix(base, ".go") || strings.HasPrefix(path, filepath.Join("api", "websocket")) || strings.HasSuffix(base, "_test.go") || path == filepath.Join("api", "api.go")
}

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: scan a directory tree for string-literal error codes (reads filesystem)
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

// SEM@a11598e767bdaf64954a242d6939cc01535c5bfa: run the error-code literal check and exit non-zero on violations
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
