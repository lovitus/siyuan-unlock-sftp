// SPDX-License-Identifier: AGPL-3.0-or-later
package sftpcloud

import (
	"encoding/json"
	"errors"
	"time"
)

// WithLease protects a multi-object operation that DejaVu does not lock itself,
// notably tagged backup upload. Use a dedicated provider/repository instance.
func (s *SFTP) WithLease(fn func() error) (err error) {
	refresh := func() error {
		data, e := json.Marshal(syncLock{DeviceID: "sftp-backup", Time: time.Now().UnixMilli()})
		if e != nil {
			return e
		}
		return s.writeSyncLock(data)
	}
	if err = refresh(); err != nil {
		return err
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				done <- nil
				return
			case <-ticker.C:
				if e := refresh(); e != nil {
					done <- e
					return
				}
			}
		}
	}()
	defer func() { close(stop); err = errors.Join(err, <-done, s.removeSyncLock()) }()
	return fn()
}
