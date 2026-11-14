// Command stylecheck enforces the style rules no golangci-lint linter covers
// (D-043 rules 7 to 9):
//
//   - a function body is never on one line;
//   - a map or slice literal with 3 or more items has one item per line;
//   - a local variable is never a single letter, except `c := require.New(t)`
//     in tests (D-030) and the loop counter `i`.
//
// It prints every violation and exits 1. With -fix it rewrites the first two
// rules in place (run gofmt afterwards); single-letter names are renamed by hand
// or with gopls, because a rename needs type information.
package main

import (
	"bytes"
	"flag"
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

const (
	minItemsPerLine = 3
	testSuffix      = "_test.go"
	goSuffix        = ".go"
	allowedTestVar  = "c"
	allowedCounter  = "i"
	blank           = "_"
	exitViolations  = 1
	exitError       = 2
	sourceFileMode  = 0o644 // only used if the file is created; existing files keep their mode
)

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

type violation struct {
	pos  token.Position
	rule string
}

func main() {
	fix := flag.Bool("fix", false, "rewrite one-line bodies and literals in place")
	flag.Parse()

	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	var found []violation

	for _, root := range roots {
		files, err := listGoFiles(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(exitError)
		}

		for _, path := range files {
			violations, err := checkFile(path, *fix)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(exitError)
			}

			found = append(found, violations...)
		}
	}

	for _, issue := range found {
		fmt.Printf("%s: %s\n", issue.pos, issue.rule)
	}

	if len(found) > 0 {
		os.Exit(exitViolations)
	}
}

func listGoFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") && path != root {
			return filepath.SkipDir
		}

		if !entry.IsDir() && strings.HasSuffix(path, goSuffix) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

func checkFile(path string, fix bool) ([]violation, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var (
		found []violation
		edits []edit
	)

	isTest := strings.HasSuffix(path, testSuffix)

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BlockStmt:
			found, edits = checkBody(fset, src, typed, found, edits)
		case *ast.CompositeLit:
			found, edits = checkLiteral(fset, src, typed, found, edits)
		case *ast.AssignStmt:
			found = checkAssign(fset, typed, isTest, found)
		case *ast.RangeStmt:
			found = checkRange(fset, typed, found)
		case *ast.ValueSpec:
			found = checkNames(fset, typed.Names, found)
		}

		return true
	})

	if fix && len(edits) > 0 {
		return found, os.WriteFile(path, applyEdits(src, edits), sourceFileMode)
	}

	return found, nil
}

// checkBody flags a function body opened and closed on the same line.
func checkBody(fset *token.FileSet, src []byte, body *ast.BlockStmt, found []violation, edits []edit) ([]violation, []edit) {
	open, closing := fset.Position(body.Lbrace), fset.Position(body.Rbrace)
	if open.Line != closing.Line || len(body.List) == 0 {
		return found, edits
	}

	inner := strings.TrimSpace(string(src[open.Offset+1 : closing.Offset]))
	found = append(found, violation{open, "function or block body on one line"})
	edits = append(edits, edit{open.Offset + 1, closing.Offset, "\n" + inner + "\n"})

	return found, edits
}

// checkLiteral flags a map or slice literal with 3+ items on one line.
func checkLiteral(fset *token.FileSet, src []byte, lit *ast.CompositeLit, found []violation, edits []edit) ([]violation, []edit) {
	if !isMapOrSlice(lit.Type) || len(lit.Elts) < minItemsPerLine {
		return found, edits
	}

	open, closing := fset.Position(lit.Lbrace), fset.Position(lit.Rbrace)
	if open.Line != closing.Line {
		return found, edits
	}

	var lines strings.Builder

	lines.WriteString("\n")

	for _, item := range lit.Elts {
		start, end := fset.Position(item.Pos()).Offset, fset.Position(item.End()).Offset
		lines.WriteString(string(src[start:end]) + ",\n")
	}

	found = append(found, violation{open, fmt.Sprintf("literal with %d items on one line", len(lit.Elts))})
	edits = append(edits, edit{open.Offset + 1, closing.Offset, lines.String()})

	return found, edits
}

func isMapOrSlice(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.MapType, *ast.ArrayType:
		return true
	default:
		return false
	}
}

func checkAssign(fset *token.FileSet, assign *ast.AssignStmt, isTest bool, found []violation) []violation {
	if assign.Tok != token.DEFINE {
		return found
	}

	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || !isShort(ident.Name) || ident.Name == allowedCounter {
			continue
		}

		if isTest && ident.Name == allowedTestVar {
			continue
		}

		found = append(found, violation{fset.Position(ident.Pos()), fmt.Sprintf("single-letter variable %q", ident.Name)})
	}

	return found
}

func checkRange(fset *token.FileSet, rng *ast.RangeStmt, found []violation) []violation {
	for _, expr := range []ast.Expr{rng.Key, rng.Value} {
		ident, ok := expr.(*ast.Ident)
		if !ok || !isShort(ident.Name) || ident.Name == allowedCounter {
			continue
		}

		found = append(found, violation{fset.Position(ident.Pos()), fmt.Sprintf("single-letter variable %q", ident.Name)})
	}

	return found
}

func checkNames(fset *token.FileSet, names []*ast.Ident, found []violation) []violation {
	for _, ident := range names {
		if isShort(ident.Name) {
			found = append(found, violation{fset.Position(ident.Pos()), fmt.Sprintf("single-letter variable %q", ident.Name)})
		}
	}

	return found
}

func isShort(name string) bool {
	return len(name) == 1 && name != blank
}

// applyEdits applies the outermost edits; nested ones are fixed on the next run.
func applyEdits(src []byte, edits []edit) []byte {
	sort.Slice(edits, func(a, b int) bool {
		return edits[a].start < edits[b].start
	})

	var (
		out  bytes.Buffer
		last int
	)

	for _, ed := range edits {
		if ed.start < last {
			continue
		}

		out.Write(src[last:ed.start])
		out.WriteString(ed.text)
		last = ed.end
	}

	out.Write(src[last:])

	return out.Bytes()
}
