package main

import (
	"strings"
	"testing"
)

const dejavuBackup = `package dejavu
func (repo *Repo) DownloadTagIndex(tag, id string, context map[string]interface{}) (err error) {
 lock.Lock()
 defer lock.Unlock()
 err = repo.downloadIndex(id, context)
 if err != nil { return }
 err = repo.AddTag(id, tag)
 return
}`
const dejavuRef = `package dejavu
func (repo *Repo) AddTag(id, tag string) (err error) {
 lock.Lock()
 defer lock.Unlock()
 // Retain validation and atomic publication under the caller's lock.
 if !valid(tag) { return invalidTag }
 return repo.writeTag(id, tag)
}`

func TestDejavuRepairKeepsBothPublicOperationsLocked(t *testing.T) {
	b, r, changed := repairDejavu([]byte(dejavuBackup), []byte(dejavuRef))
	if !changed {
		t.Fatal("nested lock was not repaired")
	}
	backup, ref := parseSource(b), parseSource(r)
	for _, f := range []struct {
		s    *sourceFile
		name string
	}{{backup, "DownloadTagIndex"}, {ref, "AddTag"}} {
		fn := f.s.fn(f.name)
		if len(calls(fn.Body, "Lock")) != 1 || len(calls(fn.Body, "Unlock")) != 1 || len(calls(fn.Body, "sftpAddTagLocked")) != 1 {
			t.Fatalf("%s lost its locking contract: %s", f.name, f.s.text(fn))
		}
	}
	helper := ref.fn("sftpAddTagLocked")
	if len(calls(helper.Body, "Lock")) != 0 || len(calls(helper.Body, "valid")) != 1 || len(calls(helper.Body, "writeTag")) != 1 {
		t.Fatal("unlocked helper must retain upstream validation and publication")
	}
	b2, r2, changed := repairDejavu(b, r)
	if changed || string(b2) != string(b) || string(r2) != string(r) {
		t.Fatal("repair is not idempotent")
	}
}

func TestDejavuUnaffectedShapesStayByteIdentical(t *testing.T) {
	for _, pair := range [][2]string{
		{dejavuBackup, strings.ReplaceAll(dejavuRef, " lock.Lock()\n defer lock.Unlock()\n", "")},
		{strings.Replace(dejavuBackup, "repo.AddTag", "repo.addTag", 1), dejavuRef},
		{strings.ReplaceAll(dejavuBackup, " lock.Lock()\n defer lock.Unlock()\n", ""), dejavuRef},
		{strings.ReplaceAll(dejavuBackup, "lock.", "otherLock."), dejavuRef},
	} {
		b, r, changed := repairDejavu([]byte(pair[0]), []byte(pair[1]))
		if changed || string(b) != pair[0] || string(r) != pair[1] {
			t.Fatal("rewrote an unaffected dependency")
		}
	}
}

func TestDejavuUnexpectedLockShapeStopsPreparation(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("accepted an unknown lock lifetime")
		}
	}()
	repairDejavu([]byte(dejavuBackup), []byte(strings.Replace(dejavuRef, "defer lock.Unlock()", "defer other.Unlock()", 1)))
}
