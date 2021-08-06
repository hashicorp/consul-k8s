// Parses golang code looking for github.com/hashicorp/consul/api.NewClient()
// being used in non-test code. If it finds this, it will error.
// The purpose of this lint is that we actually want to use our internal
// github.com/hashicorp/consul-k8s/consul.NewClient() function because that
// adds the consul-k8s version as a header.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var (
	broken           = make(map[string]bool) // Stored in a map for deduplication
	exitCode         = 0
	fset             = token.NewFileSet()
	consulApiPackage = "github.com/hashicorp/consul/api"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("failed to get cwd: %v", err))
		os.Exit(1)
	}
	err = walkDir(dir)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	if len(broken) > 0 {
		exitCode = 1
		_, _ = os.Stderr.WriteString("Found code using github.com/hashicorp/consul/api.NewClient()\ninstead of github.com/hashicorp/consul-k8s/consul.NewClient()\nin the following files:\n")
		for filePath := range broken {
			_, _ = os.Stderr.WriteString(fmt.Sprintf("-  %s\n", filePath))
		}
	}
	os.Exit(exitCode)
}

type visitor struct {
	path  string
	alias string
}

func (v visitor) Visit(n ast.Node) ast.Visitor {
	switch node := n.(type) {
	case *ast.CallExpr:
		function, ok := node.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		pkg, ok := function.X.(*ast.Ident)
		if !ok {
			break
		}
		if !(pkg.Name == v.alias && function.Sel.Name == "NewClient") {
			break
		}
		broken[v.path] = true
	}
	return v
}

// imports returns true if file imports pkg and the name of the alias
// used to import.
func imports(file *ast.File, pkgName string) (bool, string) {
	var specs []ast.Spec

	for _, decl := range file.Decls {
		if general, ok := decl.(*ast.GenDecl); ok {
			specs = append(specs, general.Specs...)
		}
	}
	for _, spec := range specs {
		pkg, ok := spec.(*ast.ImportSpec)
		if !ok {
			continue
		}
		path := pkg.Path.Value
		// path may have leading/trailing quotes.
		path = strings.Trim(path, "\"")
		if path == pkgName {
			alias := filepath.Base(pkgName)
			if pkg.Name != nil {
				alias = pkg.Name.Name
			}
			return true, alias
		}
	}
	return false, ""
}

func walkDir(path string) error {
	return filepath.Walk(path, visitFile)
}

func visitFile(path string, f os.FileInfo, err error) error {
	if err != nil {
		return fmt.Errorf("failed to visit '%s', %v", path, err)
	}

	// consul/consul.go is where we have our re-implementation of NewClient()
	// which under the hood calls api.NewClient() so we need to discard that
	// path.
	if isNonTestFile(path, f) && !strings.Contains(path, "consul/consul.go") {
		tree, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		// Only process files importing github.com/hashicorp/consul/api.
		importsAPI, alias := imports(tree, consulApiPackage)
		if importsAPI {
			v := visitor{
				path:  path,
				alias: alias,
			}
			ast.Walk(v, tree)
		}
	}
	return nil
}

func isNonTestFile(path string, f os.FileInfo) bool {
	return !f.IsDir() && !strings.Contains(path, "test") && filepath.Ext(path) == ".go"
}
