package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

var ErrHostNotFound = errors.New("host not found")

type HostStore interface {
	Create(h *model.Host) error
	Get(id int64) (*model.Host, error)
	GetByName(orgID int64, name string) (*model.Host, error)
	ListByOrg(orgID int64) ([]model.Host, error)
	Update(h *model.Host) error
	Delete(id int64) error
}

type GormHostStore struct {
	db *gorm.DB
}

func NewHostRepository(db *gorm.DB) *GormHostStore {
	return &GormHostStore{db: db}
}

func (s *GormHostStore) Create(h *model.Host) error {
	return s.db.Create(h).Error
}

func (s *GormHostStore) Get(id int64) (*model.Host, error) {
	var h model.Host
	if err := s.db.First(&h, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrHostNotFound
		}
		return nil, err
	}
	return &h, nil
}

func (s *GormHostStore) GetByName(orgID int64, name string) (*model.Host, error) {
	var h model.Host
	if err := s.db.Where("org_id = ? AND name = ?", orgID, name).First(&h).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrHostNotFound
		}
		return nil, err
	}
	return &h, nil
}

func (s *GormHostStore) ListByOrg(orgID int64) ([]model.Host, error) {
	var list []model.Host
	err := s.db.Where("org_id = ?", orgID).Order("id DESC").Find(&list).Error
	return list, err
}

func (s *GormHostStore) Update(h *model.Host) error {
	return s.db.Save(h).Error
}

func (s *GormHostStore) Delete(id int64) error {
	return s.db.Delete(&model.Host{}, id).Error
}
