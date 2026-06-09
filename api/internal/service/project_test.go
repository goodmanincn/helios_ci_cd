package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/internal/service"
)

// ===== in-memory mock repo =====

type memProjectRepo struct {
	byID   map[int64]*model.Project
	bySlug map[string]*model.Project // key: orgID|slug
	seq    int64
	// hooks 让测试可以注入错误
	createErr error
}

func newMem() *memProjectRepo {
	return &memProjectRepo{byID: map[int64]*model.Project{}, bySlug: map[string]*model.Project{}}
}

func key(org int64, slug string) string {
	return slugKey(org, slug)
}
func slugKey(org int64, slug string) string {
	return string(rune(org)) + "|" + slug
}

func (m *memProjectRepo) Create(_ context.Context, p *model.Project) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.seq++
	p.ID = m.seq
	cp := *p
	m.byID[p.ID] = &cp
	m.bySlug[key(p.OrgID, p.Slug)] = &cp
	return nil
}

func (m *memProjectRepo) GetByID(_ context.Context, orgID, id int64) (*model.Project, error) {
	if p, ok := m.byID[id]; ok && p.OrgID == orgID {
		cp := *p
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}

func (m *memProjectRepo) GetBySlug(_ context.Context, orgID int64, slug string) (*model.Project, error) {
	if p, ok := m.bySlug[key(orgID, slug)]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}

func (m *memProjectRepo) List(_ context.Context, f repository.ListProjectsFilter) ([]model.Project, int64, error) {
	out := make([]model.Project, 0)
	for _, p := range m.byID {
		if p.OrgID != f.OrgID {
			continue
		}
		out = append(out, *p)
	}
	return out, int64(len(out)), nil
}

func (m *memProjectRepo) Update(_ context.Context, p *model.Project) error {
	cur, ok := m.byID[p.ID]
	if !ok || cur.OrgID != p.OrgID {
		return repository.ErrNotFound
	}
	cp := *p
	m.byID[p.ID] = &cp
	m.bySlug[key(p.OrgID, p.Slug)] = &cp
	return nil
}

func (m *memProjectRepo) Delete(_ context.Context, orgID, id int64) error {
	cur, ok := m.byID[id]
	if !ok || cur.OrgID != orgID {
		return repository.ErrNotFound
	}
	delete(m.byID, id)
	delete(m.bySlug, key(cur.OrgID, cur.Slug))
	return nil
}

// ===== 测试 =====

func validInput(orgID int64, slug string) service.CreateProjectInput {
	return service.CreateProjectInput{
		OrgID: orgID, CreatedBy: 1, Name: "API Gateway",
		Slug: slug, RepoURL: "https://github.com/acme/api.git", RepoType: "github",
	}
}

func TestCreateProject_Happy(t *testing.T) {
	svc := service.NewProjectService(newMem())
	p, err := svc.Create(context.Background(), validInput(3, "api"))
	require.NoError(t, err)
	require.Equal(t, "api", p.Slug)
	require.Equal(t, "main", p.DefaultBranch) // 默认值
	require.Equal(t, "private", p.Visibility) // 默认值
	require.True(t, p.ID > 0)
	require.NotNil(t, p.CreatedBy)
}

func TestCreateProject_SlugTaken(t *testing.T) {
	svc := service.NewProjectService(newMem())
	_, err := svc.Create(context.Background(), validInput(3, "api"))
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), validInput(3, "api"))
	require.ErrorIs(t, err, service.ErrProjectSlugTaken)
	// 不同 org 同 slug 应该 OK
	_, err = svc.Create(context.Background(), validInput(4, "api"))
	require.NoError(t, err)
}

