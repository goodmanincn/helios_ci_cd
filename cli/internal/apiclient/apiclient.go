// Package apiclient — 极薄的 helios API HTTP 客户端。
//
// 设计:
//   - 自动加 Authorization: Bearer <token> 和 Content-Type
//   - 401 抛 ErrUnauthorized, CLI 提示重新 login
//   - 非 2xx 抛 *APIError 含 status + body
//   - JSON only, 不支持 multipart (CLI 当前用不到)
package apiclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

var ErrUnauthorized = errors.New("unauthorized (401) — run `helios login`")

// APIError — 非 2xx 响应包装, body 通常是 {"error":..., "message":...}
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("API error %d", e.Status)
	}
	return fmt.Sprintf("API error %d: %s", e.Status, e.Body)
}

type Client struct {
	BaseURL    string
	Token      string
	OrgID      int64
	HTTPClient *http.Client
}

func New(baseURL, token string, orgID int64) *Client {
	return &Client{
		BaseURL: baseURL, Token: token, OrgID: orgID,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Do 发请求, 自动加 header, 解析响应 JSON 到 out.
// out 可传 nil (不关心响应体), 也可传 *[]byte 拿原始字节, 也可传 struct ptr.
func (c *Client) Do(method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.OrgID > 0 {
		req.Header.Set("X-Org-ID", strconv.FormatInt(c.OrgID, 10))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(respBytes)}
	}

	if out == nil || len(respBytes) == 0 {
		return nil
	}
	switch o := out.(type) {
	case *[]byte:
		*o = respBytes
		return nil
	default:
		return json.Unmarshal(respBytes, out)
	}
}
