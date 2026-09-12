// SPDX-License-Identifier: AGPL-3.0-or-later
package sftpcloud

import (
	"encoding/base64"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
)

type Config struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Path           string `json:"path"`
	HostKey        string `json:"hostKey"`
	Timeout        int    `json:"timeout"`
	ConcurrentReqs int    `json:"concurrentReqs"`
}

func DefaultConfig() *Config { return &Config{Port: 22, Timeout: 30, ConcurrentReqs: 4} }

// Normalize requires an explicit remote directory; it never defaults to the SSH home.
func (c *Config) Normalize() error {
	if c == nil {
		return fmt.Errorf("SFTP configuration is required")
	}
	c.Path = strings.TrimSpace(c.Path)
	if c.Path == "" {
		return fmt.Errorf("SFTP path is required")
	}
	if !path.IsAbs(c.Path) || strings.ContainsAny(c.Path, "\\\x00\r\n") {
		return fmt.Errorf("SFTP path must be an absolute POSIX directory")
	}
	for _, part := range strings.Split(c.Path, "/") {
		if part == ".." {
			return fmt.Errorf("SFTP path must not contain '..'")
		}
	}
	c.Path = path.Clean(c.Path)
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	c.HostKey = strings.TrimSpace(c.HostKey)
	if c.Host == "" || strings.ContainsAny(c.Host, "/\\ \t\r\n") {
		return fmt.Errorf("SFTP host is required (hostname or IP, without scheme or port)")
	}
	if strings.Contains(c.Host, ":") && net.ParseIP(strings.Trim(c.Host, "[]")) == nil {
		return fmt.Errorf("SFTP host must be a hostname or IP, without a port")
	}
	if c.Username == "" {
		return fmt.Errorf("SFTP username is required")
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("SFTP port must be between 1 and 65535")
	}
	fingerprint, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(c.HostKey, "SHA256:"))
	if !strings.HasPrefix(c.HostKey, "SHA256:") || err != nil || len(fingerprint) != 32 {
		return fmt.Errorf("SFTP host key must be a SHA256 fingerprint")
	}
	if c.Timeout == 0 {
		c.Timeout = 30
	}
	if c.Timeout < 7 {
		c.Timeout = 7
	}
	if c.Timeout > 300 {
		c.Timeout = 300
	}
	if c.ConcurrentReqs < 1 {
		c.ConcurrentReqs = 1
	}
	if c.ConcurrentReqs > 16 {
		c.ConcurrentReqs = 16
	}
	return nil
}
func (c *Config) Address() string {
	return net.JoinHostPort(strings.Trim(c.Host, "[]"), strconv.Itoa(c.Port))
}
func (c *Config) Identity() string { return c.Address() + "\x00" + c.Username + "\x00" + c.Path }
