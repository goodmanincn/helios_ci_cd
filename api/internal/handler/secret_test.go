// secret_test.go — SecretHandler CRUD + 加密 round-trip + 503 降级.
//
// 不依赖真 auth (用 fakeAuth 中间件直接塞 claims), 但依赖真 PG (HELIOS_TEST_DB_DSN).
package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/crypto"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

func newSecretFixture(t *testing.T, gdb *gorm.DB) int64 {
	t.Helper()
	org := model.Organization{Slug: "sec-" + randSuffix(), Name: "Sec Org"}
	require.NoError(t, gdb.Create(&org).Error)
	t.Cleanup(func() {
		_ = gdb.Exec("DELETE FROM secrets WHERE scope_id = ?", org.ID).Error
		_ = gdb.Exec("DELETE FROM organizations WHERE id = ?", org.ID).Error
	})
	return org.ID
}

func newVaultForTest(t *testing.T) *crypto.Vault {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	kek, err := crypto.NewKEK("test-v1", key)
	require.NoError(t, err)
	v, err := crypto.NewVault(kek)
	require.NoError(t, err)
	return v
}

// fakeAuthMW 把 claims 直接塞入 ctx, 跳过 JWT 校验.
func fakeAuthMW(uid int64, orgID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := &heliosjwt.Claims{
			UserID: uid,
			OrgIDs: []int64{orgID},
			Roles:  []string{"owner"},
		}
		c.Set(middleware.CtxClaimsKey, claims)
		c.Set(middleware.CtxUserIDKey, uid)
		c.Next()
	}
}

func newSecretRouter(t *testing.T, gdb *gorm.DB, vault *crypto.Vault, orgID int64) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(fakeAuthMW(1, orgID))
	NewSecretHandler(gdb, vault).Register(g)
	return r
}

func TestSecretHandler_Create_Get_List_Update_Delete(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	vault := newVaultForTest(t)
	srv := httptest.NewServer(newSecretRouter(t, gdb, vault, orgID))
	defer srv.Close()

	// === Create ===
	createBody, _ := json.Marshal(map[string]any{
		"scope":       "org",
		"scope_id":    orgID,
		"name":        "DB_PASSWORD",
		"type":        "text",
		"description": "primary db",
		"value":       "super-secret-pw",
	})
	resp, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var created map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	require.Equal(t, "DB_PASSWORD", created["name"])
	require.Equal(t, "test-v1", created["kek_id"])
	require.NotContains(t, created, "value", "API 永远不返 value")
	require.NotContains(t, created, "encrypted_value", "也不返密文")
	secretID := int64(created["id"].(float64))

	// DB 落地: encrypted_value 不是明文, kek_id=test-v1
	var raw model.Secret
	require.NoError(t, gdb.First(&raw, secretID).Error)
	require.NotEqual(t, []byte("super-secret-pw"), raw.EncryptedValue, "DB 存的不能是明文")
	require.Equal(t, "test-v1", raw.EncryptionKEKID)
	// 解密 round-trip 通
	plain, err := vault.Decrypt(raw.EncryptedValue)
	require.NoError(t, err)
	require.Equal(t, "super-secret-pw", string(plain))

	// === Get ===
	resp, err = http.Get(srv.URL + "/api/v1/secrets/" + itoa64(secretID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "DB_PASSWORD", got["name"])
	require.NotContains(t, got, "value")

	// === List ===
	resp, err = http.Get(srv.URL + "/api/v1/secrets?scope=org&scope_id=" + itoa64(orgID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var listed map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&listed))
	items := listed["items"].([]any)
	require.GreaterOrEqual(t, len(items), 1)
	for _, item := range items {
		m := item.(map[string]any)
		require.NotContains(t, m, "value", "list 也不能返 value")
	}

	// === Update value (rotation) ===
	updBody, _ := json.Marshal(map[string]any{"value": "rotated-pw"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/secrets/"+itoa64(secretID),
		bytes.NewReader(updBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// DB 密文已变
	var raw2 model.Secret
	require.NoError(t, gdb.First(&raw2, secretID).Error)
	require.NotEqual(t, raw.EncryptedValue, raw2.EncryptedValue, "rotation 后密文应变")
	plain2, err := vault.Decrypt(raw2.EncryptedValue)
	require.NoError(t, err)
	require.Equal(t, "rotated-pw", string(plain2))

	// === Delete ===
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/secrets/"+itoa64(secretID), nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestSecretHandler_NoVault_503(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	// vault = nil
	srv := httptest.NewServer(newSecretRouter(t, gdb, nil, orgID))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]any{
		"scope": "org", "scope_id": orgID,
		"name": "X", "value": "y",
	})
	resp, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestSecretHandler_BadScope_400(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	srv := httptest.NewServer(newSecretRouter(t, gdb, newVaultForTest(t), orgID))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]any{
		"scope": "weird", "scope_id": orgID,
		"name": "X", "value": "y",
	})
	resp, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSecretHandler_OrgScopeMismatch_403(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	srv := httptest.NewServer(newSecretRouter(t, gdb, newVaultForTest(t), orgID))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]any{
		"scope": "org", "scope_id": orgID + 999, // 不是 caller 的 org
		"name": "X", "value": "y",
	})
	resp, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSecretHandler_CloudTypes_Create(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	vault := newVaultForTest(t)
	srv := httptest.NewServer(newSecretRouter(t, gdb, vault, orgID))
	defer srv.Close()

	tkeValue, _ := json.Marshal(map[string]string{
		"secret_id": "AKIDxxx", "secret_key": "SKxxx", "region": "ap-guangzhou",
	})
	createBody, _ := json.Marshal(map[string]any{
		"scope": "org", "scope_id": orgID,
		"name": "tke-creds", "type": "tencent_cloud", "value": string(tkeValue),
	})
	resp, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	ackValue, _ := json.Marshal(map[string]string{
		"access_key_id": "LTAIxxx", "access_key_secret": "SKxxx", "region": "cn-hangzhou",
	})
	createBody2, _ := json.Marshal(map[string]any{
		"scope": "org", "scope_id": orgID,
		"name": "ack-creds", "type": "aliyun_cloud", "value": string(ackValue),
	})
	resp2, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody2))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusCreated, resp2.StatusCode)
}

