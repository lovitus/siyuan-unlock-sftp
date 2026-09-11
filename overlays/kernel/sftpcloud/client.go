// SPDX-License-Identifier: AGPL-3.0-or-later
package sftpcloud

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/siyuan-note/dejavu/cloud"
	"golang.org/x/crypto/ssh"
)

// Every operation owns its connection. The Cloud interface has no Close method;
// this keeps transient repository instances from leaking SSH sessions.
func (s *SFTP) session(fn func(*sftp.Client) error) error {
	timeout := time.Duration(s.config.Timeout) * time.Second
	conn, err := net.DialTimeout("tcp", s.config.Address(), timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	cfg := &ssh.ClientConfig{User: s.config.Username, Auth: []ssh.AuthMethod{ssh.Password(s.config.Password)}, Timeout: timeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != s.config.HostKey {
				return fmt.Errorf("SFTP host key fingerprint mismatch")
			}
			return nil
		}}
	cc, chans, reqs, err := ssh.NewClientConn(conn, s.config.Address(), cfg)
	if err != nil {
		return err
	}
	sc := ssh.NewClient(cc, chans, reqs)
	defer sc.Close()
	client, err := sftp.NewClient(sc)
	if err != nil {
		return err
	}
	defer client.Close()
	// Resolve the configured root once per operation and refuse links below it.
	// A configured symlink root is allowed, but object paths must remain within it.
	root, err := client.RealPath(s.config.Path)
	if err != nil {
		return err
	}
	info, err := client.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("SFTP path is not a directory")
	}
	return fn(client)
}

func (s *SFTP) remote(client *sftp.Client, key string) (string, error) {
	if strings.ContainsAny(key, "\\\x00") || path.IsAbs(key) {
		return "", fmt.Errorf("invalid SFTP object path")
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".." {
			return "", fmt.Errorf("invalid SFTP object path")
		}
	}
	root, err := client.RealPath(s.config.Path)
	if err != nil {
		return "", err
	}
	current := root
	for _, part := range strings.Split(path.Clean(key), "/") {
		if part == "." || part == "" {
			continue
		}
		current = path.Join(current, part)
		info, err := client.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("SFTP object paths must not contain symbolic links")
		}
	}
	return current, nil
}
func parseErr(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return cloud.ErrCloudObjectNotFound
	}
	return err
}
func (s *SFTP) read(key string) (data []byte, err error) {
	err = s.session(func(c *sftp.Client) error {
		p, e := s.remote(c, key)
		if e != nil {
			return e
		}
		f, e := c.Open(p)
		if e != nil {
			return e
		}
		defer f.Close()
		data, e = io.ReadAll(f)
		return e
	})
	return data, parseErr(err)
}
func (s *SFTP) readDir(key string) (infos []os.FileInfo, err error) {
	err = s.session(func(c *sftp.Client) error {
		p, e := s.remote(c, key)
		if e != nil {
			return e
		}
		infos, e = c.ReadDir(p)
		return e
	})
	return infos, parseErr(err)
}
func (s *SFTP) write(key string, data []byte, overwrite bool) error {
	return parseErr(s.session(func(c *sftp.Client) error {
		p, err := s.remote(c, key)
		if err != nil {
			return err
		}
		if !overwrite {
			if _, err = c.Lstat(p); err == nil {
				return nil
			}
			if !os.IsNotExist(err) {
				return err
			}
		}
		if err = c.MkdirAll(path.Dir(p)); err != nil {
			return err
		}
		nonce := make([]byte, 16)
		if _, err = rand.Read(nonce); err != nil {
			return err
		}
		// Stage outside repo/: incomplete writes must never appear as objects or
		// references, and legitimate tag names must not need filtering.
		staging, err := s.remote(c, s.Dir+"/siyuan/.sftp-tmp")
		if err != nil {
			return err
		}
		if err = c.MkdirAll(staging); err != nil {
			return err
		}
		temp := path.Join(staging, fmt.Sprintf("%x", nonce))
		f, err := c.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
		if err != nil {
			return err
		}
		defer c.Remove(temp)
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		// POSIX rename atomically replaces refs/locks. Never delete the destination
		// first: readers must not observe a missing or partially uploaded object.
		if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
			return c.PosixRename(temp, p)
		}
		return c.Rename(temp, p)
	}))
}
