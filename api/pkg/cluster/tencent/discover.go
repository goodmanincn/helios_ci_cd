package tencent

import (
	"context"
	"encoding/json"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tke "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20180525"

	"github.com/helios-cicd/helios/api/pkg/cluster"
)

// CloudCredentials 腾讯云 API 凭据 (向导 / discover 用)。
type CloudCredentials struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
	RoleARN   string `json:"role_arn,omitempty"`
	Region    string `json:"region"`
}

// ClusterSummary TKE 集群列表项。
type ClusterSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
}

// ListClusters 列出 region 下 TKE 集群 (用于接入向导下拉)。
func ListClusters(ctx context.Context, creds CloudCredentials) ([]ClusterSummary, error) {
	if creds.SecretID == "" || creds.SecretKey == "" || creds.Region == "" {
		return nil, cluster.NewError(cluster.KindInvalidInput, "secret_id, secret_key and region required", nil)
	}
	cm := NewCredentialManager(creds.SecretID, creds.SecretKey, creds.RoleARN, creds.Region)
	stsCred, err := cm.Get()
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "get credential", err)
	}

	apiCred := common.NewCredential(stsCred.SecretID, stsCred.SecretKey)
	apiCred.Token = stsCred.SessionToken
	prof := profile.NewClientProfile()
	client, err := tke.NewClient(apiCred, creds.Region, prof)
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "tke client", err)
	}

	req := tke.NewDescribeClustersRequest()
	// Limit 默认 20; 向导场景够用
	limit := int64(100)
	req.Limit = &limit
	resp, err := client.DescribeClustersWithContext(ctx, req)
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "describe clusters", err)
	}
	out := make([]ClusterSummary, 0)
	if resp.Response == nil || resp.Response.Clusters == nil {
		return out, nil
	}
	for _, c := range resp.Response.Clusters {
		if c == nil {
			continue
		}
		item := ClusterSummary{}
		if c.ClusterId != nil {
			item.ID = *c.ClusterId
		}
		if c.ClusterName != nil {
			item.Name = *c.ClusterName
		}
		if c.ClusterVersion != nil {
			item.Version = *c.ClusterVersion
		}
		if c.ClusterStatus != nil {
			item.Status = *c.ClusterStatus
		}
		if item.ID != "" {
			out = append(out, item)
		}
	}
	return out, nil
}

// CredentialsJSON 把凭据 + cluster_id 序列化为 Provider 所需的 config JSON。
func CredentialsJSON(creds CloudCredentials, clusterID string) ([]byte, error) {
	type payload struct {
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
		RoleARN   string `json:"role_arn,omitempty"`
		Region    string `json:"region"`
		ClusterID string `json:"cluster_id"`
	}
	return json.Marshal(payload{
		SecretID: creds.SecretID, SecretKey: creds.SecretKey,
		RoleARN: creds.RoleARN, Region: creds.Region, ClusterID: clusterID,
	})
}
