package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

func main() {
	// Add a -w flag to write to the file directly, similar to gofmt.
	writeFlag := flag.Bool("w", false, "write result to (source) file instead of stdout")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: clumpbools [-w] <file.go>")
		os.Exit(1)
	}

	filename := flag.Arg(0)
	fset := token.NewFileSet()

	// Parse the file and preserve comments.
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse file: %v\n", err)
		os.Exit(1)
	}

	// Traverse the AST looking for structs.
	ast.Inspect(node, func(n ast.Node) bool {
		// We are looking for Type Specifications:
		//
		//	type MyStruct struct {
		//		...
		//	}
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		// Ensure the type is a struct.
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		// Skip empty structs or structs with only one field.
		if structType.Fields == nil || len(structType.Fields.List) <= 1 {
			return true
		}

		var nonBools []*ast.Field
		var pgBools []*ast.Field
		var bools []*ast.Field

		// Split fields into:
		//
		// 1. everything else
		// 2. pgtype.Bool
		// 3. built-in bool
		for _, field := range structType.Fields.List {
			switch {
			case isPgtypeBool(field.Type):
				pgBools = append(pgBools, field)

			case isBuiltinBool(field.Type):
				bools = append(bools, field)

			default:
				nonBools = append(nonBools, field)
			}
		}

		// Reconstruct the fields:
		//
		//	non-bools
		//	pgtype.Bool
		//	bool
		fields := make([]*ast.Field, 0, len(structType.Fields.List))
		fields = append(fields, nonBools...)
		fields = append(fields, pgBools...)
		fields = append(fields, bools...)

		structType.Fields.List = fields

		// Fix up field positions so go/format doesn't insert blank lines.
		//
		// When fields are reordered their original source positions stay attached,
		// which can leave gaps (or backwards jumps) between consecutive entries in
		// the new list. go/format interprets those gaps as blank lines and emits
		// them. We fix this by ensuring each field starts on the line immediately
		// after the previous field ends.
		fixFieldPositions(fset, structType.Fields.List)

		return true
	})

	// Format the modified AST back into source code.
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to format node: %v\n", err)
		os.Exit(1)
	}

	// Output the result.
	if *writeFlag {
		fileInfo, err := os.Stat(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to stat file: %v\n", err)
			os.Exit(1)
		}

		if err := os.WriteFile(filename, buf.Bytes(), fileInfo.Mode()); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write file: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Reformatted %s\n", filename)
	} else {
		fmt.Print(buf.String())
	}
}

// isBuiltinBool reports whether expr is the built-in bool type:
//
//	bool
func isBuiltinBool(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "bool"
}

// isPgtypeBool reports whether expr is syntactically:
//
//	pgtype.Bool
//
// Note that this is purely syntactic. It does not resolve imports, so an
// aliased import such as:
//
//	import pgt "github.com/jackc/pgx/v5/pgtype"
//
// followed by:
//
//	pgt.Bool
//
// will not be detected.
func isPgtypeBool(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	return pkg.Name == "pgtype" && sel.Sel.Name == "Bool"
}

// fixFieldPositions ensures that consecutive fields in the reordered list have
// no line gaps between them, preventing go/format from inserting blank lines.
//
// go/format (via go/printer) compares the line numbers of consecutive tokens
// and emits a blank line whenever the gap exceeds one line. After reordering,
// fields that moved earlier in the list may still carry their original
// (higher) line positions, causing gaps that trigger spurious blank lines.
//
// We fix this by moving any out-of-place field's start position to the line
// immediately after the previous field ends.
func fixFieldPositions(fset *token.FileSet, fields []*ast.Field) {
	for i := 1; i < len(fields); i++ {
		prev := fields[i-1]
		curr := fields[i]

		prevEndLine := fset.Position(prev.End()).Line
		currStartLine := fset.Position(curr.Pos()).Line

		if currStartLine == prevEndLine+1 {
			continue // already on the very next line — nothing to fix
		}

		file := fset.File(prev.End())
		if file == nil {
			continue
		}

		targetLine := prevEndLine + 1
		if targetLine > file.LineCount() {
			continue
		}

		// LineStart returns the Pos of the first character on that line.
		// We use it directly; the exact column within the line doesn't affect
		// which line number the formatter sees.
		newPos := file.LineStart(targetLine)

		setFieldStartPos(curr, newPos)
	}
}

// setFieldStartPos moves the leading token of a struct field to pos.
func setFieldStartPos(field *ast.Field, pos token.Pos) {
	if len(field.Names) > 0 {
		field.Names[0].NamePos = pos
		return
	}

	// Embedded / anonymous field: update the type expression instead.
	setExprStartPos(field.Type, pos)
}

// setExprStartPos moves the first token of an expression to pos.
// Only the most common struct-field type forms need to be handled here.
func setExprStartPos(expr ast.Expr, pos token.Pos) {
	switch e := expr.(type) {
	case *ast.Ident:
		e.NamePos = pos

	case *ast.StarExpr:
		// *T
		e.Star = pos

	case *ast.SelectorExpr:
		// pkg.T
		setExprStartPos(e.X, pos)

	case *ast.ArrayType:
		// []T / [N]T
		e.Lbrack = pos

	case *ast.MapType:
		// map[K]V
		e.Map = pos
	}
}
