package aliyun

import (
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
)

// Credential 阿里云 STS 临时凭据。
type Credential struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	ExpiredAt       time.Time
}

// IsExpired 判断是否已过期(提前 5 分钟视为过期)。
func (c *Credential) IsExpired() bool {
	return time.Now().After(c.ExpiredAt.Add(-5 * time.Minute))
}

// CredentialManager 管理 STS 凭据的获取与缓存。
type CredentialManager struct {
	accessKeyID     string
	accessKeySecret string
	roleARN         string
	region          string

	mu   sync.RWMutex
	cred *Credential
}

// NewCredentialManager 创建凭据管理器。
func NewCredentialManager(accessKeyID, accessKeySecret, roleARN, region string) *CredentialManager {
	return &CredentialManager{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		roleARN:         roleARN,
		region:          region,
	}
}

// Get 获取有效凭据(缓存命中直接返,否则刷新)。
// roleARN 为空时直接使用永久 AK/SK。
func (m *CredentialManager) Get() (*Credential, error) {
	if m.roleARN == "" {
		return &Credential{
			AccessKeyID:     m.accessKeyID,
			AccessKeySecret: m.accessKeySecret,
			ExpiredAt:       time.Now().Add(24 * time.Hour),
		}, nil
	}
	m.mu.RLock()
	c := m.cred
	m.mu.RUnlock()
	if c != nil && !c.IsExpired() {
		return c, nil
	}
	return m.refresh()
}

func (m *CredentialManager) refresh() (*Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// double-check
	if m.cred != nil && !m.cred.IsExpired() {
		return m.cred, nil
	}

	config := sdk.NewConfig()
	cred := credentials.NewAccessKeyCredential(m.accessKeyID, m.accessKeySecret)
	client, err := sts.NewClientWithOptions(m.region, config, cred)
	if err != nil {
		return nil, err
	}

	req := sts.CreateAssumeRoleRequest()
	req.RoleArn = m.roleARN
	req.RoleSessionName = "helios"
	req.DurationSeconds = "3600"

	resp, err := client.AssumeRole(req)
	if err != nil {
		return nil, err
	}

	c := &Credential{
		AccessKeyID:     resp.Credentials.AccessKeyId,
		AccessKeySecret: resp.Credentials.AccessKeySecret,
		SecurityToken:   resp.Credentials.SecurityToken,
		ExpiredAt:       time.Now().Add(3600 * time.Second),
	}
	m.cred = c
	return c, nil
}