func TestSecretHandler_CloudType_InvalidJSON_400(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	srv := httptest.NewServer(newSecretRouter(t, gdb, newVaultForTest(t), orgID))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]any{
		"scope": "org", "scope_id": orgID,
		"name": "bad", "type": "tencent_cloud", "value": "not-json",
	})
	resp, err := http.Post(srv.URL+"/api/v1/secrets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSecretHandler_DeleteReferenced_409(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	vault := newVaultForTest(t)
	srv := httptest.NewServer(newSecretRouter(t, gdb, vault, orgID))
	defer srv.Close()

	enc, err := vault.Encrypt([]byte("ssh-key-content"))
	require.NoError(t, err)
	sec := model.Secret{
		Scope: "org", ScopeID: orgID, Name: "ssh-1", Type: "ssh-key",
		EncryptedValue: enc, EncryptionKEKID: vault.PrimaryID(),
	}
	require.NoError(t, gdb.Create(&sec).Error)
	t.Cleanup(func() {
		_ = gdb.Exec("DELETE FROM clusters WHERE credential_id = ?", sec.ID).Error
	})

	credID := sec.ID
	cl := model.Cluster{
		OrgID: orgID, Name: "ref-cluster-" + randSuffix(),
		Provider: "selfhosted", CredentialID: &credID,
		Config: []byte(`{"kubeconfig":"apiVersion: v1"}`),
	}
	require.NoError(t, gdb.Create(&cl).Error)
	t.Cleanup(func() { _ = gdb.Delete(&cl).Error })

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/secrets/"+itoa64(sec.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	refs := body["references"].([]any)
	require.Len(t, refs, 1)
	ref := refs[0].(map[string]any)
	require.Equal(t, "cluster", ref["kind"])
}

func TestResolveSecrets_EngineEntry(t *testing.T) {
	gdb := openRunTestDB(t)
	orgID := newSecretFixture(t, gdb)
	vault := newVaultForTest(t)

	// 落两条 secret
	for _, kv := range []struct{ n, v string }{
		{"API_KEY", "key-123"},
		{"WEBHOOK_URL", "https://hook.x/abc"},
	} {
		enc, err := vault.Encrypt([]byte(kv.v))
		require.NoError(t, err)
		require.NoError(t, gdb.Create(&model.Secret{
			Scope: "org", ScopeID: orgID, Name: kv.n, Type: "text",
			EncryptedValue: enc, EncryptionKEKID: vault.PrimaryID(),
		}).Error)
	}

	got, missing, err := ResolveSecrets(context.Background(), gdb, vault, "org", orgID,
		[]string{"API_KEY", "WEBHOOK_URL", "MISSING_ONE"})
	require.NoError(t, err)
	require.Equal(t, "key-123", got["API_KEY"])
	require.Equal(t, "https://hook.x/abc", got["WEBHOOK_URL"])
	require.Equal(t, []string{"MISSING_ONE"}, missing)
}
