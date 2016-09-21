// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"os"
	"os/exec"
)

var fset = token.NewFileSet()

func handle(err error) {
	if err != nil {
		fmt.Println(os.Stderr, err)
		os.Exit(-2)
	}
}

func main() {
	flag.Parse()

	// parser file
	file, err := parser.ParseFile(fset, flag.Arg(0), nil, 0)
	handle(err)

	// rewrite operator method names
	ast.Apply(file, func(parent ast.Node, name string, index int, n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Recv != nil {
				if name, ok := methName[n.Name.Name]; ok {
					n.Name.Name = name
				}
			}
			return false // no need to go inside functions
		}
		return true
	}, nil)

	// rewrite operators
	for progress := true; ; {
		pkg, tmap, err := typecheck(file)
		if err == nil || !progress {
			break
		}
		progress = false
		ast.Apply(file, nil, func(parent ast.Node, name string, index int, n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if r := rewrite(pkg, tmap, n.X, n.Op.String(), n.Y); r != nil {
					ast.SetField(parent, name, index, r)
					progress = true
				}
			}
			return true
		})
	}

	// write AST
	buf := bytes.NewBuffer([]byte("// +build ignore\n\n")) // don't pollute directory with buildable files
	handle(format.Node(buf, fset, file))
	filename := "generated." + flag.Arg(0)
	handle(ioutil.WriteFile(filename, buf.Bytes(), 0666))

	// compile and run
	out, _ := exec.Command("go", "run", filename).CombinedOutput()
	fmt.Printf("%s", out)
}

func typecheck(file *ast.File) (*types.Package, map[ast.Expr]types.TypeAndValue, error) {
	conf := types.Config{Importer: importer.For("gc", nil), Error: func(error) {}}
	tmap := make(map[ast.Expr]types.TypeAndValue)
	pkg, err := conf.Check("pkg", fset, []*ast.File{file}, &types.Info{Types: tmap})
	return pkg, tmap, err
}

func rewrite(pkg *types.Package, tmap map[ast.Expr]types.TypeAndValue, recv ast.Expr, opname string, args ...ast.Expr) *ast.CallExpr {
	meth, _, _ := types.LookupFieldOrMethod(tmap[recv].Type, false, pkg, methName[opname])
	if _, ok := meth.(*types.Func); !ok {
		return nil // no method found
	}
	fun := &ast.SelectorExpr{X: recv, Sel: ast.NewIdent(meth.Name())}
	return &ast.CallExpr{Fun: fun, Args: args}
}

var methName = map[string]string{"+": "ADD__"} // extend as desired
