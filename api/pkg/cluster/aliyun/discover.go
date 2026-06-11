package aliyun

import (
	"context"
	"encoding/json"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cs"

	"github.com/helios-cicd/helios/api/pkg/cluster"
)

// CloudCredentials 阿里云 API 凭据。
type CloudCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	RoleARN         string `json:"role_arn,omitempty"`
	Region          string `json:"region"`
}

// ClusterSummary ACK 集群列表项。
type ClusterSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
}

// ListClusters 列出 region 下 ACK 集群。
func ListClusters(ctx context.Context, creds CloudCredentials) ([]ClusterSummary, error) {
	if creds.AccessKeyID == "" || creds.AccessKeySecret == "" || creds.Region == "" {
		return nil, cluster.NewError(cluster.KindInvalidInput, "access_key_id, access_key_secret and region required", nil)
	}
	cm := NewCredentialManager(creds.AccessKeyID, creds.AccessKeySecret, creds.RoleARN, creds.Region)
	stsCred, err := cm.Get()
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "get credential", err)
	}

	cfg := sdk.NewConfig()
	var client *cs.Client
	if stsCred.SecurityToken != "" {
		client, err = cs.NewClientWithOptions(creds.Region, cfg,
			credentials.NewStsTokenCredential(stsCred.AccessKeyID, stsCred.AccessKeySecret, stsCred.SecurityToken))
	} else {
		client, err = cs.NewClientWithOptions(creds.Region, cfg,
			credentials.NewAccessKeyCredential(stsCred.AccessKeyID, stsCred.AccessKeySecret))
	}
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "cs client", err)
	}

	req := cs.CreateDescribeClustersV1Request()
	req.PageSize = requests.NewInteger(100)
	resp, err := client.DescribeClustersV1(req)
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "describe clusters", err)
	}

	type clusterItem struct {
		ClusterID      string `json:"cluster_id"`
		Name           string `json:"name"`
		CurrentVersion string `json:"current_version"`
		State          string `json:"state"`
	}
	body := resp.GetHttpContentBytes()
	var items []clusterItem
	var wrapped struct {
		Clusters []clusterItem `json:"clusters"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Clusters) > 0 {
		items = wrapped.Clusters
	} else if err := json.Unmarshal(body, &items); err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "parse clusters response", err)
	}

	out := make([]ClusterSummary, 0, len(items))
	for _, c := range items {
		if c.ClusterID == "" {
			continue
		}
		out = append(out, ClusterSummary{
			ID: c.ClusterID, Name: c.Name, Version: c.CurrentVersion, Status: c.State,
		})
	}
	return out, nil
}

// CredentialsJSON 序列化 Provider config。
func CredentialsJSON(creds CloudCredentials, clusterID string) ([]byte, error) {
	type payload struct {
		AccessKeyID     string `json:"access_key_id"`
		AccessKeySecret string `json:"access_key_secret"`
		RoleARN         string `json:"role_arn,omitempty"`
		Region          string `json:"region"`
		ClusterID       string `json:"cluster_id"`
	}
	return json.Marshal(payload{
		AccessKeyID: creds.AccessKeyID, AccessKeySecret: creds.AccessKeySecret,
		RoleARN: creds.RoleARN, Region: creds.Region, ClusterID: clusterID,
	})
}
