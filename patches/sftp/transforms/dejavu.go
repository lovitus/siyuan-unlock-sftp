package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
)

// Keep AddTag serialized with purge, while its already-locked caller uses the
// same implementation without recursively acquiring the repository mutex.
func repairDejavu(backup, ref []byte) ([]byte, []byte, bool) {
	b, r := parseSource(backup), parseSource(ref)
	download, add := b.fn("DownloadTagIndex"), r.fn("AddTag")
	addCalls := calls(download.Body, "AddTag")
	if len(addCalls) == 0 || len(calls(add.Body, "Lock")) == 0 || len(calls(download.Body, "Lock")) == 0 {
		return backup, ref, false // Older releases or an upstream repair.
	}
	if len(addCalls) != 1 {
		panic("DejaVu DownloadTagIndex has ambiguous AddTag calls")
	}
	locked := func(s *sourceFile, fn *ast.FuncDecl) string {
		if len(fn.Body.List) < 3 || len(calls(fn.Body, "Lock")) != 1 || len(calls(fn.Body, "Unlock")) != 1 {
			panic("DejaVu lock contract changed")
		}
		lock, ok := fn.Body.List[0].(*ast.ExprStmt)
		if !ok {
			panic("DejaVu lock must precede all repository operations")
		}
		unlock, ok := fn.Body.List[1].(*ast.DeferStmt)
		if !ok {
			panic("DejaVu unlock must be deferred")
		}
		name := strings.TrimSuffix(s.text(lock.X), ".Lock()")
		if s.text(lock.X) != name+".Lock()" || s.text(unlock.Call) != name+".Unlock()" {
			panic("DejaVu lock/unlock pair changed")
		}
		return name
	}
	mutex := locked(r, add)
	if locked(b, download) != mutex {
		return backup, ref, false // No recursive acquisition of the same mutex.
	}
	const helper = "sftpAddTagLocked"
	if len(nodes(r.file, func(n ast.Node) bool { return ident(n, helper) })) != 0 {
		panic("DejaVu repair helper already exists with an unexpected caller")
	}
	call := addCalls[0].(*ast.CallExpr)
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || r.text(add.Type.Params) != "(id, tag string)" || len(add.Recv.List[0].Names) != 1 {
		panic("DejaVu AddTag signature changed")
	}
	receiver := add.Recv.List[0].Names[0].Name
	if b.text(selector.X) != receiver || b.text(call) != receiver+".AddTag(id, tag)" {
		panic("DejaVu AddTag call changed")
	}
	// Copy the upstream body verbatim, removing only the leading lock pair.
	body := string(ref[r.at(add.Body.List[1].End()):r.at(add.Body.Rbrace)])
	declaration := strings.Replace(r.text(add), "AddTag(", helper+"(", 1)
	header := declaration[:strings.Index(declaration, "{")]
	r.insert(add.End(), "\n\n"+header+"{"+body+"}\n")
	r.replace(add.Body, fmt.Sprintf("{\n%s.Lock()\ndefer %s.Unlock()\nreturn %s.%s(id, tag)\n}", mutex, mutex, receiver, helper))
	b.replace(selector.Sel, helper)
	return b.finish(), r.finish(), true
}

func repairDejavuDirectory(root string) {
	read := func(name string) []byte {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			panic(err)
		}
		return source
	}
	backup, ref, changed := repairDejavu(read("backup.go"), read("ref.go"))
	if !changed {
		fmt.Println("unchanged")
		return
	}
	for name, source := range map[string][]byte{"backup.go": backup, "ref.go": ref} {
		if err := os.WriteFile(filepath.Join(root, name), source, 0644); err != nil {
			panic(err)
		}
	}
	fmt.Println("repaired")
}
