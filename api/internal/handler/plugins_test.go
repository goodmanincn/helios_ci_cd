// plugins_test.go — PluginHandler 集成测试 (M9).
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

func newPluginRouter(tx *gorm.DB, uid, orgID int64) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(fakeAuthWithOrg(uid, orgID))
	NewPluginHandler(tx).Register(g)
	return r
}

func withPluginTx(t *testing.T, fn func(tx *gorm.DB, orgID, uid int64)) {
	t.Helper()
	gdb := openHGTestDB(t)
	tx := gdb.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback() })
	orgID, uid := seedOrgUser(t, tx)
	fn(tx, orgID, uid)
}

// seedPlugin — 直接 INSERT plugins + plugin_versions.
func seedPlugin(t *testing.T, tx *gorm.DB, ns, name, version string, official bool) (*model.Plugin, *model.PluginVersion) {
	t.Helper()
	p := &model.Plugin{
		Namespace: ns, Name: name,
		Description:   "test plugin",
		Category:      "demo",
		Publisher:     "Test",
		Verified:      official,
		Official:      official,
		LatestVersion: version,
	}
	require.NoError(t, tx.Create(p).Error)
	yml := `name: ` + name + `
runs:
  using: container
  image: alpine:3
`
	v := &model.PluginVersion{
		PluginID:   p.ID,
		Version:    version,
		ActionYML:  yml,
		ActionSpec: datatypes.JSON([]byte(`{"name":"` + name + `"}`)),
		IsLatest:   true,
	}
	require.NoError(t, tx.Create(v).Error)
	// 重新查回 slug (是 generated column)
	require.NoError(t, tx.First(p, p.ID).Error)
	return p, v
}

// ===== 列表 =====

func TestPlugin_List_Basic(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		seedPlugin(t, tx, "ns1-"+randSuffix(), "p1", "v1", true)
		seedPlugin(t, tx, "ns2-"+randSuffix(), "p2", "v1", false)

		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "GET", "/api/v1/plugins", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var list []model.Plugin
		require.NoError(t, json.Unmarshal(body, &list))
		require.GreaterOrEqual(t, len(list), 2)
	})
}

func TestPlugin_List_FilterVerified(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		seedPlugin(t, tx, "v1-"+randSuffix(), "p", "v1", true)
		seedPlugin(t, tx, "n1-"+randSuffix(), "p", "v1", false)

		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "GET", "/api/v1/plugins?verified=true", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var list []model.Plugin
		require.NoError(t, json.Unmarshal(body, &list))
		for _, p := range list {
			require.True(t, p.Verified, "all returned plugins must be verified")
		}
	})
}

// ===== 详情 =====

func TestPlugin_Get_IncludesVersions(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		ns := "g-" + randSuffix()
		p, _ := seedPlugin(t, tx, ns, "foo", "v1", true)

		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "GET", "/api/v1/plugins/"+ns+"/foo", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "body=%s", body)

		var got struct {
			Plugin    model.Plugin          `json:"plugin"`
			Versions  []model.PluginVersion `json:"versions"`
			Installed *bool                 `json:"installed,omitempty"`
		}
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, p.ID, got.Plugin.ID)
		require.Len(t, got.Versions, 1)
		require.Nil(t, got.Installed)
	})
}

func TestPlugin_Get_NotFound(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "GET", "/api/v1/plugins/nobody/nothing", nil)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// ===== 安装 + 卸载 =====

func TestPlugin_InstallThenUninstall(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		ns := "i-" + randSuffix()
		p, v := seedPlugin(t, tx, ns, "echo", "v1", true)

		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()

		// 安装
		resp, body := doJSON(t, srv, "POST", "/api/v1/plugins/"+ns+"/echo/install",
			map[string]any{"version": "v1"})
		require.Equal(t, http.StatusCreated, resp.StatusCode, "body=%s", body)

		var inst struct {
			PluginID  int64  `json:"plugin_id"`
			VersionID int64  `json:"version_id"`
			Version   string `json:"version"`
			OrgID     int64  `json:"org_id"`
		}
		require.NoError(t, json.Unmarshal(body, &inst))
		require.Equal(t, p.ID, inst.PluginID)
		require.Equal(t, v.ID, inst.VersionID)
		require.Equal(t, orgID, inst.OrgID)

		// 列出已安装
		resp, body = doJSON(t, srv, "GET", "/api/v1/plugins/installed", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var list []map[string]any
		require.NoError(t, json.Unmarshal(body, &list))
		require.Len(t, list, 1)

		// 详情应显示 installed=true
		resp, body = doJSON(t, srv, "GET", "/api/v1/plugins/"+ns+"/echo", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var detail struct {
			Installed        *bool  `json:"installed,omitempty"`
			InstalledVersion string `json:"installed_version,omitempty"`
		}
		require.NoError(t, json.Unmarshal(body, &detail))
		require.NotNil(t, detail.Installed)
		require.True(t, *detail.Installed)
		require.Equal(t, "v1", detail.InstalledVersion)

		// 卸载
		resp, _ = doJSON(t, srv, "DELETE", "/api/v1/plugins/"+ns+"/echo/install", nil)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)

		// 再列出应为空
		resp, body = doJSON(t, srv, "GET", "/api/v1/plugins/installed", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, json.Unmarshal(body, &list))
		require.Len(t, list, 0)
	})
}

func TestPlugin_Install_LatestVersionOmitted(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		ns := "l-" + randSuffix()
		seedPlugin(t, tx, ns, "echo", "v1", true)

		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()

		// body 不带 version → 走 latest
		resp, body := doJSON(t, srv, "POST", "/api/v1/plugins/"+ns+"/echo/install", nil)
		require.Equal(t, http.StatusCreated, resp.StatusCode, "body=%s", body)
	})
}

func TestPlugin_Install_PluginNotFound(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()
		resp, _ := doJSON(t, srv, "POST", "/api/v1/plugins/nope/x/install",
			map[string]any{"version": "v1"})
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestPlugin_Reinstall_SwitchesVersion(t *testing.T) {
	withPluginTx(t, func(tx *gorm.DB, orgID, uid int64) {
		ns := "r-" + randSuffix()
		p, _ := seedPlugin(t, tx, ns, "echo", "v1", true)

		// 加一个 v2
		v2 := &model.PluginVersion{
			PluginID:   p.ID,
			Version:    "v2",
			ActionYML:  "name: echo\nruns:\n  using: container\n  image: alpine\n",
			ActionSpec: datatypes.JSON([]byte(`{}`)),
			IsLatest:   false,
		}
		require.NoError(t, tx.Create(v2).Error)

		srv := httptest.NewServer(newPluginRouter(tx, uid, orgID))
		defer srv.Close()

		// 装 v1
		resp, _ := doJSON(t, srv, "POST", "/api/v1/plugins/"+ns+"/echo/install",
			map[string]any{"version": "v1"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		// 切到 v2
		resp, body := doJSON(t, srv, "POST", "/api/v1/plugins/"+ns+"/echo/install",
			map[string]any{"version": "v2"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var got struct {
			Version string `json:"version"`
		}
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, "v2", got.Version)
	})
}
