package sftpcloud

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/pkg/sftp"
	"github.com/siyuan-note/dejavu"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siyuan-note/dejavu/cloud"
)

func TestSyncLockOwnership(t *testing.T) {
	a, cfg := testCloud(t)
	b, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	payload := func(id string, at time.Time) []byte {
		t.Helper()
		data, err := json.Marshal(syncLock{DeviceID: id, Time: at.UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if _, err = a.UploadBytes("lock-sync", payload("a", time.Now()), true); err != nil {
		t.Fatal(err)
	}
	if _, err = b.UploadBytes("lock-sync", payload("b", time.Now()), true); err == nil {
		t.Fatal("second owner acquired live lock")
	}
	if err = b.RemoveObject("lock-sync"); err == nil {
		t.Fatal("non-owner removed lock")
	}
	if _, err = a.UploadBytes("lock-sync", payload("a", time.Now()), true); err != nil {
		t.Fatal(err)
	}
	// Simulate lease expiry without waiting; an old owner must neither refresh
	// nor delete the new owner's lock after takeover.
	expired := syncLock{DeviceID: "a", Time: time.Now().Add(-time.Minute * 2).UnixMilli(), Owner: a.lockToken}
	data, err := json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(cfg.Path, "main/siyuan/repo/lock-sync"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = b.UploadBytes("lock-sync", payload("b", time.Now()), true); err != nil {
		t.Fatal(err)
	}
	if _, err = a.UploadBytes("lock-sync", payload("a", time.Now()), true); err == nil {
		t.Fatal("old owner refreshed new owner's lock")
	}
	if err = a.RemoveObject("lock-sync"); err == nil {
		t.Fatal("old owner removed new owner's lock")
	}
	if err = b.RemoveObject("lock-sync"); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonedLockGuardFailsClosed(t *testing.T) {
	s, cfg := testCloud(t)
	guard := filepath.Join(cfg.Path, "main/siyuan/.sftp-lock-guard")
	if err := os.MkdirAll(guard, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(syncLock{DeviceID: "a", Time: time.Now().UnixMilli()})
	if _, err := s.UploadBytes("lock-sync", data, true); err == nil {
		t.Fatal("acquired through another operation's guard")
	}
	if _, err := os.Stat(guard); err != nil {
		t.Fatalf("another operation's guard was removed: %v", err)
	}
}

func TestExpiredOwnerCannotMutateRepository(t *testing.T) {
	a, cfg := testCloud(t)
	b, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(s *SFTP, id string) {
		t.Helper()
		data, _ := json.Marshal(syncLock{DeviceID: id, Time: time.Now().UnixMilli()})
		if _, err := s.UploadBytes("lock-sync", data, true); err != nil {
			t.Fatal(err)
		}
	}
	acquire(a, "a")
	stale, _ := json.Marshal(syncLock{DeviceID: "a", Time: time.Now().Add(-2 * time.Minute).UnixMilli(), Owner: a.lockToken})
	if err = os.WriteFile(filepath.Join(cfg.Path, "main/siyuan/repo/lock-sync"), stale, 0600); err != nil {
		t.Fatal(err)
	}
	acquire(b, "b")
	for _, key := range []string{"refs/latest", "indexes-v2.json", "objects/ab/keep"} {
		if _, err = b.UploadBytes(key, []byte("new owner"), true); err != nil {
			t.Fatal(err)
		}
		if _, err = a.UploadBytes(key, []byte("stale owner"), true); err == nil {
			t.Errorf("stale owner overwrote %s", key)
		}
		if err = a.RemoveObject(key); err == nil {
			t.Errorf("stale owner deleted %s", key)
		}
		got, err := b.DownloadObject(key)
		if err != nil || string(got) != "new owner" {
			t.Errorf("new owner data changed: %s: %q %v", key, got, err)
		}
	}
}

func TestSameDeviceIDCannotStealLiveLock(t *testing.T) {
	for _, id := range []string{"same-device", "purge", "create", "remove"} {
		t.Run(id, func(t *testing.T) {
			a, cfg := testCloud(t)
			b, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main"}}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(syncLock{DeviceID: id, Time: time.Now().UnixMilli()})
			if _, err = a.UploadBytes("lock-sync", data, true); err != nil {
				t.Fatal(err)
			}
			if _, err = b.UploadBytes("lock-sync", data, true); err == nil {
				t.Fatal("matching device ID bypassed live lock ownership")
			}
		})
	}
}

// Take over after staging is complete, but before publication. Checking only
// at the beginning of an upload would incorrectly allow the final rename.
func TestTakeoverBeforePublish(t *testing.T) {
	a, cfg := testCloud(t)
	b, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(s *SFTP, id string) {
		t.Helper()
		raw, _ := json.Marshal(syncLock{DeviceID: id, Time: time.Now().UnixMilli()})
		if _, err := s.UploadBytes("lock-sync", raw, true); err != nil {
			t.Fatal(err)
		}
	}
	acquire(a, "a")
	staged := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	var release sync.Once
	defer release.Do(func() { close(resume) })
	go func() {
		done <- a.session(func(c *sftp.Client) error {
			return a.writeFile(c, a.key("refs/latest"), []byte("stale snapshot"), true, func(publish func() error) error {
				close(staged)
				<-resume
				return a.withMutationGuard(c, publish)
			})
		})
	}()
	select {
	case <-staged:
	case err := <-done:
		t.Fatalf("staging failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("staging timeout")
	}
	old, _ := json.Marshal(syncLock{DeviceID: "a", Time: time.Now().Add(-2 * time.Minute).UnixMilli(), Owner: a.lockToken})
	if err = os.WriteFile(filepath.Join(cfg.Path, "main/siyuan/repo/lock-sync"), old, 0600); err != nil {
		t.Fatal(err)
	}
	acquire(b, "b")
	if _, err = b.UploadBytes("refs/latest", []byte("new snapshot"), true); err != nil {
		t.Fatal(err)
	}
	release.Do(func() { close(resume) })
	if err = <-done; err == nil {
		t.Fatal("old staged upload was published after takeover")
	}
	got, err := b.DownloadObject("refs/latest")
	if err != nil || string(got) != "new snapshot" {
		t.Fatalf("latest changed: %q %v", got, err)
	}
}

func TestRepoMutationRespectsTargetLock(t *testing.T) {
	target, cfg := testCloud(t)
	source, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "other"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(syncLock{DeviceID: "target", Time: time.Now().UnixMilli()})
	if _, err = target.UploadBytes("lock-sync", raw, true); err != nil {
		t.Fatal(err)
	}
	if _, err = target.UploadBytes("refs/latest", []byte("keep"), true); err != nil {
		t.Fatal(err)
	}
	if err = source.RemoveRepo("main"); err == nil {
		t.Fatal("removed repository locked by another device")
	}
	if err = source.CreateRepo("main"); err == nil {
		t.Fatal("created repository through another device's lock")
	}
	got, err := target.DownloadObject("refs/latest")
	if err != nil || string(got) != "keep" {
		t.Fatalf("target changed: %q %v", got, err)
	}
	if err = target.RemoveObject("lock-sync"); err != nil {
		t.Fatal(err)
	}
	if err = source.RemoveRepo("main"); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedRemoteLockDoesNotPanic(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"deviceID":"other"}`, `{"deviceID":3,"time":1}`, `{"deviceID":"other","time":"bad"}`, `{"deviceID":"other","time":1e100}`, strings.Repeat("x", 4097)} {
		t.Run(fmt.Sprintf("bytes-%d-%x", len(raw), sha256.Sum256([]byte(raw))), func(t *testing.T) {
			s, cfg := testCloud(t)
			base := t.TempDir()
			data := filepath.Join(base, "data")
			if err := os.MkdirAll(data, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "note.txt"), []byte("local content"), 0600); err != nil {
				t.Fatal(err)
			}
			repo, err := dejavu.NewRepo(data, filepath.Join(base, "repo"), filepath.Join(base, "history"), filepath.Join(base, "tmp"), "a", "a", "linux", []byte("0123456789abcdef0123456789abcdef"), nil, s)
			if err != nil {
				t.Fatal(err)
			}
			remote := filepath.Join(cfg.Path, "main/siyuan/repo/lock-sync")
			if err = os.MkdirAll(filepath.Dir(remote), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(remote, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = repo.Index("local", false, map[string]interface{}{}); err != nil {
				t.Fatal(err)
			}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("malformed remote lock panicked: %v", recovered)
					}
				}()
				if _, _, err = repo.Sync(map[string]interface{}{}); err == nil {
					t.Error("malformed lock unexpectedly accepted")
				}
			}()
			got, err := os.ReadFile(remote)
			if err != nil || string(got) != raw {
				t.Errorf("malformed lock was silently changed: %q %v", got, err)
			}
		})
	}
}
