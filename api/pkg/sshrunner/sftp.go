// sftp.go — SFTP 文件传输。
package sshrunner

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
)

// UploadOpts 上传选项。
type UploadOpts struct {
	Src  string // 本地路径 (文件或目录)
	Dest string // 远程路径
}

// SFTP 封装 SFTP 操作。
type SFTP struct {
	client *sftp.Client
}

// NewSFTP 从 SSH Client 创建 SFTP 会话。
func NewSFTP(c *Client) (*SFTP, error) {
	sftpClient, err := sftp.NewClient(c.Raw())
	if err != nil {
		return nil, fmt.Errorf("sftp new client: %w", err)
	}
	return &SFTP{client: sftpClient}, nil
}

// Close 关闭 SFTP 会话。
func (s *SFTP) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// UploadFile 上传单个文件。
func (s *SFTP) UploadFile(localPath, remotePath string) error {
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local %s: %w", localPath, err)
	}
	defer src.Close()

	if err := s.client.MkdirAll(path.Dir(remotePath)); err != nil {
		return fmt.Errorf("mkdir remote %s: %w", path.Dir(remotePath), err)
	}

	dst, err := s.client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote %s: %w", remotePath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", localPath, remotePath, err)
	}
	return nil
}

// UploadDir 递归上传目录。
func (s *SFTP) UploadDir(localDir, remoteDir string) error {
	return filepath.Walk(localDir, func(localPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, localPath)
		if err != nil {
			return err
		}
		remotePath := path.Join(remoteDir, filepath.ToSlash(rel))

		if info.IsDir() {
			return s.client.MkdirAll(remotePath)
		}
		return s.UploadFile(localPath, remotePath)
	})
}

// DownloadFile 下载单个文件。
func (s *SFTP) DownloadFile(remotePath, localPath string) error {
	src, err := s.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote %s: %w", remotePath, err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("mkdir local %s: %w", filepath.Dir(localPath), err)
	}

	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local %s: %w", localPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", remotePath, localPath, err)
	}
	return nil
}
