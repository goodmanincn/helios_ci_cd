package model

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

// Cluster 对应 clusters 表
type Cluster struct {
	Base
	OrgID           int64          `gorm:"column:org_id;not null;index"                                       json:"org_id"`
	Name            string         `gorm:"column:name;size:128;not null;uniqueIndex:uq_clusters_org_name,priority:2" json:"name"`
	Provider        string         `gorm:"column:provider;size:32;not null"                                   json:"provider"`
	Region          string         `gorm:"column:region;size:64"                                              json:"region,omitempty"`
	Endpoint        string         `gorm:"column:endpoint"                                                    json:"endpoint,omitempty"`
	CredentialID    *int64         `gorm:"column:credential_id"                                               json:"credential_id,omitempty"`
	Config          datatypes.JSON `gorm:"column:config;type:jsonb;default:'{}'"                              json:"config,omitempty"`
	Status          string         `gorm:"column:status;size:32;default:'unknown'"                            json:"status"`
	LastHealthCheck *time.Time     `gorm:"column:last_health_check"                                           json:"last_health_check,omitempty"`
	CreatedBy       *int64         `gorm:"column:created_by"                                                  json:"created_by,omitempty"`
}

func (Cluster) TableName() string { return "clusters" }

// 复合唯一约束在 DDL 中已建,这里 priority:1 = org_id (上面 not null;index)
// gorm v2 复合唯一索引: 在两列上都标 uniqueIndex:同名

// Host 对应 hosts 表
type Host struct {
	Base
	OrgID         int64          `gorm:"column:org_id;not null;index"                                  json:"org_id"`
	Name          string         `gorm:"column:name;size:128;not null;uniqueIndex:uq_hosts_org_name,priority:2" json:"name"`
	IP            string         `gorm:"column:ip;type:inet;not null"                                  json:"ip"`
	SSHPort       int            `gorm:"column:ssh_port;default:22"                                    json:"ssh_port"`
	SSHUser       string         `gorm:"column:ssh_user;size:64;default:'root'"                        json:"ssh_user"`
	CredentialID  *int64         `gorm:"column:credential_id"                                          json:"credential_id,omitempty"`
	OS            string         `gorm:"column:os;size:64"                                             json:"os,omitempty"`
	Arch          string         `gorm:"column:arch;size:32"                                           json:"arch,omitempty"`
	Labels        datatypes.JSON `gorm:"column:labels;type:jsonb;default:'{}'"                         json:"labels,omitempty"`
	Status        string         `gorm:"column:status;size:32;default:'unknown'"                       json:"status"`
	LastHeartbeat *time.Time     `gorm:"column:last_heartbeat"                                         json:"last_heartbeat,omitempty"`
}

func (Host) TableName() string { return "hosts" }

// HostGroup 对应 host_groups 表
type HostGroup struct {
	Base
	OrgID int64          `gorm:"column:org_id;not null;index"                                       json:"org_id"`
	Name  string         `gorm:"column:name;size:128;not null;uniqueIndex:uq_hgroups_org_name,priority:2" json:"name"`
	Vars  datatypes.JSON `gorm:"column:vars;type:jsonb;default:'{}'"                                json:"vars,omitempty"`
}

func (HostGroup) TableName() string { return "host_groups" }

// HostGroupMember 对应 host_group_members 表 (复合主键, no time cols)
type HostGroupMember struct {
	GroupID int64 `gorm:"column:group_id;primaryKey" json:"group_id"`
	HostID  int64 `gorm:"column:host_id;primaryKey"  json:"host_id"`
}

func (HostGroupMember) TableName() string { return "host_group_members" }

// Runner 对应 runners 表
type Runner struct {
	ID            int64          `gorm:"primaryKey"                                  json:"id"`
	Name          string         `gorm:"column:name;size:128;uniqueIndex;not null"   json:"name"`
	Type          string         `gorm:"column:type;size:32;not null"                json:"type"`
	Labels        pq.StringArray `gorm:"column:labels;type:text[]"                   json:"labels,omitempty"`
	Capacity      int            `gorm:"column:capacity;default:1"                   json:"capacity"`
	CurrentLoad   int            `gorm:"column:current_load;default:0"               json:"current_load"`
	Status        string         `gorm:"column:status;size:32;default:'unknown'"     json:"status"`
	LastHeartbeat *time.Time     `gorm:"column:last_heartbeat"                       json:"last_heartbeat,omitempty"`
	CreatedAt     time.Time      `gorm:"column:created_at;not null"                  json:"created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at;not null"                  json:"updated_at"`
}

func (Runner) TableName() string { return "runners" }

// Deployment 对应 deployments 表 (部署历史)。
type Deployment struct {
	ID         int64          `gorm:"primaryKey"                              json:"id"`
	ClusterID  int64          `gorm:"column:cluster_id;not null;index"        json:"cluster_id"`
	Namespace  string         `gorm:"column:namespace;size:128;not null"      json:"namespace"`
	Name       string         `gorm:"column:name;size:128;not null"           json:"name"`
	Image      string         `gorm:"column:image"                            json:"image,omitempty"`
	Revision   int64          `gorm:"column:revision"                         json:"revision"`
	RunID      *int64         `gorm:"column:run_id"                           json:"run_id,omitempty"`
	Status     string         `gorm:"column:status;size:32;default:'success'" json:"status"`
	Spec       datatypes.JSON `gorm:"column:spec;type:jsonb;default:'{}'"     json:"spec,omitempty"`
	CreatedAt  time.Time      `gorm:"column:created_at;not null"              json:"created_at"`
}

func (Deployment) TableName() string { return "deployments" }
