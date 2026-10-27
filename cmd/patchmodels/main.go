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
	"slices"
)

func main() {
	write := flag.Bool("w", false, "write result to (source) file instead of stdout")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: patchmodels [-w] <file.go>")
		os.Exit(1)
	}
	if err := run(flag.Arg(0), *write); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(filename string, write bool) error {
	source, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	result, err := patchModels(source)
	if err != nil {
		return err
	}
	if !write {
		_, err = os.Stdout.Write(result)
		return err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, result, info.Mode())
}

func patchModels(source []byte) ([]byte, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var property *ast.StructType
	ast.Inspect(node, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok && spec.Name.Name == "Property" {
			property, _ = spec.Type.(*ast.StructType)
			return false
		}
		return true
	})
	if property == nil {
		return nil, fmt.Errorf("Property struct not found")
	}
	fields := property.Fields.List
	domainIndex := -1
	asciiIndex := -1
	for i, field := range fields {
		for _, name := range field.Names {
			if name.Name == "Domain" {
				domainIndex = i
			} else if name.Name == "DomainASCII" {
				asciiIndex = i
			}
		}
	}
	if domainIndex < 0 {
		return nil, fmt.Errorf("Property.Domain field not found")
	}
	const tag = "`db:\"-\" json:\"-\"`"
	if asciiIndex >= 0 {
		field := fields[asciiIndex]
		typ, ok := field.Type.(*ast.Ident)
		if asciiIndex != domainIndex+1 || len(field.Names) != 1 || !ok || typ.Name != "string" || field.Tag == nil || field.Tag.Value != tag {
			return nil, fmt.Errorf("unexpected Property.DomainASCII field")
		}
	} else {
		domain := fields[domainIndex]
		field := &ast.Field{
			Names: []*ast.Ident{{NamePos: domain.Pos(), Name: "DomainASCII"}},
			Type:  &ast.Ident{NamePos: domain.Type.Pos(), Name: "string"},
			Tag:   &ast.BasicLit{ValuePos: domain.End(), Kind: token.STRING, Value: tag},
		}
		property.Fields.List = slices.Insert(fields, domainIndex+1, field)
	}
	var result bytes.Buffer
	if err := format.Node(&result, fset, node); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}
