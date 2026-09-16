package sftpcloud

import (
	"encoding/json"
	"errors"
	"github.com/siyuan-note/dejavu/cloud"
	"testing"
	"time"
)

func TestBackupLeaseExcludesOtherOperations(t *testing.T) {
	s, cfg := testCloud(t)
	other, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	purge, _ := json.Marshal(syncLock{DeviceID: "purge", Time: time.Now().UnixMilli()})
	deliberate := errors.New("interrupted backup")
	err = s.WithLease(func() error {
		if _, e := s.UploadBytes("objects/ab/backup", []byte("staged backup"), true); e != nil {
			return e
		}
		// A pause between uploads must not allow purge or another backup to enter.
		if _, e := other.UploadBytes("lock-sync", purge, true); e == nil {
			t.Error("purge entered in the middle of backup")
		}
		if e := other.WithLease(func() error { t.Error("second backup entered"); return nil }); e == nil {
			t.Error("second backup acquired lease")
		}
		return deliberate
	})
	if !errors.Is(err, deliberate) {
		t.Fatalf("lost operation error: %v", err)
	}
	if _, err = other.UploadBytes("lock-sync", purge, true); err != nil {
		t.Fatalf("failed backup did not release lease: %v", err)
	}
	if err = other.RemoveObject("lock-sync"); err != nil {
		t.Fatal(err)
	}
}
