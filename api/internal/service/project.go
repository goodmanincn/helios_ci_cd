package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"gorm.io/datatypes"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
)

// 业务错误
var (
	ErrProjectNotFound      = errors.New("project not found")
	ErrProjectSlugTaken     = errors.New("project slug already exists in org")
	ErrInvalidSlug          = errors.New("invalid slug (lowercase letters/digits/hyphen, 2-64 chars)")
	ErrInvalidRepoURL       = errors.New("invalid repo url")
	ErrUnsupportedRepoType  = errors.New("unsupported repo type")
	ErrInvalidVisibility    = errors.New("visibility must be 'private' or 'public'")
	ErrProjectNameRequired  = errors.New("project name is required")
)

var (
	slugRe       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)
	allowedRepos = map[string]bool{"github": true, "gitlab": true, "gitee": true, "bitbucket": true}
	allowedVis   = map[string]bool{"private": true, "public": true}
)

// CreateProjectInput 创建项目的入参。
type CreateProjectInput struct {
	OrgID         int64
	CreatedBy     int64
	Name          string
	Slug          string
	Description   string
	RepoURL       string
	RepoType      string // github / gitlab / ...
	DefaultBranch string
	Visibility    string
}

// UpdateProjectInput 更新可变字段。slug/repo_url/repo_type 锁死。
type UpdateProjectInput struct {
	OrgID         int64
	ID            int64
	Name          *string
	Description   *string
	DefaultBranch *string
	Visibility    *string
}

// ProjectService 项目业务逻辑。
type ProjectService struct {
	repo repository.ProjectRepository
}

func NewProjectService(repo repository.ProjectRepository) *ProjectService {
	return &ProjectService{repo: repo}
}

// Create 创建项目,做完整业务校验。
func (s *ProjectService) Create(ctx context.Context, in CreateProjectInput) (*model.Project, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(strings.ToLower(in.Slug))
	in.RepoURL = strings.TrimSpace(in.RepoURL)
	in.RepoType = strings.TrimSpace(strings.ToLower(in.RepoType))
	if in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	if in.Visibility == "" {
		in.Visibility = "private"
	}

	if in.Name == "" {
		return nil, ErrProjectNameRequired
	}
	if !slugRe.MatchString(in.Slug) {
		return nil, ErrInvalidSlug
	}
	if !allowedRepos[in.RepoType] {
		return nil, ErrUnsupportedRepoType
	}
	if err := validateRepoURL(in.RepoURL); err != nil {
		return nil, err
	}
	if !allowedVis[in.Visibility] {
		return nil, ErrInvalidVisibility
	}

	// slug 在 org 内唯一
	existing, err := s.repo.GetBySlug(ctx, in.OrgID, in.Slug)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, ErrProjectSlugTaken
	}

	p := &model.Project{
		OrgID:         in.OrgID,
		Name:          in.Name,
		Slug:          in.Slug,
		Description:   in.Description,
		RepoURL:       in.RepoURL,
		RepoType:      in.RepoType,
		DefaultBranch: in.DefaultBranch,
		Visibility:    in.Visibility,
		Config:        datatypes.JSON([]byte(`{}`)),
		CreatedBy:     &in.CreatedBy,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// GetByID 单个查询(强制带 org 隔离)。
func (s *ProjectService) GetByID(ctx context.Context, orgID, id int64) (*model.Project, error) {
	p, err := s.repo.GetByID(ctx, orgID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrProjectNotFound
	}
	return p, err
}

// List 列表(带筛选 + 分页)。
func (s *ProjectService) List(ctx context.Context, f repository.ListProjectsFilter) ([]model.Project, int64, error) {
	return s.repo.List(ctx, f)
}

// Update 修改可变字段。
func (s *ProjectService) Update(ctx context.Context, in UpdateProjectInput) (*model.Project, error) {
	cur, err := s.GetByID(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, ErrProjectNameRequired
		}
		cur.Name = n
	}
	if in.Description != nil {
		cur.Description = *in.Description
	}
	if in.DefaultBranch != nil && *in.DefaultBranch != "" {
		cur.DefaultBranch = *in.DefaultBranch
	}
	if in.Visibility != nil {
		v := strings.ToLower(*in.Visibility)
		if !allowedVis[v] {
			return nil, ErrInvalidVisibility
		}
		cur.Visibility = v
	}
	if err := s.repo.Update(ctx, cur); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrProjectNotFound
		}
		return nil, fmt.Errorf("update project: %w", err)
	}
	return cur, nil
}

// Delete 删除项目。
func (s *ProjectService) Delete(ctx context.Context, orgID, id int64) error {
	err := s.repo.Delete(ctx, orgID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrProjectNotFound
	}
	return err
}

// validateRepoURL 接受 http(s)://... 或 git@host:owner/repo.git。
func validateRepoURL(raw string) error {
	if raw == "" {
		return ErrInvalidRepoURL
	}
	// SSH 形式: git@github.com:owner/repo.git
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidRepoURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidRepoURL
	}
	if u.Host == "" {
		return ErrInvalidRepoURL
	}
	return nil
}
