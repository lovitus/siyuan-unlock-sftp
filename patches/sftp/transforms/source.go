package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type edit struct {
	start, end int
	text       string
}
type sourceFile struct {
	src   []byte
	file  *ast.File
	fs    *token.FileSet
	edits []edit
}

func parseSource(src []byte) *sourceFile {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "source.go", src, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	return &sourceFile{src, f, fs, nil}
}
func (s *sourceFile) at(pos token.Pos) int   { return s.fs.Position(pos).Offset }
func (s *sourceFile) text(n ast.Node) string { return string(s.src[s.at(n.Pos()):s.at(n.End())]) }
func (s *sourceFile) replace(n ast.Node, t string) {
	s.edits = append(s.edits, edit{s.at(n.Pos()), s.at(n.End()), t})
}
func (s *sourceFile) insert(pos token.Pos, t string) {
	s.edits = append(s.edits, edit{s.at(pos), s.at(pos), t})
}
func nodes(root ast.Node, pred func(ast.Node) bool) []ast.Node {
	var ret []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n != nil && pred(n) {
			ret = append(ret, n)
		}
		return true
	})
	return ret
}
func one(ns []ast.Node) ast.Node {
	if len(ns) != 1 {
		panic(fmt.Errorf("expected one structural anchor, got %d", len(ns)))
	}
	return ns[0]
}
func (s *sourceFile) fn(name string) *ast.FuncDecl {
	return one(nodes(s.file, func(n ast.Node) bool { f, ok := n.(*ast.FuncDecl); return ok && f.Name.Name == name })).(*ast.FuncDecl)
}
func (s *sourceFile) importPath(path string) {
	for _, i := range s.file.Imports {
		if i.Path.Value == strconv.Quote(path) {
			return
		}
	}
	s.insert(s.file.Name.End(), "\nimport "+strconv.Quote(path)+"\n")
}
func ident(n ast.Node, name string) bool { i, ok := n.(*ast.Ident); return ok && i.Name == name }
func calls(root ast.Node, name string) []ast.Node {
	return nodes(root, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return false
		}
		if ident(c.Fun, name) {
			return true
		}
		p, ok := c.Fun.(*ast.SelectorExpr)
		return ok && p.Sel.Name == name
	})
}
func (s *sourceFile) field(structName, field string) {
	st := one(nodes(s.file, func(n ast.Node) bool { t, ok := n.(*ast.TypeSpec); return ok && t.Name.Name == structName })).(*ast.TypeSpec).Type.(*ast.StructType)
	s.insert(st.Fields.Opening+1, "\n"+field+"\n")
}
func (s *sourceFile) defaults(typeName string, values map[string]string) {
	lit := one(nodes(s.file, func(n ast.Node) bool { v, ok := n.(*ast.CompositeLit); return ok && ident(v.Type, typeName) })).(*ast.CompositeLit)
	for key, value := range values {
		kv := one(nodes(lit, func(n ast.Node) bool { v, ok := n.(*ast.KeyValueExpr); return ok && ident(v.Key, key) })).(*ast.KeyValueExpr)
		s.replace(kv.Value, value)
	}
}
func (s *sourceFile) caseFor(fn, provider string) *ast.CaseClause {
	seen := map[string]bool{}
	for !seen[fn] {
		seen[fn] = true
		f := s.fn(fn)
		cases := nodes(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CaseClause)
			if !ok {
				return false
			}
			for _, v := range c.List {
				if s.text(v) == provider {
					return true
				}
			}
			return false
		})
		if len(cases) > 0 {
			return one(cases).(*ast.CaseClause)
		}
		// Follow a transparent forwarding function when upstream moves its body.
		if len(f.Body.List) != 1 {
			break
		}
		ret, ok := f.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			break
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			break
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok {
			break
		}
		fn = name.Name
	}
	panic(fmt.Errorf("%s: missing provider branch %s", fn, provider))
}
func (s *sourceFile) addCase(fn, anchor, code string) {
	s.insert(s.caseFor(fn, anchor).Pos(), code+"\n")
}
func (s *sourceFile) extendCase(fn, anchor string) {
	c := s.caseFor(fn, anchor)
	s.insert(c.Colon, ", conf.ProviderSFTP")
}
func (s *sourceFile) finish() []byte {
	sort.SliceStable(s.edits, func(i, j int) bool { return s.edits[i].start > s.edits[j].start })
	out := append([]byte{}, s.src...)
	boundary := len(out)
	for _, e := range s.edits {
		if e.end > boundary {
			panic("overlapping structural changes")
		}
		out = append(append(append([]byte{}, out[:e.start]...), []byte(e.text)...), out[e.end:]...)
		boundary = e.start
	}
	formatted, err := format.Source(out)
	if err != nil {
		panic(err)
	}
	return formatted
}
func mutate(root, path string, fn func(*sourceFile)) {
	defer func() {
		if failure := recover(); failure != nil {
			panic(fmt.Errorf("%s: %v", path, failure))
		}
	}()
	full := filepath.Join(root, path)
	src, err := os.ReadFile(full)
	if err != nil {
		panic(err)
	}
	s := parseSource(src)
	fn(s)
	if err = os.WriteFile(full, s.finish(), 0644); err != nil {
		panic(err)
	}
}
func applySource(root string) {
	mutate(root, "kernel/conf/appearance.go", func(s *sourceFile) {
		s.defaults("Appearance", map[string]string{"Lang": "\"zh_CN\"", "CloseButtonBehavior": "1"})
	})
	mutate(root, "kernel/conf/system.go", func(s *sourceFile) { s.defaults("System", map[string]string{"DownloadInstallPkg": "false"}) })
	mutate(root, "kernel/conf/sync.go", func(s *sourceFile) {
		s.field("Sync", "SFTP *SFTP `json:\"sftp\"`")
		s.defaults("Sync", map[string]string{"GenerateConflictDoc": "true", "Provider": "ProviderS3"})
		s.addCase("ProviderToStr", "ProviderSiYuan", "case ProviderSFTP: return \"SFTP\"")
	})
	mutate(root, "kernel/apicontract/system_conf.go", func(s *sourceFile) {
		s.field("SystemSync", "SFTP *SystemSFTP `json:\"sftp\" api:\"optional,nullable\"`")
	})
	mutate(root, "kernel/api/router.go", func(s *sourceFile) {
		c := one(nodes(s.file, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return false
			}
			for _, a := range c.Args {
				if v, ok := a.(*ast.BasicLit); ok && v.Value == "\"/api/sync/setSyncProviderWebDAV\"" {
					return true
				}
			}
			return false
		}))
		s.importPath("github.com/siyuan-note/siyuan/kernel/model")
		route := `ginServer.Handle("POST", "/api/sync/setSyncProviderSFTP", model.CheckAuth, model.CheckAdminRole, model.CheckReadonly, setSyncProviderSFTP)`
		s.insert(c.End(), "\n"+route)
	})
	mutate(root, "kernel/model/cloud_account_auth.go", func(s *sourceFile) {
		cond := one(nodes(s.fn("cloudAccountAuthFailureHandler"), func(n ast.Node) bool { v, ok := n.(*ast.IfStmt); return ok && s.text(v.Cond) == "requestedUser == nil" })).(*ast.IfStmt).Cond
		s.replace(cond, s.text(cond)+" || (requestedUser.UserId == \"0\" && requestedUser.UserToken == \"token\")")
	})
	mutate(root, "kernel/model/cloud_service.go", func(s *sourceFile) {
		stmt := one(nodes(s.fn("RefreshCheckJob2H"), func(n ast.Node) bool { g, ok := n.(*ast.GoStmt); return ok && ident(g.Call.Fun, "refreshUser") }))
		s.replace(stmt, "")
	})
	mutate(root, "kernel/model/asset_download.go", func(s *sourceFile) {
		s.extendCase("checkAssetDownloadAccess", "conf.ProviderLocal")
		s.addCase("assetDownloadScope", "conf.ProviderLocal", "case conf.ProviderSFTP: identity = append(identity, c.Endpoint)")
	})
	mutate(root, "kernel/model/lan_sync.go", func(s *sourceFile) {
		s.addCase("lanSyncScope", "conf.ProviderLocal", "case conf.ProviderSFTP: return base + \":\" + GetSFTPConfig().Identity()")
	})
	mutate(root, "kernel/model/repository.go", func(s *sourceFile) {
		s.importPath("github.com/siyuan-note/siyuan/kernel/sftpcloud")
		for _, f := range []string{"DownloadCloudSnapshot", "UploadCloudSnapshot", "RemoveCloudRepoTag", "GetCloudRepoTagSnapshots", "GetCloudRepoSnapshots"} {
			s.extendCase(f, "conf.ProviderLocal")
		}
		for _, f := range []string{"newCloudRepositoryWithAssetSourceLocked", "newRepositoryWithAssetSourceLocked"} {
			p := s.fn(f).Type.Params
			if len(p.List) != 0 {
				panic("repository parameters changed")
			}
			s.insert(p.Opening+1, "captureSFTP ...func(*sftpcloud.SFTP)")
		}
		call := one(calls(s.fn("newCloudRepositoryWithAssetSourceLocked"), "newRepositoryWithAssetSourceLocked")).(*ast.CallExpr)
		s.insert(call.Lparen+1, "captureSFTP...")
		s.addCase("newRepositoryWithAssetSourceLocked", "conf.ProviderLocal", `case conf.ProviderSFTP:
   var provider *sftpcloud.SFTP
   provider, err = sftpcloud.New(&cloud.BaseCloud{Conf: cloudConf}, GetSFTPConfig())
   if err != nil { return }
   cloudRepo = provider
   for _,capture := range captureSFTP { capture(provider) }`)
		s.addCase("buildCloudConf", "conf.ProviderLocal", `case conf.ProviderSFTP: ret.Endpoint = GetSFTPConfig().Identity()`)
		upload := s.fn("UploadCloudSnapshot")
		call = one(calls(upload, "newCloudRepositoryWithAssetSourceLocked")).(*ast.CallExpr)
		s.insert(upload.Body.Lbrace+1, "\nvar backupProvider *sftpcloud.SFTP\n")
		s.insert(call.Lparen+1, "func(provider *sftpcloud.SFTP) {backupProvider = provider}")
		tagged := one(calls(upload, "UploadTagIndex"))
		s.replace(tagged, "withSFTPBackupLease(backupProvider, func()(int,int,int64,error){return "+s.text(tagged)+"})")
	})
	mutate(root, "kernel/model/sync.go", func(s *sourceFile) {
		for _, f := range []string{"checkSync", "CreateCloudSyncDir", "RemoveCloudSyncDir"} {
			s.extendCase(f, "conf.ProviderLocal")
		}
	})
	mutate(root, "kernel/model/updater.go", func(s *sourceFile) {
		body := s.fn("skipNewVerInstallPkg").Body
		s.replace(body, "{ return true } // SFTP builds must retain their provider.")
		// Drop imports used exclusively by the removed downloader policy.
		for _, item := range s.file.Imports {
			imported, _ := strconv.Unquote(item.Path.Value)
			name := filepath.Base(imported)
			if item.Name != nil {
				name = item.Name.Name
			}
			if name == "_" || name == "." {
				continue
			}
			refs := nodes(s.file, func(n ast.Node) bool { p, ok := n.(*ast.SelectorExpr); return ok && ident(p.X, name) })
			if len(refs) == 0 {
				continue
			}
			elsewhere := false
			for _, ref := range refs {
				if ref.Pos() < body.Pos() || ref.End() > body.End() {
					elsewhere = true
				}
			}
			if !elsewhere {
				s.replace(item, "")
			}
		}
	})
	mutate(root, "kernel/model/updater_release.go", func(s *sourceFile) {
		for name, value := range map[string]string{"githubReleasesURL": "https://api.github.com/repos/lovitus/siyuan-unlock-sftp/releases?per_page=100", "githubReleaseURLPrefix": "https://github.com/lovitus/siyuan-unlock-sftp/releases/tag/v"} {
			v := one(nodes(s.file, func(n ast.Node) bool {
				v, ok := n.(*ast.ValueSpec)
				return ok && len(v.Names) == 1 && v.Names[0].Name == name
			})).(*ast.ValueSpec)
			s.replace(v.Values[0], strconv.Quote(value))
		}
		stmt := one(nodes(s.fn("getUpdateRelease"), func(n ast.Node) bool {
			v, ok := n.(*ast.IfStmt)
			return ok && len(calls(v, "getStableUpdateRelease")) == 1
		}))
		s.replace(stmt, "")
	})
	// Choose the configuration adapter by the actual API shape, not by version.
	data, err := os.ReadFile(filepath.Join(root, "kernel/model/conf.go"))
	if err != nil {
		panic(err)
	}
	s := parseSource(data)
	st := one(nodes(s.file, func(n ast.Node) bool { t, ok := n.(*ast.TypeSpec); return ok && t.Name.Name == "AppConf" })).(*ast.TypeSpec).Type.(*ast.StructType)
	field := one(nodes(st, func(n ast.Node) bool {
		f, ok := n.(*ast.Field)
		return ok && len(f.Names) == 1 && f.Names[0].Name == "Sync"
	})).(*ast.Field)
	var adapter string
	switch s.text(field.Type) {
	case "ConfSection[conf.Sync]":
		adapter = `func sftpSyncConfig() *conf.Sync { return Conf.Sync.Load() }
 func storeSFTPConfig(config *conf.SFTP) { UpdateSyncConf(func(value *conf.Sync) { value.SFTP = config }) }`
	case "*conf.Sync":
		adapter = `func sftpSyncConfig() *conf.Sync { return Conf.Sync }
 func storeSFTPConfig(config *conf.SFTP) { Conf.Sync.SFTP = config; Conf.Save() }`
	default:
		panic("unsupported sync configuration API")
	}
	out, err := format.Source([]byte("package model\nimport \"github.com/siyuan-note/siyuan/kernel/conf\"\n" + adapter))
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(filepath.Join(root, "kernel/model/sftp_config_adapter.go"), out, 0644); err != nil {
		panic(err)
	}
}
