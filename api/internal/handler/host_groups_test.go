// host_groups_test.go — HostGroupHandler 集成测试 (T6.1.3)
//
// 依赖 PG, 跟 run_test 同范式: HELIOS_TEST_DB_DSN → tx → 测完回滚.
// 用 fakeAuth 模拟 claims (复用 approval_test 的辅助).
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

func openHGTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("HELIOS_DB_DSN / HELIOS_TEST_DB_DSN 未设置, 跳过 HostGroupHandler 集成测试")
	}
	require.NoError(t, db.Migrate(dsn))
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return gdb
}

// fakeAuthWithOrg 把 claims 注入, 让 activeOrg() 拿得到 org_id.
func fakeAuthWithOrg(uid, orgID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(middleware.CtxClaimsKey, &heliosjwt.Claims{
			UserID: uid,
			OrgIDs: []int64{orgID},
		})
		c.Set(middleware.CtxUserIDKey, uid)
		c.Next()
	}
}

func newHGRouter(tx *gorm.DB, uid, orgID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(fakeAuthWithOrg(uid, orgID))
	NewHostGroupHandler(tx).Register(g)
	NewHostHandler(tx).Register(g) // 走 ?group= 过滤路径需要
	return r
}

// seedOrgUser 建一个 org + user, 返回 (orgID, userID).
func seedOrgUser(t *testing.T, tx *gorm.DB) (int64, int64) {
	t.Helper()
	suf := randSuffix()
	u := model.User{Username: "u-" + suf, Email: suf + "@example.com"}
	require.NoError(t, tx.Create(&u).Error)
	org := model.Organization{Slug: "hg-" + suf, Name: "HG Test Org"}
	require.NoError(t, tx.Create(&org).Error)
	require.NoError(t, tx.Exec(
		`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, 'member')`,
		org.ID, u.ID,
	).Error)
	return org.ID, u.ID
}

// seedHost 建一台主机, 给指定 org.
func seedHost(t *testing.T, tx *gorm.DB, orgID int64, name string) int64 {
	t.Helper()
	h := model.Host{
		OrgID:   orgID,
		Name:    name,
		IP:      "10.0.0.1",
		SSHPort: 22,
		SSHUser: "root",
		Status:  "unknown",
	}
	require.NoError(t, tx.Create(&h).Error)
	return h.ID
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp, buf.Bytes()
}

func withHGTx(t *testing.T, fn func(tx *gorm.DB, orgID, uid int64)) {
	t.Helper()
	gdb := openHGTestDB(t)
	tx := gdb.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback() })
	orgID, uid := seedOrgUser(t, tx)
	fn(tx, orgID, uid)
}

// ---- 测试用例 ----

func TestHostGroup_CreateListGet(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		// 创建
		resp, body := doJSON(t, srv, "POST", "/api/v1/host-groups", map[string]any{
			"name": "prod-web",
			"vars": map[string]string{"env": "prod"},
		})
		require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
		var created model.HostGroup
		require.NoError(t, json.Unmarshal(body, &created))
		require.Equal(t, "prod-web", created.Name)
		require.Equal(t, orgID, created.OrgID)

		// 列表
		resp, body = doJSON(t, srv, "GET", "/api/v1/host-groups", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var list []model.HostGroup
		require.NoError(t, json.Unmarshal(body, &list))
		require.Len(t, list, 1)

		// 详情 (无成员)
		resp, body = doJSON(t, srv, "GET", "/api/v1/host-groups/"+itoa64(created.ID), nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var detail map[string]any
		require.NoError(t, json.Unmarshal(body, &detail))
		members, _ := detail["members"].([]any)
		require.Len(t, members, 0)
	})
}

// 重名 409 单独跑, 避免 PG tx aborted 状态影响其他断言.
func TestHostGroup_DuplicateNameConflict(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		resp, _ := doJSON(t, srv, "POST", "/api/v1/host-groups",
			map[string]any{"name": "dup"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		// 重名 → 409
		resp, _ = doJSON(t, srv, "POST", "/api/v1/host-groups",
			map[string]any{"name": "dup"})
		require.Equal(t, http.StatusConflict, resp.StatusCode)
	})
}

