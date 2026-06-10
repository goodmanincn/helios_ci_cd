// Package crypto — Helios 密钥加密层 (M2 T2.5.1)。
//
// 模型: 信封加密 (envelope encryption)
//   - KEK (master key, key-encryption key): 一段 32 字节固定密钥, 来自 env / KMS
//   - DEK (data encryption key): 每条 secret 一次性生成 32 字节随机, 用 KEK 加密后跟密文一起存
//   - Payload 格式:  version(1B) | kek_id_len(1B) | kek_id | nonce_dek(12B) | encrypted_dek(48B)
//                  | nonce_value(12B) | ciphertext
//   - 同样输入两次加密结果不同 (DEK 和两个 nonce 都是 crypto/rand 新生成)
//
// 安全:
//   - AES-256-GCM (auth tag 16B, nonce 12B)
//   - KEK 永远不在磁盘 / DB 出现, 只在 process memory
//   - rotation 通过更换 KEK 实现; payload 里 kek_id 让多 KEK 并存 (rotation 期间)
//
// 不做的事 (留 future):
//   - 真 KMS 集成 (AWS KMS / Tencent KMS), 留 wrapper 接口
//   - HSM / 硬件密封
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	// AES-256 → 32 字节 key
	kekLen   = 32
	dekLen   = 32
	nonceLen = 12 // GCM 推荐
	envVer   = 1  // payload 版本, 后续算法升级用
)

// KEK 主密钥. 通常来自 env (HELIOS_KEK_BASE64) 或 KMS.
//
// ID 用来支持多 KEK 并存 (rotation 期间, 不同 secret 可能由不同 KEK 加密)。
type KEK struct {
	ID  string // 任意标识 (e.g. "v1", "kms:arn:...")
	Key []byte // 必须 32 字节
}

// NewKEK 校验并构造.
func NewKEK(id string, key []byte) (*KEK, error) {
	if len(key) != kekLen {
		return nil, fmt.Errorf("KEK key must be %d bytes, got %d", kekLen, len(key))
	}
	if id == "" {
		return nil, errors.New("KEK id is required")
	}
	return &KEK{ID: id, Key: key}, nil
}

// Vault 加密/解密入口. 可注册多个 KEK (rotation 期间所有都要在), 默认用 primary 加密.
type Vault struct {
	keks    map[string]*KEK
	primary string // 默认加密用的 KEK id
}

// NewVault 用 primary KEK 构造. 之后可 AddKEK 加备选.
func NewVault(primary *KEK) (*Vault, error) {
	if primary == nil {
		return nil, errors.New("primary KEK is required")
	}
	return &Vault{
		keks:    map[string]*KEK{primary.ID: primary},
		primary: primary.ID,
	}, nil
}

// AddKEK 添加备选 KEK (decrypt 用). 不动 primary.
func (v *Vault) AddKEK(k *KEK) {
	if k == nil {
		return
	}
	v.keks[k.ID] = k
}

// PrimaryID 当前默认加密用的 KEK id.
func (v *Vault) PrimaryID() string { return v.primary }

// Encrypt 加密明文, 返完整 envelope payload (用 primary KEK).
//
// 同 plaintext 两次调用返回不同 payload (DEK + nonce 都 random).
func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
	kek := v.keks[v.primary]
	if kek == nil {
		return nil, errors.New("vault: primary KEK missing")
	}

	// 1) 生成 DEK
	dek := make([]byte, dekLen)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("random dek: %w", err)
	}

	// 2) 用 KEK 加密 DEK
	kekGCM, err := newGCM(kek.Key)
	if err != nil {
		return nil, err
	}
	nonceDEK := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonceDEK); err != nil {
		return nil, fmt.Errorf("random nonce-dek: %w", err)
	}
	encDEK := kekGCM.Seal(nil, nonceDEK, dek, nil) // 32 + 16(tag) = 48

	// 3) 用 DEK 加密 plaintext
	dekGCM, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonceVal := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonceVal); err != nil {
		return nil, fmt.Errorf("random nonce-val: %w", err)
	}
	ciphertext := dekGCM.Seal(nil, nonceVal, plaintext, nil)

	// 4) 拼 envelope
	if len(kek.ID) > 255 {
		return nil, errors.New("KEK id too long (>255)")
	}
	out := make([]byte, 0, 2+len(kek.ID)+nonceLen+len(encDEK)+nonceLen+len(ciphertext))
	out = append(out, byte(envVer))
	out = append(out, byte(len(kek.ID)))
	out = append(out, []byte(kek.ID)...)
	out = append(out, nonceDEK...)
	out = append(out, encDEK...)
	out = append(out, nonceVal...)
	out = append(out, ciphertext...)
	return out, nil
}

// Decrypt 反操作. 自动按 envelope 内 kek_id 找对应 KEK.
func (v *Vault) Decrypt(payload []byte) ([]byte, error) {
	if len(payload) < 2 {
		return nil, errors.New("payload too short")
	}
	if payload[0] != envVer {
		return nil, fmt.Errorf("unsupported envelope version %d", payload[0])
	}
	idLen := int(payload[1])
	pos := 2
	if len(payload) < pos+idLen+nonceLen+dekLen+16+nonceLen {
		return nil, errors.New("payload truncated")
	}
	kekID := string(payload[pos : pos+idLen])
	pos += idLen

	kek := v.keks[kekID]
	if kek == nil {
		return nil, fmt.Errorf("unknown KEK id %q (registered: %v)", kekID, keysOf(v.keks))
	}

	nonceDEK := payload[pos : pos+nonceLen]
	pos += nonceLen
	// encDEK = dekLen + 16 (GCM tag)
	encDEK := payload[pos : pos+dekLen+16]
	pos += dekLen + 16
	nonceVal := payload[pos : pos+nonceLen]
	pos += nonceLen
	ciphertext := payload[pos:]

	// 解 DEK
	kekGCM, err := newGCM(kek.Key)
	if err != nil {
		return nil, err
	}
	dek, err := kekGCM.Open(nil, nonceDEK, encDEK, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt DEK: %w", err)
	}
	defer zero(dek)

	// 解明文
	dekGCM, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	plain, err := dekGCM.Open(nil, nonceVal, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt value: %w", err)
	}
	return plain, nil
}

// EnvelopeKEKID 解 envelope 拿 kek_id (不解密, 用于审计/排错).
func EnvelopeKEKID(payload []byte) (string, error) {
	if len(payload) < 2 {
		return "", errors.New("payload too short")
	}
	if payload[0] != envVer {
		return "", fmt.Errorf("unsupported envelope version %d", payload[0])
	}
	idLen := int(payload[1])
	if len(payload) < 2+idLen {
		return "", errors.New("truncated id")
	}
	return string(payload[2 : 2+idLen]), nil
}

// ---- helpers ----

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func keysOf(m map[string]*KEK) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// zero 抹掉 secret bytes (best-effort, GC 之后还在 heap, 但解密栈帧出去之前抹一遍降露面)
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
