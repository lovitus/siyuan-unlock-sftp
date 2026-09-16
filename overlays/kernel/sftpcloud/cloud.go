// SPDX-License-Identifier: AGPL-3.0-or-later
package sftpcloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/pkg/sftp"
	"github.com/siyuan-note/dejavu/cloud"
	"github.com/siyuan-note/dejavu/entity"
)

type SFTP struct {
	*cloud.BaseCloud
	config    Config
	lockMu    sync.Mutex
	lockToken string
	lockHeld  bool
}

var _ cloud.Cloud = (*SFTP)(nil)

func New(base *cloud.BaseCloud, config *Config) (*SFTP, error) {
	if config == nil {
		return nil, fmt.Errorf("SFTP configuration is required")
	}
	cfg := *config
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	if !cloud.IsValidCloudDirName(base.Dir) {
		return nil, fmt.Errorf("invalid SFTP repository name")
	}
	return &SFTP{BaseCloud: base, config: cfg}, nil
}
func (s *SFTP) key(file string) string { return s.Dir + "/siyuan/repo/" + file }
func (s *SFTP) GetConcurrentReqs() int { return s.config.ConcurrentReqs }
func (s *SFTP) UploadObject(file string, overwrite bool) (int64, error) {
	if path.IsAbs(file) || strings.Contains(file, "\\") {
		return 0, fmt.Errorf("invalid object path")
	}
	for _, part := range strings.Split(file, "/") {
		if part == ".." {
			return 0, fmt.Errorf("invalid object path")
		}
	}
	data, err := os.ReadFile(filepath.Join(s.RepoPath, filepath.FromSlash(file)))
	if err != nil {
		return 0, err
	}
	return s.UploadBytes(file, data, overwrite)
}
func (s *SFTP) UploadBytes(file string, data []byte, overwrite bool) (int64, error) {
	var err error
	if file == "lock-sync" {
		err = s.writeSyncLock(data)
	} else {
		err = s.write(s.key(file), data, overwrite)
	}
	if err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}
func (s *SFTP) DownloadObject(file string) ([]byte, error) {
	if file == "lock-sync" {
		return s.downloadSyncLock()
	}
	return s.read(s.key(file))
}
func (s *SFTP) RemoveObject(file string) error {
	if file == "lock-sync" {
		return s.removeSyncLock()
	}
	return parseErr(s.session(func(c *sftp.Client) error {
		p, e := s.remote(c, s.key(file))
		if e != nil {
			return e
		}
		return s.withMutationGuard(c, func() error { return c.Remove(p) })
	}))
}
func (s *SFTP) CreateRepo(name string) error {
	if !cloud.IsValidCloudDirName(name) {
		return fmt.Errorf("invalid SFTP repository name")
	}
	return s.session(func(c *sftp.Client) error {
		p, e := s.remote(c, name+"/siyuan/repo")
		if e != nil {
			return e
		}
		return s.withRepoMutation(c, name, func() error { return c.MkdirAll(p) })
	})
}
func (s *SFTP) RemoveRepo(name string) error {
	if !cloud.IsValidCloudDirName(name) {
		return fmt.Errorf("invalid SFTP repository name")
	}
	return s.session(func(c *sftp.Client) error {
		p, e := s.remote(c, name+"/siyuan/repo")
		if e != nil {
			return e
		}
		return s.withRepoMutation(c, name, func() error {
			// Only delete our repository subtree and our upload staging area; leave
			// unrelated files in the configured root untouched.
			if err := removeTreeIfExists(c, p); err != nil {
				return err
			}
			staging, err := s.remote(c, name+"/siyuan/.sftp-tmp")
			if err != nil {
				return err
			}
			if err := removeTreeIfExists(c, staging); err != nil {
				return err
			}
			return nil
		})
	})
}
func removeTree(c *sftp.Client, p string) error {
	entries, err := c.ReadDir(p)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := path.Join(p, entry.Name())
		if entry.IsDir() && entry.Mode()&os.ModeSymlink == 0 {
			err = removeTree(c, child)
		} else {
			err = c.Remove(child)
		}
		if err != nil {
			return err
		}
	}
	return c.RemoveDirectory(p)
}
func removeTreeIfExists(c *sftp.Client, p string) error {
	if _, err := c.Stat(p); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return removeTree(c, p)
}
func (s *SFTP) GetRepos() (repos []*cloud.Repo, size int64, err error) {
	repos = []*cloud.Repo{}
	err = s.session(func(c *sftp.Client) error {
		root, e := s.remote(c, "")
		if e != nil {
			return e
		}
		infos, e := c.ReadDir(root)
		if e != nil {
			return e
		}
		for _, info := range infos {
			if !info.IsDir() || !cloud.IsValidCloudDirName(info.Name()) {
				continue
			}
			repo, e := s.remote(c, info.Name()+"/siyuan/repo")
			if e != nil {
				return e
			}
			stat, e := c.Stat(repo)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			if !stat.IsDir() {
				continue
			}
			repoSize, e := treeSize(c, repo)
			if e != nil {
				return e
			}
			repos = append(repos, &cloud.Repo{Name: info.Name(), Size: repoSize, Updated: stat.ModTime().Local().Format("2006-01-02 15:04:05")})
			size += repoSize
		}
		return nil
	})
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return
}

