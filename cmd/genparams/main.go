package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func main() {
	filename := os.Getenv("GOFILE")
	if filename == "" {
		fmt.Println("GOFILE environment variable not set")
		os.Exit(1)
	}
	src, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	// First, build a set of all existing function names on the *Tools type.
	existingFuncs := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Check if it's a method on *Tools
		if fn.Recv != nil && len(fn.Recv.List) == 1 {
			if starExpr, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if ident, ok := starExpr.X.(*ast.Ident); ok && ident.Name == "Tools" {
					existingFuncs[fn.Name.Name] = true
				}
			}
		}
	}

	var inserts []struct {
		offset int
		code   string
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			// Look for //go:generate:params marker
			if gen.Doc != nil {
				for _, comment := range gen.Doc.List {
					if strings.Contains(comment.Text, "go:generate:params") {
						funcName := strings.TrimSuffix(ts.Name.Name, "Params")
						if _, exists := existingFuncs[funcName]; exists {
							// Function already exists, skip generation.
							continue
						}

						funcName = strings.TrimSuffix(ts.Name.Name, "Params")
						end := fset.Position(ts.End()).Offset
						funcDef := fmt.Sprintf(
							"\n\n// %s is a generated function. Implement me.\nfunc (t *Tools) %s(ctx context.Context, toolReq *mcp.CallToolRequest, params %s) (*mcp.CallToolResult, any, error) {\n\t// TODO: implement\n\treturn nil, nil, fmt.Errorf(\"tool %s not implemented\")\n}\n",
							funcName,
							funcName, ts.Name.Name,
							funcName,
						)
						inserts = append(inserts, struct {
							offset int
							code   string
						}{end, funcDef})
					}
				}
			}
		}
	}

	// Insert in reverse order to not mess up offsets
	out := src
	for i := len(inserts) - 1; i >= 0; i-- {
		ins := inserts[i]
		out = append(out[:ins.offset], append([]byte(ins.code), out[ins.offset:]...)...)
	}

	if err := os.WriteFile(filename, out, 0644); err != nil {
		panic(err)
	}
}