func TestHostGroup_AddRemoveMembers(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		// 准备组 + 两台主机
		resp, body := doJSON(t, srv, "POST", "/api/v1/host-groups",
			map[string]any{"name": "web"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var g model.HostGroup
		require.NoError(t, json.Unmarshal(body, &g))

		h1 := seedHost(t, tx, orgID, "host-1-"+randSuffix())
		h2 := seedHost(t, tx, orgID, "host-2-"+randSuffix())

		// 加成员
		resp, body = doJSON(t, srv, "POST", "/api/v1/host-groups/"+itoa64(g.ID)+"/members",
			map[string]any{"host_ids": []int64{h1, h2}})
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

		// 详情应有 2 成员
		resp, body = doJSON(t, srv, "GET", "/api/v1/host-groups/"+itoa64(g.ID), nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var detail map[string]any
		require.NoError(t, json.Unmarshal(body, &detail))
		require.Len(t, detail["members"].([]any), 2)

		// 重复 add 幂等 (不报错)
		resp, _ = doJSON(t, srv, "POST", "/api/v1/host-groups/"+itoa64(g.ID)+"/members",
			map[string]any{"host_ids": []int64{h1}})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// 删一个
		resp, _ = doJSON(t, srv, "DELETE", "/api/v1/host-groups/"+itoa64(g.ID)+"/members/"+itoa64(h1), nil)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)

		resp, body = doJSON(t, srv, "GET", "/api/v1/host-groups/"+itoa64(g.ID), nil)
		require.NoError(t, json.Unmarshal(body, &detail))
		require.Len(t, detail["members"].([]any), 1)
	})
}

func TestHostGroup_AddMember_ForeignOrgRejected(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "POST", "/api/v1/host-groups", map[string]any{"name": "g1"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var g model.HostGroup
		require.NoError(t, json.Unmarshal(body, &g))

		// 建另一个 org + 一台属于它的 host
		otherOrgID, _ := seedOrgUser(t, tx)
		foreignHost := seedHost(t, tx, otherOrgID, "foreign-"+randSuffix())

		resp, _ = doJSON(t, srv, "POST", "/api/v1/host-groups/"+itoa64(g.ID)+"/members",
			map[string]any{"host_ids": []int64{foreignHost}})
		require.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestHostGroup_CrossOrg_NotFound(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		// 另一个 org 建组, 当前 org 不能看见
		otherOrgID, _ := seedOrgUser(t, tx)
		var foreign model.HostGroup
		foreign = model.HostGroup{OrgID: otherOrgID, Name: "secret-" + randSuffix(),
			Vars: datatypes.JSON([]byte(`{}`))}
		require.NoError(t, tx.Create(&foreign).Error)

		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		resp, _ := doJSON(t, srv, "GET", "/api/v1/host-groups/"+itoa64(foreign.ID), nil)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestHostsList_FilterByGroup(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		// 建组 + 一台成员 + 一台非成员
		resp, body := doJSON(t, srv, "POST", "/api/v1/host-groups", map[string]any{"name": "blue"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var g model.HostGroup
		require.NoError(t, json.Unmarshal(body, &g))

		memberH := seedHost(t, tx, orgID, "blue-h-"+randSuffix())
		_ = seedHost(t, tx, orgID, "green-h-"+randSuffix())

		resp, _ = doJSON(t, srv, "POST", "/api/v1/host-groups/"+itoa64(g.ID)+"/members",
			map[string]any{"host_ids": []int64{memberH}})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// 按 group 过滤
		resp, body = doJSON(t, srv, "GET", "/api/v1/hosts?group=blue", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var hosts []model.Host
		require.NoError(t, json.Unmarshal(body, &hosts))
		require.Len(t, hosts, 1)
		require.Equal(t, memberH, hosts[0].ID)

		// 不存在的组返空列表
		resp, body = doJSON(t, srv, "GET", "/api/v1/hosts?group=nosuch", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, json.Unmarshal(body, &hosts))
		require.Len(t, hosts, 0)
	})
}

func TestHostGroup_DeleteCascadesMembers(t *testing.T) {
	withHGTx(t, func(tx *gorm.DB, orgID, uid int64) {
		srv := httptest.NewServer(newHGRouter(tx, uid, orgID))
		defer srv.Close()

		resp, body := doJSON(t, srv, "POST", "/api/v1/host-groups", map[string]any{"name": "tbd"})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var g model.HostGroup
		require.NoError(t, json.Unmarshal(body, &g))

		h := seedHost(t, tx, orgID, "h-"+randSuffix())
		resp, _ = doJSON(t, srv, "POST", "/api/v1/host-groups/"+itoa64(g.ID)+"/members",
			map[string]any{"host_ids": []int64{h}})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// 删组
		resp, _ = doJSON(t, srv, "DELETE", "/api/v1/host-groups/"+itoa64(g.ID), nil)
		require.Equal(t, http.StatusNoContent, resp.StatusCode)

		// 成员关联应被级联删 (host_group_members ON DELETE CASCADE)
		var count int64
		require.NoError(t, tx.Table("host_group_members").
			Where("group_id = ?", g.ID).Count(&count).Error)
		require.Zero(t, count)

		// 但 host 本身不能被删
		var hostCount int64
		require.NoError(t, tx.Model(&model.Host{}).Where("id = ?", h).Count(&hostCount).Error)
		require.Equal(t, int64(1), hostCount)
	})
}
