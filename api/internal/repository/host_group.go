package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// HostGroup CRUD + 成员管理
//
// 设计:
//   - HostGroup 与 Host 都按 org 隔离, 成员关联表 host_group_members 没有 org_id,
//     依赖应用层保证 group 和 host 同 org (handler 校验)。
//   - Members 操作走 Upsert + Delete, 不抛 "already member" — 幂等。
//   - ListHosts 通过 join 直接拿 group 下所有 host (常用查询, 给 ssh-deploy 用)。

var ErrHostGroupNotFound = errors.New("host group not found")

type HostGroupStore interface {
	Create(g *model.HostGroup) error
	Get(id int64) (*model.HostGroup, error)
	GetByName(orgID int64, name string) (*model.HostGroup, error)
	ListByOrg(orgID int64) ([]model.HostGroup, error)
	Update(g *model.HostGroup) error
	Delete(id int64) error

	// 成员管理
	AddMember(groupID, hostID int64) error
	RemoveMember(groupID, hostID int64) error
	ListMembers(groupID int64) ([]model.Host, error)
}

type GormHostGroupStore struct {
	db *gorm.DB
}

func NewHostGroupRepository(db *gorm.DB) *GormHostGroupStore {
	return &GormHostGroupStore{db: db}
}

func (s *GormHostGroupStore) Create(g *model.HostGroup) error {
	return s.db.Create(g).Error
}

func (s *GormHostGroupStore) Get(id int64) (*model.HostGroup, error) {
	var g model.HostGroup
	if err := s.db.First(&g, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostGroupNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (s *GormHostGroupStore) GetByName(orgID int64, name string) (*model.HostGroup, error) {
	var g model.HostGroup
	if err := s.db.Where("org_id = ? AND name = ?", orgID, name).First(&g).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostGroupNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (s *GormHostGroupStore) ListByOrg(orgID int64) ([]model.HostGroup, error) {
	var list []model.HostGroup
	err := s.db.Where("org_id = ?", orgID).Order("id DESC").Find(&list).Error
	return list, err
}

func (s *GormHostGroupStore) Update(g *model.HostGroup) error {
	return s.db.Save(g).Error
}

func (s *GormHostGroupStore) Delete(id int64) error {
	// host_group_members ON DELETE CASCADE, 不用手动删
	res := s.db.Delete(&model.HostGroup{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrHostGroupNotFound
	}
	return nil
}

// AddMember 幂等加入, 已存在不报错。
func (s *GormHostGroupStore) AddMember(groupID, hostID int64) error {
	// ON CONFLICT DO NOTHING (group_id, host_id) 是复合主键
	return s.db.Exec(
		`INSERT INTO host_group_members (group_id, host_id) VALUES (?, ?)
		 ON CONFLICT (group_id, host_id) DO NOTHING`,
		groupID, hostID,
	).Error
}

// RemoveMember 幂等移除, 不存在不报错。
func (s *GormHostGroupStore) RemoveMember(groupID, hostID int64) error {
	return s.db.Where("group_id = ? AND host_id = ?", groupID, hostID).
		Delete(&model.HostGroupMember{}).Error
}

// ListMembers 返回组内所有 host (join 查询)。
func (s *GormHostGroupStore) ListMembers(groupID int64) ([]model.Host, error) {
	var hosts []model.Host
	err := s.db.
		Table("hosts").
		Joins("JOIN host_group_members m ON m.host_id = hosts.id").
		Where("m.group_id = ? AND hosts.deleted_at IS NULL", groupID).
		Order("hosts.id DESC").
		Find(&hosts).Error
	return hosts, err
}
