// SPDX-License-Identifier: AGPL-3.0-or-later
// Replace only the implementation of the registered cloud-user handler.
// The API contract and authorization callback must remain unchanged.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

func rewrite(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "setting.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var target *ast.FuncLit
	count := 0
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for _, name := range value.Names {
				if name.Name != "getCloudUser" {
					continue
				}
				count++
				if len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				call, ok := value.Values[0].(*ast.CallExpr)
				if !ok || len(call.Args) != 3 {
					continue
				}
				var callee, contract bytes.Buffer
				format.Node(&callee, fset, call.Fun)
				format.Node(&contract, fset, call.Args[0])
				if callee.String() != "contractHandler" || contract.String() != "apicontract.GetCloudUser" {
					continue
				}
				handler, ok := call.Args[1].(*ast.FuncLit)
				if !ok {
					continue
				}
				if _, ok := call.Args[2].(*ast.FuncLit); !ok {
					continue
				}
				var signature bytes.Buffer
				format.Node(&signature, fset, handler.Type)
				if signature.String() != "func(c *gin.Context, request apicontract.SettingCloudUserRequest) apicontract.Response[*apicontract.SettingUser]" {
					continue
				}
				target = handler
			}
		}
	}
	if count != 1 || target == nil {
		return nil, fmt.Errorf("getCloudUser contract missing, ambiguous or changed; compatibility review required")
	}
	start, end := fset.Position(target.Body.Pos()).Offset, fset.Position(target.Body.End()).Offset
	out := append([]byte{}, src[:start]...)
	out = append(out, []byte("{\n\treturn sftpLocalCloudUser()\n}")...)
	out = append(out, src[end:]...)
	if _, err := parser.ParseFile(token.NewFileSet(), "setting.go", out, 0); err != nil {
		return nil, err
	}
	return out, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: apply-source setting.go repository.go")
		os.Exit(2)
	}
	for i, transform := range []func([]byte) ([]byte, error){rewrite, rewriteBackup} {
		path := os.Args[i+1]
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		out, err := transform(src)
		if err != nil {
			fmt.Fprintln(os.Stderr, "PATCH_CONFLICT:", err)
			os.Exit(42)
		}
		if err := os.WriteFile(path, out, 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
