package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// ErrClusterNotFound sentinel.
var ErrClusterNotFound = errors.New("cluster not found")

// ClusterStore 集群数据接口。
type ClusterStore interface {
	Create(c *model.Cluster) error
	Get(id int64) (*model.Cluster, error)
	GetByName(orgID int64, name string) (*model.Cluster, error)
	ListByOrg(orgID int64) ([]model.Cluster, error)
	Update(c *model.Cluster) error
	Delete(id int64) error
}

// GormClusterStore GORM 实现。
type GormClusterStore struct {
	db *gorm.DB
}

func NewClusterRepository(db *gorm.DB) *GormClusterStore {
	return &GormClusterStore{db: db}
}

func (s *GormClusterStore) Create(c *model.Cluster) error {
	return s.db.Create(c).Error
}

func (s *GormClusterStore) Get(id int64) (*model.Cluster, error) {
	var c model.Cluster
	if err := s.db.First(&c, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrClusterNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *GormClusterStore) GetByName(orgID int64, name string) (*model.Cluster, error) {
	var c model.Cluster
	if err := s.db.Where("org_id = ? AND name = ?", orgID, name).First(&c).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrClusterNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *GormClusterStore) ListByOrg(orgID int64) ([]model.Cluster, error) {
	var list []model.Cluster
	err := s.db.Where("org_id = ?", orgID).Order("id DESC").Find(&list).Error
	return list, err
}

func (s *GormClusterStore) Update(c *model.Cluster) error {
	return s.db.Save(c).Error
}

func (s *GormClusterStore) Delete(id int64) error {
	return s.db.Delete(&model.Cluster{}, id).Error
}