func TestCreateProject_InvalidInputs(t *testing.T) {
	svc := service.NewProjectService(newMem())
	cases := []struct {
		name string
		mut  func(*service.CreateProjectInput)
		want error
	}{
		{"empty name", func(in *service.CreateProjectInput) { in.Name = "  " }, service.ErrProjectNameRequired},
		{"bad slug uppercase", func(in *service.CreateProjectInput) { in.Slug = "API" }, nil}, // 会被 lower 通过
		{"bad slug special", func(in *service.CreateProjectInput) { in.Slug = "api_gateway" }, service.ErrInvalidSlug},
		{"bad slug too short", func(in *service.CreateProjectInput) { in.Slug = "a" }, service.ErrInvalidSlug},
		{"bad slug leading hyphen", func(in *service.CreateProjectInput) { in.Slug = "-api" }, service.ErrInvalidSlug},
		{"unsupported repo type", func(in *service.CreateProjectInput) { in.RepoType = "svn" }, service.ErrUnsupportedRepoType},
		{"bad repo url", func(in *service.CreateProjectInput) { in.RepoURL = "ftp://x/y" }, service.ErrInvalidRepoURL},
		{"bad repo url empty", func(in *service.CreateProjectInput) { in.RepoURL = "" }, service.ErrInvalidRepoURL},
		{"bad visibility", func(in *service.CreateProjectInput) { in.Visibility = "unknown" }, service.ErrInvalidVisibility},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(3, fmt.Sprintf("project-%d", i))
			tc.mut(&in)
			_, err := svc.Create(context.Background(), in)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
}

func TestCreateProject_SSHRepoURL(t *testing.T) {
	svc := service.NewProjectService(newMem())
	in := validInput(3, "api")
	in.RepoURL = "git@github.com:acme/api.git"
	_, err := svc.Create(context.Background(), in)
	require.NoError(t, err)
}

func TestUpdateProject(t *testing.T) {
	svc := service.NewProjectService(newMem())
	p, _ := svc.Create(context.Background(), validInput(3, "api"))

	name := "New Name"
	vis := "public"
	branch := "develop"
	updated, err := svc.Update(context.Background(), service.UpdateProjectInput{
		OrgID: 3, ID: p.ID,
		Name: &name, Visibility: &vis, DefaultBranch: &branch,
	})
	require.NoError(t, err)
	require.Equal(t, "New Name", updated.Name)
	require.Equal(t, "public", updated.Visibility)
	require.Equal(t, "develop", updated.DefaultBranch)
}

func TestUpdateProject_InvalidVisibility(t *testing.T) {
	svc := service.NewProjectService(newMem())
	p, _ := svc.Create(context.Background(), validInput(3, "api"))
	bad := "secret"
	_, err := svc.Update(context.Background(), service.UpdateProjectInput{
		OrgID: 3, ID: p.ID, Visibility: &bad,
	})
	require.ErrorIs(t, err, service.ErrInvalidVisibility)
}

func TestUpdate_OrgIsolation(t *testing.T) {
	svc := service.NewProjectService(newMem())
	p, _ := svc.Create(context.Background(), validInput(3, "api"))
	// 用另一个 org 试图更新 — 应当 NotFound
	name := "stolen"
	_, err := svc.Update(context.Background(), service.UpdateProjectInput{
		OrgID: 999, ID: p.ID, Name: &name,
	})
	require.ErrorIs(t, err, service.ErrProjectNotFound)
}

func TestGetByID_NotFound(t *testing.T) {
	svc := service.NewProjectService(newMem())
	_, err := svc.GetByID(context.Background(), 3, 1234)
	require.ErrorIs(t, err, service.ErrProjectNotFound)
}

func TestDelete(t *testing.T) {
	svc := service.NewProjectService(newMem())
	p, _ := svc.Create(context.Background(), validInput(3, "api"))
	require.NoError(t, svc.Delete(context.Background(), 3, p.ID))
	_, err := svc.GetByID(context.Background(), 3, p.ID)
	require.ErrorIs(t, err, service.ErrProjectNotFound)
}

func TestDelete_OrgIsolation(t *testing.T) {
	svc := service.NewProjectService(newMem())
	p, _ := svc.Create(context.Background(), validInput(3, "api"))
	err := svc.Delete(context.Background(), 999, p.ID)
	require.ErrorIs(t, err, service.ErrProjectNotFound)
}

func TestCreate_RepoError(t *testing.T) {
	mem := newMem()
	mem.createErr = errors.New("boom")
	svc := service.NewProjectService(mem)
	_, err := svc.Create(context.Background(), validInput(3, "api"))
	require.Error(t, err)
}

func TestList(t *testing.T) {
	mem := newMem()
	svc := service.NewProjectService(mem)
	for _, slug := range []string{"a-svc", "b-svc", "c-svc"} {
		_, _ = svc.Create(context.Background(), validInput(3, slug))
	}
	// 不同 org 的不该混入
	_, _ = svc.Create(context.Background(), validInput(99, "z-svc"))

	items, total, err := svc.List(context.Background(), repository.ListProjectsFilter{OrgID: 3, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, items, 3)
}
