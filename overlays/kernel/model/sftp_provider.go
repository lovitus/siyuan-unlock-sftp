package model

import (
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/sftpcloud"
)

// Return a copy: callers must not mutate upstream immutable section snapshots.
func GetSFTPConfig() *conf.SFTP {
	config := sftpSyncConfig().SFTP
	if config == nil {
		return sftpcloud.DefaultConfig()
	}
	copy := *config
	return &copy
}

func SetSyncProviderSFTP(config *conf.SFTP) (err error) {
	if config == nil {
		return config.Normalize()
	}
	release := lockAssetSourceChange()
	defer release()
	copy := *config
	config = &copy
	if err = config.Normalize(); err != nil {
		return
	}
	current := sftpSyncConfig()
	if current.Provider == conf.ProviderSFTP && (current.SFTP == nil || current.SFTP.Identity() != config.Identity()) {
		if err = requireCompleteAssetDownloads(); err != nil {
			return
		}
	}
	storeSFTPConfig(config)
	refreshLANSyncManager()
	return
}

func withSFTPBackupLease(provider *sftpcloud.SFTP, upload func() (int, int, int64, error)) (files, chunks int, size int64, err error) {
	if provider == nil {
		return upload()
	}
	err = provider.WithLease(func() error { files, chunks, size, err = upload(); return err })
	return
}
