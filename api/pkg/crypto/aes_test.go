// aes_test.go — Vault 加密/解密 + KEK rotation + envelope 解析.
package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, kekLen)
	_, err := rand.Read(k)
	require.NoError(t, err)
	return k
}

func TestVault_EncryptDecrypt_Roundtrip(t *testing.T) {
	kek, err := NewKEK("v1", newKey(t))
	require.NoError(t, err)
	v, err := NewVault(kek)
	require.NoError(t, err)

	plain := []byte("super-secret-token-abc123")
	enc, err := v.Encrypt(plain)
	require.NoError(t, err)
	require.NotEqual(t, plain, enc, "密文必须和明文不同")

	got, err := v.Decrypt(enc)
	require.NoError(t, err)
	require.Equal(t, plain, got)
}

func TestVault_SameInputDifferentCiphertext(t *testing.T) {
	kek, _ := NewKEK("v1", newKey(t))
	v, _ := NewVault(kek)
	plain := []byte("repeated")
	a, err := v.Encrypt(plain)
	require.NoError(t, err)
	b, err := v.Encrypt(plain)
	require.NoError(t, err)
	require.False(t, bytes.Equal(a, b), "DEK+nonce 都 random, 同输入两次密文应不同")
}

func TestVault_KEKRotation(t *testing.T) {
	// 用 v1 加密 → 切到 v2 (新 primary) → 再用 v2 加密;
	// vault 同时持有 v1 + v2, 旧密文仍解得开.
	v1, _ := NewKEK("v1", newKey(t))
	v2, _ := NewKEK("v2", newKey(t))

	vault1, _ := NewVault(v1)
	old, err := vault1.Encrypt([]byte("old-value"))
	require.NoError(t, err)

	vault2, _ := NewVault(v2)
	vault2.AddKEK(v1) // 保留 v1 用于解旧密文
	newCt, err := vault2.Encrypt([]byte("new-value"))
	require.NoError(t, err)

	// vault2 解 v1 密文 ok (含 v1 KEK)
	got1, err := vault2.Decrypt(old)
	require.NoError(t, err)
	require.Equal(t, "old-value", string(got1))
	// vault2 解自己加密的也 ok
	got2, err := vault2.Decrypt(newCt)
	require.NoError(t, err)
	require.Equal(t, "new-value", string(got2))

	// envelope 中 kek_id 应反映加密时用的 KEK
	id, err := EnvelopeKEKID(old)
	require.NoError(t, err)
	require.Equal(t, "v1", id)
	id, err = EnvelopeKEKID(newCt)
	require.NoError(t, err)
	require.Equal(t, "v2", id)
}

func TestVault_DecryptUnknownKEK(t *testing.T) {
	kek1, _ := NewKEK("v1", newKey(t))
	v1, _ := NewVault(kek1)
	ct, err := v1.Encrypt([]byte("x"))
	require.NoError(t, err)

	// 新 vault 只有 v2, 无法解 v1 密文
	kek2, _ := NewKEK("v2", newKey(t))
	v2, _ := NewVault(kek2)
	_, err = v2.Decrypt(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown KEK")
}

func TestVault_TamperedCiphertext_Fails(t *testing.T) {
	kek, _ := NewKEK("v1", newKey(t))
	v, _ := NewVault(kek)
	ct, _ := v.Encrypt([]byte("intact"))
	// 翻转最后一字节
	ct[len(ct)-1] ^= 0xff
	_, err := v.Decrypt(ct)
	require.Error(t, err, "GCM 认证应失败")
}

func TestNewKEK_Validation(t *testing.T) {
	_, err := NewKEK("v1", []byte("too-short"))
	require.Error(t, err)
	_, err = NewKEK("", newKey(t))
	require.Error(t, err)
}

func TestEnvelopeKEKID_Truncated(t *testing.T) {
	_, err := EnvelopeKEKID([]byte{1})
	require.Error(t, err)
	_, err = EnvelopeKEKID([]byte{99, 1})
	require.Error(t, err, "version 不支持")
}
