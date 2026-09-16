// SPDX-License-Identifier: AGPL-3.0-or-later
package sftpcloud

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
	"github.com/siyuan-note/dejavu/cloud"
)

var errSyncLocked = errors.New("SFTP sync lock is owned by another client; retry synchronization")

type syncLock struct {
	DeviceID string `json:"deviceID"`
	Time     int64  `json:"time"`
	Owner    string `json:"sftpOwner,omitempty"`
}

// A server-side exclusive mkdir serializes lock read/check/write and release.
// Do not automatically steal a leftover guard: deleting a live guard would
// recreate the very check-then-write race this guard prevents.
func (s *SFTP) withLockGuard(c *sftp.Client, fn func(string) error) (err error) {
	guard, err := s.remote(c, s.Dir+"/siyuan/.sftp-lock-guard")
	if err != nil {
		return err
	}
	if err = c.MkdirAll(path.Dir(guard)); err != nil {
		return err
	}
	if err = c.Mkdir(guard); err != nil {
		return fmt.Errorf("could not acquire SFTP lock guard (retry, or inspect a leftover guard after stopping all clients): %w", err)
	}
	defer func() {
		cleanupErr := c.RemoveDirectory(guard)
		// Never retry an ambiguous removal on a new connection: the server
		// may have removed our guard and another client may already own a new
		// directory at the same path. Fail closed instead of deleting theirs.
		if cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("SFTP lock guard cleanup failed: %w", cleanupErr))
		}
	}()
	p, err := s.remote(c, s.key("lock-sync"))
	if err != nil {
		return err
	}
	return fn(p)
}

func readSyncLock(c *sftp.Client, p string) (*syncLock, error) {
	f, err := c.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Refuse corrupt/unbounded lock content instead of deleting an unknown lock.
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	if len(data) > 4096 {
		return nil, errors.New("SFTP sync lock exceeds maximum size")
	}
	var ret syncLock
	if err = json.Unmarshal(data, &ret); err != nil {
		return nil, err
	}
	if ret.DeviceID == "" || ret.Time <= 0 {
		return nil, errors.New("invalid SFTP sync lock")
	}
	return &ret, nil
}

func (s *SFTP) writeSyncLock(data []byte) error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	var next syncLock
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	if next.DeviceID == "" || next.Time <= 0 {
		return errors.New("invalid SFTP sync lock")
	}
	if s.lockToken == "" {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		s.lockToken = hex.EncodeToString(nonce)
	}
	next.Owner = s.lockToken
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return s.session(func(c *sftp.Client) error {
		return s.withLockGuard(c, func(p string) error {
			current, err := readSyncLock(c, p)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				// A device ID is not ownership: admin operations even share IDs
				// such as "purge". Only the token permits a live-lock refresh.
				if s.lockHeld && current.Owner != s.lockToken {
					return errSyncLocked
				}
				if current.Owner != s.lockToken && time.Now().Before(time.UnixMilli(current.Time).Add(65*time.Second)) {
					return errSyncLocked
				}
			} else if s.lockHeld {
				return errSyncLocked
			}
			if err = s.writeFile(c, s.key("lock-sync"), data, true, nil); err != nil {
				return err
			}
			s.lockHeld = true
			return nil
		})
	})
}

func (s *SFTP) removeSyncLock() error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if !s.lockHeld {
		return errSyncLocked
	}
	return s.session(func(c *sftp.Client) error {
		return s.withLockGuard(c, func(p string) error {
			current, err := readSyncLock(c, p)
			if os.IsNotExist(err) {
				s.lockHeld = false
				return nil
			}
			if err != nil {
				return err
			}
			if current.Owner != s.lockToken {
				return errSyncLocked
			}
			if err = c.Remove(p); err != nil {
				return err
			}
			s.lockHeld = false
			return nil
		})
	})
}

// Serialize the ownership check AND mutation with acquisition/takeover. A
// separate check before an upload would still allow takeover before rename.
func (s *SFTP) withMutationGuard(c *sftp.Client, fn func() error) error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	return s.withLockGuard(c, func(p string) error {
		current, err := readSyncLock(c, p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if s.lockHeld {
			if err != nil || current.Owner != s.lockToken {
				return errSyncLocked
			}
		} else if err == nil {
			// Standalone tag operations may run without a lease only when no sync
			// owns the repository. They must not bypass another client's lock.
			return errSyncLocked
		}
		return fn()
	})
}

func (s *SFTP) withRepoMutation(c *sftp.Client, name string, fn func() error) error {
	return s.withMutationGuard(c, func() error {
		if name == s.Dir {
			return fn()
		}
		// DejaVu locks the selected repository, but Create/Remove may target a
		// different repository. Respect the target's lock as well.
		target := &SFTP{BaseCloud: &cloud.BaseCloud{Conf: &cloud.Conf{Dir: name}}, config: s.config}
		return target.withMutationGuard(c, fn)
	})
}

// DejaVu asserts the deviceID/time JSON types without checking them. Validate
// untrusted remote locks here so corrupt content causes an error, not a panic.
func (s *SFTP) downloadSyncLock() (data []byte, err error) {
	err = s.session(func(c *sftp.Client) error {
		p, e := s.remote(c, s.key("lock-sync"))
		if e != nil {
			return e
		}
		lock, e := readSyncLock(c, p)
		if e != nil {
			return e
		}
		data, e = json.Marshal(lock)
		return e
	})
	return data, parseErr(err)
}
