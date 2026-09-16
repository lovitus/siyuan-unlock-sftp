package sftpcloud

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/siyuan-note/dejavu"
	"github.com/siyuan-note/dejavu/cloud"
)

// Exercise normal Sync with distinct device IDs and independent local stores.
// A tag download/checkout alone does not test bidirectional synchronization.
func TestTwoDeviceSync(t *testing.T) {
	_, config := testCloud(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	type device struct {
		repo *dejavu.Repo
		data string
	}
	newDevice := func(id string) device {
		base := t.TempDir()
		data := filepath.Join(base, "data")
		must(os.MkdirAll(data, 0700))
		provider, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30}}, config)
		must(err)
		repo, err := dejavu.NewRepo(data, filepath.Join(base, "repo"), filepath.Join(base, "history"), filepath.Join(base, "tmp"), id, id, "linux", []byte("0123456789abcdef0123456789abcdef"), nil, provider)
		must(err)
		return device{repo, data}
	}
	a, b := newDevice("device-a"), newDevice("device-b")
	stamp := time.Now().Add(-time.Hour)
	write := func(d device, name, content string) {
		p := filepath.Join(d.data, name)
		must(os.WriteFile(p, []byte(content), 0600))
		stamp = stamp.Add(time.Second)
		must(os.Chtimes(p, stamp, stamp))
	}
	syncDevice := func(d device) {
		_, err := d.repo.Index("sync", false, map[string]interface{}{})
		must(err)
		_, _, err = d.repo.Sync(map[string]interface{}{})
		must(err)
	}
	assertContent := func(d device, name, want string) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(d.data, name))
		must(err)
		if string(got) != want {
			t.Fatalf("%s: got %q, want %q", name, got, want)
		}
	}
	write(a, "note.txt", "original from A")
	// SiYuan workspaces contain initial data; DejaVu refuses empty indexes.
	write(b, "device-b.txt", "initial B workspace")
	syncDevice(a)
	syncDevice(b)
	assertContent(b, "note.txt", "original from A")
	write(b, "note.txt", "updated from B")
	syncDevice(b)
	syncDevice(a)
	assertContent(a, "note.txt", "updated from B")
	// Both devices add independent files before either uploads.
	write(a, "a.txt", "offline A")
	write(b, "b.txt", "offline B")
	syncDevice(a)
	syncDevice(b)
	syncDevice(a)
	for _, d := range []device{a, b} {
		assertContent(d, "a.txt", "offline A")
		assertContent(d, "b.txt", "offline B")
	}
	must(os.Remove(filepath.Join(b.data, "note.txt")))
	syncDevice(b)
	syncDevice(a)
	if _, err := os.Stat(filepath.Join(a.data, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete did not propagate: %v", err)
	}
}
