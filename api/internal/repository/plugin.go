// Package repository — plugin.go: 插件市场存储 (M9).
//
// 设计:
//   - List 公开 (跨 org), 因为插件本身是全局资源; 隔离粒度在 plugin_installations (org_id).
//   - Get/GetBySlug 返回 plugin + 全部 versions, 详情页一次拉完.
//   - InstallToOrg 走 ON CONFLICT DO UPDATE: 同 org 重复装 → 切版本.
//   - ListInstalled 按 org 拉, JOIN plugins + plugin_versions.
package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

var (
	ErrPluginNotFound        = errors.New("plugin not found")
	ErrPluginVersionNotFound = errors.New("plugin version not found")
	ErrPluginOfficial        = errors.New("official plugins cannot be removed from registry")
)

// PluginFilter 列表过滤.
type PluginFilter struct {
	Category string
	Q        string // namespace/name/description 模糊
	Verified bool   // true 时只返 verified
}

type PluginStore interface {
	List(filter PluginFilter) ([]model.Plugin, error)
	GetBySlug(slug string) (*model.Plugin, []model.PluginVersion, error)
	GetVersionByName(pluginID int64, version string) (*model.PluginVersion, error)

	InstallToOrg(orgID, pluginID, versionID int64, installedBy *int64) (*model.PluginInstallation, error)
	Uninstall(orgID, pluginID int64) error
	ListInstalled(orgID int64) ([]InstalledPlugin, error)
	GetInstallation(orgID, pluginID int64) (*model.PluginInstallation, error)

	IncrementDownloads(pluginID int64) error
}

// InstalledPlugin 已安装视图 (handler/UI 友好).
type InstalledPlugin struct {
	Installation model.PluginInstallation `json:"installation"`
	Plugin       model.Plugin             `json:"plugin"`
	Version      model.PluginVersion      `json:"version"`
}

type GormPluginStore struct{ db *gorm.DB }

func NewPluginRepository(db *gorm.DB) *GormPluginStore { return &GormPluginStore{db: db} }

func (s *GormPluginStore) List(f PluginFilter) ([]model.Plugin, error) {
	q := s.db.Model(&model.Plugin{}).Where("deleted_at IS NULL")
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if f.Verified {
		q = q.Where("verified = TRUE")
	}
	if f.Q != "" {
		like := "%" + f.Q + "%"
		q = q.Where("namespace ILIKE ? OR name ILIKE ? OR description ILIKE ?", like, like, like)
	}
	var list []model.Plugin
	err := q.Order("verified DESC, downloads DESC, id ASC").Find(&list).Error
	return list, err
}

func (s *GormPluginStore) GetBySlug(slug string) (*model.Plugin, []model.PluginVersion, error) {
	var p model.Plugin
	if err := s.db.Where("slug = ? AND deleted_at IS NULL", slug).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrPluginNotFound
		}
		return nil, nil, err
	}
	var versions []model.PluginVersion
	if err := s.db.Where("plugin_id = ?", p.ID).Order("id DESC").Find(&versions).Error; err != nil {
		return &p, nil, err
	}
	return &p, versions, nil
}

func (s *GormPluginStore) GetVersionByName(pluginID int64, version string) (*model.PluginVersion, error) {
	var v model.PluginVersion
	q := s.db.Where("plugin_id = ?", pluginID)
	if version == "latest" || version == "" {
		q = q.Where("is_latest = TRUE")
	} else {
		q = q.Where("version = ?", version)
	}
	if err := q.First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPluginVersionNotFound
		}
		return nil, err
	}
	return &v, nil
}

func (s *GormPluginStore) InstallToOrg(orgID, pluginID, versionID int64, installedBy *int64) (*model.PluginInstallation, error) {
	var existing model.PluginInstallation
	err := s.db.Where("org_id = ? AND plugin_id = ?", orgID, pluginID).First(&existing).Error
	if err == nil {
		// 已装 → 更新版本即可
		existing.VersionID = versionID
		if installedBy != nil {
			existing.InstalledBy = installedBy
		}
		if err := s.db.Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	inst := &model.PluginInstallation{
		OrgID:       orgID,
		PluginID:    pluginID,
		VersionID:   versionID,
		InstalledBy: installedBy,
	}
	if err := s.db.Create(inst).Error; err != nil {
		return nil, err
	}
	return inst, nil
}

func (s *GormPluginStore) Uninstall(orgID, pluginID int64) error {
	res := s.db.Where("org_id = ? AND plugin_id = ?", orgID, pluginID).
		Delete(&model.PluginInstallation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPluginNotFound
	}
	return nil
}

func (s *GormPluginStore) ListInstalled(orgID int64) ([]InstalledPlugin, error) {
	var insts []model.PluginInstallation
	if err := s.db.Where("org_id = ?", orgID).Order("installed_at DESC").Find(&insts).Error; err != nil {
		return nil, err
	}
	out := make([]InstalledPlugin, 0, len(insts))
	for _, in := range insts {
		var p model.Plugin
		if err := s.db.First(&p, in.PluginID).Error; err != nil {
			continue
		}
		var v model.PluginVersion
		if err := s.db.First(&v, in.VersionID).Error; err != nil {
			continue
		}
		out = append(out, InstalledPlugin{Installation: in, Plugin: p, Version: v})
	}
	return out, nil
}

func (s *GormPluginStore) GetInstallation(orgID, pluginID int64) (*model.PluginInstallation, error) {
	var inst model.PluginInstallation
	if err := s.db.Where("org_id = ? AND plugin_id = ?", orgID, pluginID).First(&inst).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPluginNotFound
		}
		return nil, err
	}
	return &inst, nil
}

func (s *GormPluginStore) IncrementDownloads(pluginID int64) error {
	return s.db.Model(&model.Plugin{}).Where("id = ?", pluginID).
		UpdateColumn("downloads", gorm.Expr("downloads + 1")).Error
}
