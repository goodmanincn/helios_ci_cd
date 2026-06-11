// Package sshrunner SSH 客户端封装 (E6.2)。
package sshrunner

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// AuthConfig SSH 认证配置。
type AuthConfig struct {
	Password   string // 密码认证
	PrivateKey string // PEM 格式私钥
	Passphrase string // 私钥 passphrase
}

// DialConfig 连接配置。
type DialConfig struct {
	Host       string
	Port       int
	User       string
	Auth       AuthConfig
	Timeout    time.Duration
	KnownHosts []byte // 可选; nil 时跳过 known_hosts 校验 (dev)
}

// Client 封装 SSH 连接。
type Client struct {
	cfg    DialConfig
	client *ssh.Client
}

// Dial 建立 SSH 连接。
func Dial(cfg DialConfig) (*Client, error) {
	if cfg.Port <= 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	authMethods, err := buildAuthMethods(cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("build auth: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		Timeout:         cfg.Timeout,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // M6 简化; prod 配 known_hosts
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	client, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	return &Client{cfg: cfg, client: client}, nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Raw 返回底层 *ssh.Client (供 executor / sftp 使用)。
func (c *Client) Raw() *ssh.Client {
	return c.client
}

func buildAuthMethods(auth AuthConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if auth.PrivateKey != "" {
		signer, err := parsePrivateKey(auth.PrivateKey, auth.Passphrase)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if auth.Password != "" {
		methods = append(methods, ssh.Password(auth.Password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no auth method provided")
	}
	return methods, nil
}

func parsePrivateKey(pem, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(passphrase))
	}
	return ssh.ParsePrivateKey([]byte(pem))
}
