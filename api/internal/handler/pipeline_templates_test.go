// pipeline_templates_test.go — PipelineTemplateHandler 集成测试 (M8 T8.2.1).
//
// 跟 host_groups_test 同范式: PG + tx + 回滚 + fakeAuthWithOrg.
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// 一个最简的合法 DSL, 用来测试 spec 校验路径.
const tmplTestSpec = `version: "1"
name: "test-tmpl"
stages:
  - id: echo
    runs-on: { type: container, image: alpine:latest }
    steps:
      - run: echo hello
`

// 故意写错的 DSL (version 缺失).
const tmplBadSpec = `name: "broken"
stages:
  - id: a
    runs-on: { type: container, image: x }
    steps: [{ run: "echo" }]
`

func newTmplRouter(tx *gorm.DB, uid, orgID int64) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(fakeAuthWithOrg(uid, orgID))
	NewPipelineTemplateHandler(tx).Register(g)
	return r
}

// withTmplTx — 与 host_groups_test 共用 openHGTestDB / seedOrgUser.
func withTmplTx(t *testing.T, fn func(tx *gorm.DB, orgID, uid int64)) {
	t.Helper()
	gdb := openHGTestDB(t)
	tx := gdb.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback() })
	orgID, uid := seedOrgUser(t, tx)
	fn(tx, orgID, uid)
}

// seedTemplate — 直接 INSERT, 不走 handler. private = true 时 org_id 设当前 org, 否则全局.
func seedTemplate(t *testing.T, tx *gorm.DB, slug string, orgID *int64, builtin bool) *model.PipelineTemplate {
	t.Helper()
	tmpl := &model.PipelineTemplate{
		Slug:    slug,
		Name:    slug,
		SpecRaw: tmplTestSpec,
		Spec:    datatypes.JSON([]byte(`{}`)),
		Builtin: builtin,
		OrgID:   orgID,
	}
	require.NoError(t, tx.Create(tmpl).Error)
	return tmpl
}

// ===== 列表 =====

func TestPipelineTemplate_List_MergesGlobalAndOrgScoped(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		seedTemplate(t, tx, "global-1-"+randSuffix(), nil, true) // 全局 builtin
		oid := orgID
		seedTemplate(t, tx, "private-1-"+randSuffix(), &oid, false) // 本 org 私有

		// 另一个 org 的私有模板不应被看到
		otherOrg, _ := seedOrgUser(t, tx)
		seedTemplate(t, tx, "private-other-"+randSuffix(), &otherOrg, false)

		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "GET", "/api/v1/pipeline-templates", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var list []model.PipelineTemplate
		require.NoError(t, json.Unmarshal(body, &list))
		// 至少 2 条 (全局 + 私有), 不会包含 other org
		require.GreaterOrEqual(t, len(list), 2)
		for _, tm := range list {
			if tm.OrgID != nil && *tm.OrgID != orgID {
				t.Fatalf("leaked foreign template: %+v", tm)
			}
		}
	})
}

// ===== 创建 =====

func TestPipelineTemplate_Create_OK(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()

		slug := "ct-" + randSuffix()
		resp, body := doJSON(t, srv, "POST", "/api/v1/pipeline-templates", map[string]any{
			"slug":     slug,
			"name":     "Created",
			"category": "deploy",
			"tags":     []string{"go"},
			"spec_raw": tmplTestSpec,
		})
		require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
		var out model.PipelineTemplate
		require.NoError(t, json.Unmarshal(body, &out))
		require.Equal(t, slug, out.Slug)
		require.NotNil(t, out.OrgID)
		require.Equal(t, orgID, *out.OrgID)
		require.False(t, out.Builtin) // 用户创建的永远不是 builtin
	})
}

func TestPipelineTemplate_Create_InvalidSpecRejected(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "POST", "/api/v1/pipeline-templates", map[string]any{
			"slug":     "bad-" + randSuffix(),
			"name":     "Bad",
			"spec_raw": tmplBadSpec,
		})
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})
}

// ===== 不可修改 builtin =====

