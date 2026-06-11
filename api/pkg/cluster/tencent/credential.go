package tencent

import (
	"sync"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sts "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts/v20180813"
)

// Credential 腾讯 STS 临时凭据。
type Credential struct {
	SecretID     string
	SecretKey    string
	SessionToken string
	ExpiredAt    time.Time
}

// IsExpired 判断是否已过期(提前 5 分钟视为过期)。
func (c *Credential) IsExpired() bool {
	return time.Now().After(c.ExpiredAt.Add(-5 * time.Minute))
}

// CredentialManager 管理 STS 凭据的获取与缓存。
type CredentialManager struct {
	secretID  string
	secretKey string
	roleARN   string
	region    string

	mu   sync.RWMutex
	cred *Credential
}

// NewCredentialManager 创建凭据管理器。
func NewCredentialManager(secretID, secretKey, roleARN, region string) *CredentialManager {
	return &CredentialManager{
		secretID:  secretID,
		secretKey: secretKey,
		roleARN:   roleARN,
		region:    region,
	}
}

// Get 获取有效凭据(缓存命中直接返,否则刷新)。
// roleARN 为空时直接使用永久 AK/SK (向导场景, 不走 STS)。
func (m *CredentialManager) Get() (*Credential, error) {
	if m.roleARN == "" {
		return &Credential{
			SecretID:  m.secretID,
			SecretKey: m.secretKey,
			ExpiredAt: time.Now().Add(24 * time.Hour),
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

	cred := common.NewCredential(m.secretID, m.secretKey)
	prof := profile.NewClientProfile()
	prof.HttpProfile.Endpoint = "sts.tencentcloudapi.com"

	client, err := sts.NewClient(cred, m.region, prof)
	if err != nil {
		return nil, err
	}

	req := sts.NewAssumeRoleRequest()
	req.RoleArn = &m.roleARN
	req.RoleSessionName = common.StringPtr("helios")
	req.DurationSeconds = common.Uint64Ptr(3600)

	resp, err := client.AssumeRole(req)
	if err != nil {
		return nil, err
	}

	c := &Credential{
		SecretID:     *resp.Response.Credentials.TmpSecretId,
		SecretKey:    *resp.Response.Credentials.TmpSecretKey,
		SessionToken: *resp.Response.Credentials.Token,
		ExpiredAt:    time.Now().Add(time.Duration(*resp.Response.ExpiredTime) * time.Second),
	}
	m.cred = c
	return c, nil
}
