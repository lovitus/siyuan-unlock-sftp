package main

import (
	"strings"
	"testing"
)

func TestBackupProviderStaysInUploadFunction(t *testing.T) {
	download := "func DownloadCloudSnapshot() {\n repo, err := newCloudRepositoryWithAssetSourceLocked()\n use(repo, err)\n}\n"
	upload := "func UploadCloudSnapshot() {\n repo, err := newCloudRepositoryWithAssetSourceLocked()\n use(repo, err)\n backupProvider.WithLease(upload)\n}\n"
	src := "package model\n" + download + upload
	out, err := rewriteBackup([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), download) {
		t.Fatal("download function changed")
	}
	if !strings.Contains(string(out), "func UploadCloudSnapshot() {\n var backupProvider *sftpcloud.SFTP") {
		t.Fatalf("provider declared in wrong scope: %s", out)
	}
	if strings.Count(string(out), "var backupProvider") != 1 {
		t.Fatal("ambiguous declaration")
	}
	for _, bad := range []string{"package model\n" + download, src + upload, strings.ReplaceAll(src, "newCloudRepositoryWithAssetSourceLocked()", "newCloudRepositoryWithAssetSourceLocked(changed)")} {
		if _, err := rewriteBackup([]byte(bad)); err == nil {
			t.Fatal("unknown initialization accepted")
		}
	}
}