func TestPipelineTemplate_BuiltinReadOnly(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		tmpl := seedTemplate(t, tx, "builtin-"+randSuffix(), nil, true)
		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()

		// 改 builtin 应被禁
		resp, _ := doJSON(t, srv, "PUT", "/api/v1/pipeline-templates/"+itoa64(tmpl.ID),
			map[string]any{"name": "hacked"})
		require.Equal(t, http.StatusForbidden, resp.StatusCode)

		// 删 builtin 应被禁
		resp, _ = doJSON(t, srv, "DELETE", "/api/v1/pipeline-templates/"+itoa64(tmpl.ID), nil)
		require.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestPipelineTemplate_CrossOrg_NotFound(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		otherOrg, _ := seedOrgUser(t, tx)
		tmpl := seedTemplate(t, tx, "foreign-"+randSuffix(), &otherOrg, false)

		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "GET", "/api/v1/pipeline-templates/"+itoa64(tmpl.ID), nil)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// ===== 克隆 =====

func TestPipelineTemplate_Clone_BySlug(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		tmpl := seedTemplate(t, tx, "tpl-"+randSuffix(), nil, true)

		// 建一个属于当前 org 的 project
		proj := model.Project{
			OrgID:    orgID,
			Name:     "demo",
			Slug:     "demo-" + randSuffix(),
			RepoURL:  "https://github.com/x/y",
			RepoType: "github",
			Config:   datatypes.JSON([]byte(`{}`)),
		}
		require.NoError(t, tx.Create(&proj).Error)

		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "POST", "/api/v1/pipelines/from-template", map[string]any{
			"template_slug": tmpl.Slug,
			"project_id":    proj.ID,
			"name":          "from-template",
		})
		require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))

		var cloned struct {
			PipelineID   int64  `json:"pipeline_id"`
			VersionID    int64  `json:"version_id"`
			Version      int    `json:"version"`
			TemplateSlug string `json:"template_slug"`
		}
		require.NoError(t, json.Unmarshal(body, &cloned))
		require.Equal(t, tmpl.Slug, cloned.TemplateSlug)
		require.Equal(t, 1, cloned.Version)

		// pipeline 应已落库, current_version_id 指向新版本
		var p model.Pipeline
		require.NoError(t, tx.First(&p, cloned.PipelineID).Error)
		require.NotNil(t, p.CurrentVersionID)
		require.Equal(t, cloned.VersionID, *p.CurrentVersionID)

		// pipeline_version 的 spec_raw 应与模板一致
		var pv model.PipelineVersion
		require.NoError(t, tx.First(&pv, cloned.VersionID).Error)
		require.Equal(t, tmpl.SpecRaw, pv.SpecRaw)
	})
}

func TestPipelineTemplate_Clone_ForeignProjectRejected(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		tmpl := seedTemplate(t, tx, "tpl-"+randSuffix(), nil, true)

		// 另一个 org 的 project
		otherOrg, _ := seedOrgUser(t, tx)
		proj := model.Project{
			OrgID:    otherOrg,
			Name:     "foreign",
			Slug:     "foreign-" + randSuffix(),
			RepoURL:  "https://github.com/a/b",
			RepoType: "github",
			Config:   datatypes.JSON([]byte(`{}`)),
		}
		require.NoError(t, tx.Create(&proj).Error)

		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "POST", "/api/v1/pipelines/from-template", map[string]any{
			"template_id": tmpl.ID,
			"project_id":  proj.ID,
			"name":        "n",
		})
		require.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestPipelineTemplate_Clone_TemplateNotFound(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "POST", "/api/v1/pipelines/from-template", map[string]any{
			"template_slug": "no-such-template-" + randSuffix(),
			"project_id":    1,
			"name":          "x",
		})
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestPipelineTemplate_Clone_MissingTemplateRef(t *testing.T) {
	withTmplTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newTmplRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "POST", "/api/v1/pipelines/from-template", map[string]any{
			"project_id": 1,
			"name":       "x",
		})
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}