func treeSize(c *sftp.Client, dir string) (int64, error) {
	entries, err := c.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if entry.Mode()&os.ModeSymlink != 0 {
			return 0, fmt.Errorf("SFTP repository contains a symbolic link")
		}
		child := path.Join(dir, entry.Name())
		if entry.IsDir() {
			n, err := treeSize(c, child)
			if err != nil {
				return 0, err
			}
			total += n
		} else {
			total += entry.Size()
		}
	}
	return total, nil
}

// ListObjects returns files relative to the prefix, including nested chunks
// and tag refs. PurgeCloud relies on complete enumeration to retain live data.
func (s *SFTP) ListObjects(prefix string) (map[string]*entity.ObjectInfo, error) {
	ret := map[string]*entity.ObjectInfo{}
	err := s.session(func(c *sftp.Client) error {
		root, err := s.remote(c, s.key(prefix))
		if err != nil {
			return err
		}
		var walk func(string, string) error
		walk = func(dir, rel string) error {
			infos, err := c.ReadDir(dir)
			if err != nil {
				return err
			}
			for _, info := range infos {
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("SFTP object is a symbolic link")
				}
				name := path.Join(rel, info.Name())
				if info.IsDir() {
					if err := walk(path.Join(dir, info.Name()), name); err != nil {
						return err
					}
				} else {
					ret[name] = &entity.ObjectInfo{Path: name, Size: info.Size()}
				}
			}
			return nil
		}
		return walk(root, "")
	})
	if errors.Is(err, os.ErrNotExist) {
		return ret, nil
	}
	return ret, err
}
func (s *SFTP) refs(prefix string) ([]*cloud.Ref, error) {
	ret := []*cloud.Ref{}
	infos, err := s.readDir(s.key("refs/" + prefix))
	if errors.Is(err, cloud.ErrCloudObjectNotFound) {
		return ret, nil
	}
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		if info.IsDir() {
			continue
		}
		data, err := s.DownloadObject("refs/" + prefix + info.Name())
		if err != nil {
			return nil, err
		}
		ret = append(ret, &cloud.Ref{Name: info.Name(), ID: string(data), Updated: info.ModTime().Local().Format("2006-01-02 15:04:05")})
	}
	return ret, nil
}
func (s *SFTP) GetTags() ([]*cloud.Ref, error) { return s.refs("tags/") }
func decode(data []byte, value any) error {
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(1<<30))
	if err != nil {
		return err
	}
	defer decoder.Close()
	data, err = decoder.DecodeAll(data, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func (s *SFTP) GetIndex(id string) (*entity.Index, error) {
	data, err := s.DownloadObject("indexes/" + id)
	if err != nil {
		return nil, err
	}
	ret := &entity.Index{}
	if err = decode(data, ret); err != nil {
		return nil, err
	}
	return ret, nil
}
func (s *SFTP) GetIndexes(page int) (ret []*entity.Index, pageCount, totalCount int, err error) {
	ret = []*entity.Index{}
	if page < 1 {
		err = fmt.Errorf("page must be positive")
		return
	}
	data, err := s.DownloadObject("indexes-v2.json")
	if errors.Is(err, cloud.ErrCloudObjectNotFound) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	indexes := &cloud.Indexes{}
	if err = decode(data, indexes); err != nil {
		return
	}
	const pageSize = 32
	totalCount = len(indexes.Indexes)
	pageCount = (totalCount + pageSize - 1) / pageSize
	if page > pageCount {
		return
	}
	for i := (page - 1) * pageSize; i < page*pageSize && i < totalCount; i++ {
		var index *entity.Index
		index, err = s.GetIndex(indexes.Indexes[i].ID)
		if err != nil {
			return
		}
		index.Files = nil
		ret = append(ret, index)
	}
	return
}
func (s *SFTP) GetRefsFiles() ([]string, []*cloud.Ref, error) {
	refs, err := s.refs("")
	if err != nil {
		return nil, nil, err
	}
	tags, err := s.GetTags()
	if err != nil {
		return nil, nil, err
	}
	refs = append(refs, tags...)
	ids := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		index, err := s.GetIndex(ref.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range index.Files {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids, refs, nil
}
func (s *SFTP) GetChunks(ids []string) (missing []string, err error) {
	missing = []string{}
	err = s.session(func(c *sftp.Client) error {
		for _, id := range ids {
			if len(id) < 3 {
				return fmt.Errorf("invalid chunk ID")
			}
			p, e := s.remote(c, s.key("objects/"+id[:2]+"/"+id[2:]))
			if e != nil {
				return e
			}
			_, e = c.Stat(p)
			if os.IsNotExist(e) {
				missing = append(missing, id)
			} else if e != nil {
				return e
			}
		}
		return nil
	})
	return
}
