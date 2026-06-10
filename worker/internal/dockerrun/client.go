// Package dockerrun 为 build handler 提供 Docker 容器化执行能力(T1.4)。
//
// 设计:
//   - Client: 封装 docker 官方 SDK,屏蔽 host/socket 细节,提供 health/ping。
//   - Executor: 一次性任务执行器,负责 pull → create → start → tail logs → wait → rm。
//   - 日志:实时回调到 LogSink,行缓冲,带 stream(stdout/stderr) 区分。
//   - 资源限制 + env 注入由 RunSpec 描述。
//
// 当前 Client 仅做连接 + health(T1.4.1);Executor/日志在后续文件。
package dockerrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
)

// ClientConfig 控制 docker client 行为。
//
// Host 优先级: 显式传 Host > DOCKER_HOST env > 默认 unix:///var/run/docker.sock。
// 不指定 APIVersion 时用 client.WithAPIVersionNegotiation(),由 SDK 自动协商。
type ClientConfig struct {
	Host          string        // e.g. "unix:///var/run/docker.sock" or "tcp://127.0.0.1:2375"
	APIVersion    string        // 留空走自动协商
	RequestTO     time.Duration // 单次 docker API 请求超时, 0 = 默认 30s
	NegotiateOnce bool          // true 时构造时立刻 ping 一次, 失败返错
}

// Client 包了 docker SDK *client.Client,加 helios 习惯的 health/close 接口。
type Client struct {
	raw    *dockerclient.Client
	host   string
	reqTO  time.Duration
}

// New 按 cfg 构造 docker Client。
//
// 错误返回时机:
//   - Host 不合法 → 立即错
//   - NegotiateOnce=true 时 ping 失败 → 立即错(避免后续每次执行都才发现 daemon down)
func New(ctx context.Context, cfg ClientConfig) (*Client, error) {
	host := cfg.Host
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	if host == "" {
		host = dockerclient.DefaultDockerHost
	}

	opts := []dockerclient.Opt{
		dockerclient.WithHost(host),
		dockerclient.WithHTTPHeaders(map[string]string{
			"User-Agent": "helios-worker/dev",
		}),
	}
	if cfg.APIVersion != "" {
		opts = append(opts, dockerclient.WithVersion(cfg.APIVersion))
	} else {
		opts = append(opts, dockerclient.WithAPIVersionNegotiation())
	}

	raw, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker new client (host=%s): %w", host, err)
	}

	reqTO := cfg.RequestTO
	if reqTO <= 0 {
		reqTO = 30 * time.Second
	}

	c := &Client{raw: raw, host: host, reqTO: reqTO}
	if cfg.NegotiateOnce {
		pctx, cancel := context.WithTimeout(ctx, reqTO)
		defer cancel()
		if _, err := raw.Ping(pctx); err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("docker ping (host=%s): %w", host, err)
		}
	}
	return c, nil
}

// Raw 返回底层 docker SDK 客户端(供 executor 等内部使用)。
// 外部使用者请通过 helper 方法,以便后续替换实现 / mock。
func (c *Client) Raw() *dockerclient.Client { return c.raw }

// Host 返回当前连接的 docker 地址(供日志/排查使用)。
func (c *Client) Host() string { return c.host }

// Close 释放底层 http 连接。
func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}

// Ping 探活 dockerd。
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return errors.New("nil docker client")
	}
	ctx, cancel := context.WithTimeout(ctx, c.reqTO)
	defer cancel()
	_, err := c.raw.Ping(ctx)
	return err
}

// ListImages 列出本地镜像(T1.4.1 验收要求)。
// 返回每个镜像的 RepoTags 拼接列表(取第一个非 <none>),空时返回空切片。
func (c *Client) ListImages(ctx context.Context) ([]string, error) {
	if c == nil || c.raw == nil {
		return nil, errors.New("nil docker client")
	}
	ctx, cancel := context.WithTimeout(ctx, c.reqTO)
	defer cancel()
	imgs, err := c.raw.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("image list: %w", err)
	}
	out := make([]string, 0, len(imgs))
	for _, img := range imgs {
		tag := pickRepoTag(img.RepoTags)
		if tag == "" {
			tag = img.ID
		}
		out = append(out, tag)
	}
	return out, nil
}

func pickRepoTag(tags []string) string {
	for _, t := range tags {
		if t != "" && t != "<none>:<none>" {
			return t
		}
	}
	return ""
}
