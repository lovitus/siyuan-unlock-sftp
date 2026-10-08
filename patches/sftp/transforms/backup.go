package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// Identical repository initialization occurs in download and upload functions.
// Bind the lease provider only inside UploadCloudSnapshot, never by text offset.
func rewriteBackup(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "repository.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var target *ast.AssignStmt
	functions, matches := 0, 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "UploadCloudSnapshot" {
			continue
		}
		functions++
		if fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
				continue
			}
			repo, ok1 := assign.Lhs[0].(*ast.Ident)
			errName, ok2 := assign.Lhs[1].(*ast.Ident)
			call, ok3 := assign.Rhs[0].(*ast.CallExpr)
			if !ok1 || !ok2 || !ok3 || repo.Name != "repo" || errName.Name != "err" {
				continue
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || callee.Name != "newCloudRepositoryWithAssetSourceLocked" || len(call.Args) != 0 {
				continue
			}
			matches++
			target = assign
		}
	}
	if functions != 1 || matches != 1 {
		return nil, fmt.Errorf("UploadCloudSnapshot repository initialization missing, ambiguous or changed")
	}
	start, end := fset.Position(target.Pos()).Offset, fset.Position(target.End()).Offset
	out := append([]byte{}, src[:start]...)
	out = append(out, []byte("var backupProvider *sftpcloud.SFTP\n\trepo, err := newCloudRepositoryWithAssetSourceLocked(func(provider *sftpcloud.SFTP) { backupProvider = provider })")...)
	out = append(out, src[end:]...)
	if _, err := parser.ParseFile(token.NewFileSet(), "repository.go", out, 0); err != nil {
		return nil, err
	}
	return out, nil
}
