package sftpcloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/pkg/sftp"
	"github.com/siyuan-note/dejavu/cloud"
	"github.com/siyuan-note/dejavu/entity"
	"golang.org/x/crypto/ssh"
)

func testCloud(t *testing.T) (*SFTP, *Config) {
	if runtime.GOOS == "windows" {
		t.Skip("embedded test server needs a POSIX filesystem; exercised on Linux/macOS")
	}
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if meta.User() != "tester" || string(password) != " password " {
			return nil, errors.New("bad credentials")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				sc, chans, reqs, err := ssh.NewServerConn(conn, serverConfig)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(reqs)
				for channel := range chans {
					if channel.ChannelType() != "session" {
						channel.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					ch, requests, err := channel.Accept()
					if err != nil {
						return
					}
					go func() {
						defer ch.Close()
						for request := range requests {
							var sub struct{ Name string }
							if request.Type != "subsystem" || ssh.Unmarshal(request.Payload, &sub) != nil || sub.Name != "sftp" {
								request.Reply(false, nil)
								continue
							}
							request.Reply(true, nil)
							server, err := sftp.NewServer(ch)
							if err != nil {
								return
							}
							defer server.Close()
							server.Serve()
							return
						}
					}()
				}
			}()
		}
	}()
	config := DefaultConfig()
	config.Host = "127.0.0.1"
	config.Port = listener.Addr().(*net.TCPAddr).Port
	config.Username = "tester"
	config.Password = " password "
	config.Path = t.TempDir()
	config.HostKey = ssh.FingerprintSHA256(signer.PublicKey())
	s, err := New(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", RepoPath: t.TempDir()}}, config)
	if err != nil {
		t.Fatal(err)
	}
	return s, config
}
func TestRequiredPath(t *testing.T) {
	for _, p := range []string{"", "   ", "relative", "/a/../b", "/a\\b", "/a\x00b"} {
		t.Run(strings.ReplaceAll(p, "\x00", "NUL"), func(t *testing.T) {
			c := DefaultConfig()
			c.Path = p
			if c.Normalize() == nil {
				t.Fatalf("accepted invalid path %q", p)
			}
		})
	}
}
func TestSFTPRoundTrip(t *testing.T) {
	s, cfg := testCloud(t)
	if err := s.CreateRepo("main"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.UploadBytes("objects/ab/cdef", []byte("first"), false); err != nil || n != 5 {
		t.Fatal(n, err)
	}
	if _, err := s.UploadBytes("objects/ab/cdef", []byte("second"), false); err != nil {
		t.Fatal(err)
	}
	data, err := s.DownloadObject("objects/ab/cdef")
	if err != nil || string(data) != "first" {
		t.Fatal(string(data), err)
	}
	if _, err = s.UploadBytes("objects/ab/cdef", []byte("second"), true); err != nil {
		t.Fatal(err)
	}
	data, err = s.DownloadObject("objects/ab/cdef")
	if err != nil || string(data) != "second" {
		t.Fatal(string(data), err)
	}
	missing, err := s.GetChunks([]string{"abcdef", "abcdef00"})
	if err != nil || len(missing) != 1 || missing[0] != "abcdef00" {
		t.Fatal(missing, err)
	}
	objects, err := s.ListObjects("objects/ab/")
	if err != nil || objects["cdef"].Size != 6 {
		t.Fatal(objects, err)
	}
	nested, err := s.ListObjects("objects/")
	if err != nil || nested["ab/cdef"] == nil || nested["ab/cdef"].Size != 6 {
		t.Fatal(nested, err)
	}
	repos, _, err := s.GetRepos()
	if err != nil || len(repos) != 1 || repos[0].Name != "main" {
		t.Fatal(repos, err)
	}
	if _, err = s.DownloadObject("missing"); !errors.Is(err, cloud.ErrCloudObjectNotFound) {
		t.Fatal(err)
	}
	for _, key := range []string{"../../../escape", "objects/../../../../escape", "objects/ab/../../../../escape"} {
		if _, err = s.UploadBytes(key, []byte("bad"), true); err == nil {
			t.Fatal("accepted traversal", key)
		}
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(cfg.Path, "main/siyuan/repo/link")); err == nil {
		if _, err = s.UploadBytes("link/escape", []byte("bad"), true); err == nil {
			t.Fatal("followed symlink")
		}
		if _, err = os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
			t.Fatal("escaped remote root")
		}
	}
	if err = s.RemoveObject("objects/ab/cdef"); err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveRepo("main"); err != nil {
		t.Fatal(err)
	}
	repos, _, err = s.GetRepos()
	if err != nil || len(repos) != 0 {
		t.Fatal(repos, err)
	}
}
func TestHostKeyAndRootValidation(t *testing.T) {
	s, cfg := testCloud(t)
	s.config.HostKey = "SHA256:wrong"
	if _, _, err := s.GetRepos(); err == nil {
		t.Fatal("accepted wrong host key")
	}
	s.config = *cfg
	s.config.Path = filepath.Join(cfg.Path, "does-not-exist")
	if _, err := s.UploadBytes("test", []byte("bad"), true); err == nil {
		t.Fatal("accepted missing root")
	}
	if _, err := os.Stat(s.config.Path); !os.IsNotExist(err) {
		t.Fatal("created root implicitly")
	}
}
func TestIndexesAndReferences(t *testing.T) {
	s, _ := testCloud(t)
	if err := s.CreateRepo("main"); err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	put := func(key string, value any) {
		t.Helper()
		data, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.UploadBytes(key, encoder.EncodeAll(data, nil), true); e != nil {
			t.Fatal(e)
		}
	}
	put("indexes/idx", &entity.Index{ID: "idx", Files: []string{"file1", "file2"}})
	put("indexes-v2.json", &cloud.Indexes{Indexes: []*cloud.Index{{ID: "idx"}}})
	for _, ref := range []string{"refs/latest", "refs/tags/snapshot"} {
		if _, err = s.UploadBytes(ref, []byte("idx"), true); err != nil {
			t.Fatal(err)
		}
	}
	indexes, pages, total, err := s.GetIndexes(1)
	if err != nil || pages != 1 || total != 1 || len(indexes) != 1 || indexes[0].Files != nil {
		t.Fatal(indexes, pages, total, err)
	}
	listedRefs, err := s.ListObjects("refs/")
	if err != nil || len(listedRefs) != 2 || listedRefs["tags/snapshot"] == nil {
		t.Fatal(listedRefs, err)
	}
	files, refs, err := s.GetRefsFiles()
	if err != nil || len(files) != 2 || len(refs) != 2 {
		t.Fatal(files, refs, err)
	}
	if _, _, _, err = s.GetIndexes(0); err == nil {
		t.Fatal("accepted page zero")
	}
	// Malformed data and inaccessible refs must propagate, not look like an empty repo.
	if _, err = s.UploadBytes("indexes/idx", []byte("corrupt"), true); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.GetRefsFiles(); err == nil {
		t.Fatal("silently ignored corrupt index")
	}
}
