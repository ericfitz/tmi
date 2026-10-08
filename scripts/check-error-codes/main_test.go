package main

import (
	"go/parser"
	"go/token"
	"testing"
)

func findingsFor(t *testing.T, path string) []finding {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return checkFile(fset, f)
}

func TestCheckErrorCodesBad(t *testing.T) {
	got := findingsFor(t, "testdata/bad.go.txt")
	if len(got) != 6 {
		t.Fatalf("bad fixture: got %d findings, want 6: %+v", len(got), got)
	}
}

func TestCheckErrorCodesGood(t *testing.T) {
	if got := findingsFor(t, "testdata/good.go.txt"); len(got) != 0 {
		t.Fatalf("good fixture: got %d findings, want 0: %+v", len(got), got)
	}
}

func TestSkipFile(t *testing.T) {
	for path, want := range map[string]bool{
		"api/api.go": true, "api/foo_test.go": true, "api/foo.go": false, "README.md": true,
	} {
		if skipFile(path) != want {
			t.Errorf("skipFile(%q) = %v, want %v", path, !want, want)
		}
	}
}
