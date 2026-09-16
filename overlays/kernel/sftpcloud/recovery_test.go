package sftpcloud

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/siyuan-note/dejavu/cloud"
)

// Drop TCP after a controlled amount of encrypted client traffic. Negative
// budget means healthy; zero drops every new connection until recovery.
func faultProxy(t *testing.T, destination string) (int, *atomic.Int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	budget := &atomic.Int64{}
	budget.Store(-1)
	var mu sync.Mutex
	active := map[net.Conn]bool{}
	stopping := false
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		stopping = true
		for c := range active {
			c.Close()
		}
	})
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if stopping {
				mu.Unlock()
				client.Close()
				return
			}
			active[client] = true
			mu.Unlock()
			go func() {
				defer func() { client.Close(); mu.Lock(); delete(active, client); mu.Unlock() }()
				server, err := net.Dial("tcp", destination)
				if err != nil {
					return
				}
				defer server.Close()
				go func() { io.Copy(client, server); client.Close() }()
				buf := make([]byte, 32768)
				for {
					n, err := client.Read(buf)
					if n > 0 {
						cut := false
						for {
							left := budget.Load()
							if left < 0 {
								break
							}
							allowed := min(int64(n), left)
							if budget.CompareAndSwap(left, left-allowed) {
								n = int(allowed)
								cut = allowed == left
								break
							}
						}
						if n > 0 {
							if _, e := server.Write(buf[:n]); e != nil {
								return
							}
						}
						if cut {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, budget
}

func TestInterruptedUploadRecovery(t *testing.T) {
	_, cfg := testCloud(t)
	port, budget := faultProxy(t, cfg.Address())
	proxied := *cfg
	proxied.Port = port
	s, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main"}}, &proxied)
	if err != nil {
		t.Fatal(err)
	}
	const name = "objects/ab/interrupt"
	original := []byte("previous complete object")
	if _, err = s.UploadBytes(name, original, true); err != nil {
		t.Fatal(err)
	}
	replacement := make([]byte, 2<<20)
	if _, err = rand.Read(replacement); err != nil {
		t.Fatal(err)
	}
	budget.Store(128 << 10)
	if _, err = s.UploadBytes(name, replacement, true); err == nil {
		t.Fatal("upload unexpectedly survived TCP cut")
	}
	if budget.Load() != 0 {
		t.Fatal("fault injection did not consume its byte budget")
	}
	budget.Store(-1)
	got, err := s.DownloadObject(name)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("interrupted upload replaced or corrupted committed object")
	}
	objects, err := s.ListObjects("")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 {
		t.Fatalf("partial upload exposed as repository objects: %+v", objects)
	}
	if _, err = s.UploadBytes(name, replacement, true); err != nil {
		t.Fatal(err)
	}
	got, err = s.DownloadObject(name)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatal("recovered upload content differs")
	}
	// Incomplete staging files may remain, but must never be listed as objects.
	entries, err := os.ReadDir(filepath.Join(cfg.Path, "main/siyuan/.sftp-tmp"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("interrupted transfer left %d isolated staging file(s)", len(entries))
}
