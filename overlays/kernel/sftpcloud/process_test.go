package sftpcloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/siyuan-note/dejavu"
	"github.com/siyuan-note/dejavu/cloud"
)

// Each invocation starts a new process, avoiding DejaVu's package-level mutex.
func TestSFTPProcessWorker(t *testing.T) {
	configPath := os.Getenv("SFTP_TEST_CONFIG")
	if configPath == "" {
		t.Skip("subprocess helper")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	base := os.Getenv("SFTP_TEST_DEVICE")
	provider, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30}}, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := dejavu.NewRepo(filepath.Join(base, "data"), filepath.Join(base, "repo"), filepath.Join(base, "history"), filepath.Join(base, "tmp"), filepath.Base(base), filepath.Base(base), "linux", []byte("0123456789abcdef0123456789abcdef"), nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	ctx := map[string]interface{}{}
	if _, err = repo.Index("process sync", false, ctx); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		var result *dejavu.MergeResult
		result, _, err = repo.Sync(ctx)
		if err == nil {
			raw, e := json.Marshal(result)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(base, "sync-result.json"), raw, 0600); e != nil {
				t.Fatal(e)
			}
			return
		}
		if !errors.Is(err, dejavu.ErrCloudLocked) && !errors.Is(err, dejavu.ErrLockCloudFailed) && !errors.Is(err, cloud.ErrCloudServiceUnavailable) {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal(err)
}

func processCommand(t *testing.T, configPath, base string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSFTPProcessWorker$", "-test.count=1")
	cmd.Env = append(os.Environ(), "SFTP_TEST_CONFIG="+configPath, "SFTP_TEST_DEVICE="+base)
	return cmd
}

func TestIndependentProcessSync(t *testing.T) {
	_, cfg := testCloud(t)
	port, budget := faultProxy(t, cfg.Address())
	cfg.Port = port
	configPath := filepath.Join(t.TempDir(), "config.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	a, b := filepath.Join(root, "device-a"), filepath.Join(root, "device-b")
	write := func(base, name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(base, "data"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "data", name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(base string) {
		t.Helper()
		if out, err := processCommand(t, configPath, base).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", filepath.Base(base), err, out)
		}
	}
	write(a, "a.txt", "from A")
	write(b, "b.txt", "from B")
	// Establish a common baseline before simultaneous offline additions.
	run(a)
	run(b)
	run(a)
	write(a, "offline-a.txt", "offline A")
	write(b, "offline-b.txt", "offline B")
	type result struct {
		output []byte
		err    error
	}
	results := make(chan result, 2)
	for _, base := range []string{a, b} {
		cmd := processCommand(t, configPath, base)
		go func() { out, err := cmd.CombinedOutput(); results <- result{out, err} }()
	}
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("simultaneous sync: %v\n%s", r.err, r.output)
		}
	}
	// Normal follow-up syncs must converge, including a restarted process.
	run(a)
	run(b)
	run(a)
	for _, base := range []string{a, b} {
		for name, want := range map[string]string{"a.txt": "from A", "b.txt": "from B", "offline-a.txt": "offline A", "offline-b.txt": "offline B"} {
			got, err := os.ReadFile(filepath.Join(base, "data", name))
			if err != nil || string(got) != want {
				t.Fatalf("%s/%s: %q, %v", filepath.Base(base), name, got, err)
			}
		}
	}
	// Interrupt a real Sync upload, restart the client and retry, then download
	// from the other independent process. Verify bytes rather than only success.
	attachment := make([]byte, 2<<20)
	if _, err = rand.Read(attachment); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(a, "data", "attachment.bin"), attachment, 0600); err != nil {
		t.Fatal(err)
	}
	budget.Store(128 << 10)
	if out, err := processCommand(t, configPath, a).CombinedOutput(); err == nil {
		t.Fatalf("sync unexpectedly survived TCP cut: %s", out)
	}
	if budget.Load() != 0 {
		t.Fatal("sync did not reach the injected TCP cut")
	}
	budget.Store(-1)
	// A restarted instance must wait for the previous owner's lease to expire.
	// Age only the test server's lock timestamp instead of sleeping 65 seconds.
	lockPath := filepath.Join(cfg.Path, "main/siyuan/repo/lock-sync")
	raw, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var expired syncLock
	if err = json.Unmarshal(raw, &expired); err != nil {
		t.Fatal(err)
	}
	expired.Time = time.Now().Add(-2 * time.Minute).UnixMilli()
	raw, err = json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(lockPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run(a)
	run(b)
	for _, base := range []string{a, b} {
		got, err := os.ReadFile(filepath.Join(base, "data", "attachment.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, attachment) {
			t.Fatalf("%s attachment differs after recovery", filepath.Base(base))
		}
	}

}

func TestIndependentProcessConflictHistory(t *testing.T) {
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
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	stamp := time.Now().Add(-time.Hour)
	write := func(base, name, content string) {
		t.Helper()
		p := filepath.Join(base, "data", name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		stamp = stamp.Add(time.Second)
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	run := func(base string) {
		t.Helper()
		if out, err := processCommand(t, configPath, base).CombinedOutput(); err != nil {
			t.Fatalf("sync failed: %v\n%s", err, out)
		}
	}
	write(a, "shared.txt", "common baseline")
	write(b, "b.txt", "initial B")
	run(a)
	run(b)
	run(a)
	// Both modify the same baseline before either syncs.
	const fromA = "offline edit from A"
	const fromB = "offline edit from B"
	write(a, "shared.txt", fromA)
	write(b, "shared.txt", fromB)
	run(a)
	run(b)
	raw, err = os.ReadFile(filepath.Join(b, "sync-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result dejavu.MergeResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.ConflictCount() != 1 {
		t.Fatalf("expected one conflict, got %+v", result)
	}
	run(a)
	current, err := os.ReadFile(filepath.Join(a, "data/shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(b, "data/shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, other) {
		t.Fatalf("conflict did not converge: %q vs %q", current, other)
	}
	losing := fromA
	if string(current) == fromA {
		losing = fromB
	} else if string(current) != fromB {
		t.Fatalf("unexpected merged content: %q", current)
	}
	found := false
	err = filepath.WalkDir(filepath.Join(b, "history"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		data, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		if string(data) == losing {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("losing edit not preserved in conflict history")
	}
}
