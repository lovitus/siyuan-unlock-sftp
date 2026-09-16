package sftpcloud

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/siyuan-note/dejavu"
	"github.com/siyuan-note/dejavu/cloud"
)

func TestSnapshotSurvivesPurgeAndRestores(t *testing.T) {
	s, cfg := testCloud(t)
	s.AvailableSize = 1 << 30
	key := []byte("0123456789abcdef0123456789abcdef")
	newRepo := func(provider cloud.Cloud) (*dejavu.Repo, string) {
		t.Helper()
		base := t.TempDir()
		data := filepath.Join(base, "data")
		if err := os.MkdirAll(data, 0700); err != nil {
			t.Fatal(err)
		}
		repo, err := dejavu.NewRepo(data, filepath.Join(base, "repo"), filepath.Join(base, "history"), filepath.Join(base, "tmp"), "device", "Device", "linux", key, nil, provider)
		if err != nil {
			t.Fatal(err)
		}
		return repo, data
	}
	repo, data := newRepo(s)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	original := []byte("content retained only by the old snapshot")
	must(os.WriteFile(filepath.Join(data, "original.txt"), original, 0600))
	ctx := map[string]interface{}{}
	index, err := repo.Index("original", false, ctx)
	must(err)
	_, _, err = repo.Sync(ctx)
	must(err)
	// Include a name indistinguishable from the old implementation's temp names.
	tags := []string{"backup.sftp-july", "backup.sftp-0123456789abcdef0123456789abcdef"}
	for _, tag := range tags {
		must(repo.AddTag(index.ID, tag))
		must(s.WithLease(func() error {
			_, _, _, err = repo.UploadTagIndex(tag, index.ID, ctx)
			return err
		}))
	}
	must(os.Remove(filepath.Join(data, "original.txt")))
	must(os.WriteFile(filepath.Join(data, "current.txt"), []byte("current"), 0600))
	_, err = repo.Index("current", false, ctx)
	must(err)
	_, _, err = repo.Sync(ctx)
	must(err)

	// Simulate an interrupted upload. No code should interpret this as a ref.
	staging := filepath.Join(cfg.Path, "main/siyuan/.sftp-tmp")
	must(os.MkdirAll(staging, 0700))
	must(os.WriteFile(filepath.Join(staging, "interrupted"), []byte("partial-index-id"), 0600))
	found, err := s.GetTags()
	must(err)
	if len(found) != len(tags) {
		t.Fatalf("unexpected tags: %+v", found)
	}
	refs, err := s.ListObjects("refs/")
	must(err)
	for _, tag := range tags {
		if refs["tags/"+tag] == nil {
			t.Fatalf("tag missing from purge refs: %s", tag)
		}
	}
	_, _, err = s.GetRefsFiles()
	must(err)
	_, err = repo.PurgeCloud()
	must(err)
	_, err = s.GetIndex(index.ID)
	must(err)

	// Restore using an empty local repository: no local objects can mask loss.
	remote, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30}}, cfg)
	must(err)
	restored, restoredData := newRepo(remote)
	_, _, _, err = restored.DownloadTagIndex(tags[0], index.ID, ctx)
	must(err)
	_, _, err = restored.Checkout(index.ID, ctx)
	must(err)
	content, err := os.ReadFile(filepath.Join(restoredData, "original.txt"))
	must(err)
	if string(content) != string(original) {
		t.Fatalf("restored content mismatch: %q", content)
	}
}

func TestUploadsLeaveNoTemporaryObjects(t *testing.T) {
	s, cfg := testCloud(t)
	for _, name := range []string{"refs/latest", "refs/tags/backup.sftp-july", "objects/ab/cdef"} {
		if _, err := s.UploadBytes(name, []byte("complete"), true); err != nil {
			t.Fatal(err)
		}
	}
	files, err := s.ListObjects("")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("temporary files exposed: %+v", files)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.Path, "main/siyuan/.sftp-tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("successful uploads left staging files: %v", entries)
	}
}
