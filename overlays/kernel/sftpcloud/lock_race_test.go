package sftpcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/siyuan-note/dejavu"
	"github.com/siyuan-note/dejavu/cloud"
)

type barrierCloud struct {
	cloud.Cloud
	barrier, id string
}

func awaitFile(p string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(p); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("barrier timeout: %s", p)
}
func (c *barrierCloud) DownloadObject(key string) ([]byte, error) {
	data, err := c.Cloud.DownloadObject(key)
	if key == "lock-sync" && errors.Is(err, cloud.ErrCloudObjectNotFound) {
		if e := os.WriteFile(filepath.Join(c.barrier, c.id+".read"), nil, 0600); e != nil {
			return nil, e
		}
		for _, id := range []string{"a", "b"} {
			if e := awaitFile(filepath.Join(c.barrier, id+".read")); e != nil {
				return nil, e
			}
		}
	}
	return data, err
}
func (c *barrierCloud) UploadObject(key string, overwrite bool) (int64, error) {
	n, err := c.Cloud.UploadObject(key, overwrite)
	if key == "lock-sync" && err == nil {
		if e := os.WriteFile(filepath.Join(c.barrier, c.id+".acquired"), nil, 0600); e != nil {
			return n, e
		}
		// Neither device can finish/unlock while the parent measures overlap.
		if e := awaitFile(filepath.Join(c.barrier, "release")); e != nil {
			return n, e
		}
	}
	return n, err
}
func TestLockProcessWorker(t *testing.T) {
	base := os.Getenv("SFTP_DIAG_DEVICE")
	if base == "" {
		t.Skip("helper")
	}
	raw, err := os.ReadFile(os.Getenv("SFTP_DIAG_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	s, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30}}, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(base)
	c := &barrierCloud{Cloud: s, barrier: filepath.Dir(base), id: id}
	repo, err := dejavu.NewRepo(filepath.Join(base, "data"), filepath.Join(base, "repo"), filepath.Join(base, "history"), filepath.Join(base, "tmp"), id, id, "linux", []byte("0123456789abcdef0123456789abcdef"), nil, c)
	if err != nil {
		t.Fatal(err)
	}
	ctx := map[string]interface{}{}
	if _, err = repo.Index("race", false, ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repo.Sync(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentLockExclusion(t *testing.T) {
	_, cfg := testCloud(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type result struct {
		out []byte
		err error
	}
	results := make(chan result, 2)
	for _, id := range []string{"a", "b"} {
		base := filepath.Join(root, id)
		if err = os.MkdirAll(filepath.Join(base, "data"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(base, "data", id+".txt"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockProcessWorker$", "-test.count=1")
		cmd.Env = append(os.Environ(), "SFTP_DIAG_DEVICE="+base, "SFTP_DIAG_CONFIG="+configPath)
		go func() { out, err := cmd.CombinedOutput(); results <- result{out, err} }()
	}
	// Observe either two simultaneous acquisitions (bug) or one rejection.
	deadline := time.Now().Add(10 * time.Second)
	var first *result
	both := false
	for time.Now().Before(deadline) {
		_, ae := os.Stat(filepath.Join(root, "a.acquired"))
		_, be := os.Stat(filepath.Join(root, "b.acquired"))
		if ae == nil && be == nil {
			both = true
			break
		}
		select {
		case r := <-results:
			first = &r
		default:
		}
		if first != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	all := []result{}
	if first != nil {
		all = append(all, *first)
	}
	for len(all) < 2 {
		all = append(all, <-results)
	}
	if both {
		t.Fatal("mutual exclusion violated: two independent processes successfully uploaded lock-sync before either could unlock")
	}
	successes := 0
	for _, r := range all {
		if r.err == nil {
			successes++
		} else if !strings.Contains(string(r.out), "lock cloud repo failed") {
			t.Fatalf("unexpected worker failure: %v\n%s", r.err, r.out)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one successful sync, got %d: %+v", successes, all)
	}
}
