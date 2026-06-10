// masker_test.go — Mask + WrapWriter + base64/URL-encode 覆盖.
package masker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMask_Basic(t *testing.T) {
	m := New([]string{"abc123", "p@ssword"})
	require.Equal(t, "login=*** ok", m.Mask("login=abc123 ok"))
	require.Equal(t, "pw=*** retry=*** ", m.Mask("pw=p@ssword retry=p@ssword "))
}

func TestMask_SkipShortValues(t *testing.T) {
	// "x" 太短 → 不进替换表, 避免日志被打成 "***"
	m := New([]string{"x", "ok", "longenough"})
	// 短值 (x / ok) 透传, 长值 (longenough) 被屏
	require.Equal(t, "x is ok ***", m.Mask("x is ok longenough"))
}

func TestMask_Base64Form(t *testing.T) {
	// secret 在日志里通过 base64 出现也应屏蔽 (Authorization: Basic ... 场景)
	m := New([]string{"my-token-xyz"})
	// base64 of "my-token-xyz" 不带尾 = → "bXktdG9rZW4teHl6"
	require.Equal(t, "Authorization: Basic ***",
		m.Mask("Authorization: Basic bXktdG9rZW4teHl6"))
}

func TestMask_URLEncodedForm(t *testing.T) {
	m := New([]string{"p@ss/word"})
	// url.QueryEscape("p@ss/word") → "p%40ss%2Fword"
	require.Equal(t, "u=foo&pw=***",
		m.Mask("u=foo&pw=p%40ss%2Fword"))
}

func TestMask_Empty(t *testing.T) {
	m := New(nil)
	require.Equal(t, "anything", m.Mask("anything"))

	m = New([]string{"", "x"})
	require.Equal(t, "anything goes", m.Mask("anything goes"))
}

func TestMask_Idempotent(t *testing.T) {
	m := New([]string{"secret123"})
	once := m.Mask("v=secret123")
	twice := m.Mask(once)
	require.Equal(t, once, twice, "替换后再过一遍不应变化")
}

func TestWriter_LineBuffered(t *testing.T) {
	var buf bytes.Buffer
	m := New([]string{"hidden-token"})
	w := m.WrapWriter(&buf)

	// 分多次写: 跨 Write 边界的 secret 也要屏蔽
	_, err := w.Write([]byte("user=alice pw="))
	require.NoError(t, err)
	_, err = w.Write([]byte("hidden-token\nnext\n"))
	require.NoError(t, err)

	require.Equal(t, "user=alice pw=***\nnext\n", buf.String())
}

func TestWriter_Flush(t *testing.T) {
	var buf bytes.Buffer
	m := New([]string{"sekrit"})
	w := m.WrapWriter(&buf)

	_, _ = w.Write([]byte("trailing=sekrit"))
	require.Empty(t, buf.String(), "无 \\n 前不会主动 flush")

	require.NoError(t, w.Flush())
	require.Equal(t, "trailing=***", buf.String())
}

func TestMaskBytes(t *testing.T) {
	m := New([]string{"abcd1234"})
	got := m.MaskBytes([]byte("k=abcd1234"))
	require.Equal(t, "k=***", string(got))
}

func TestMask_PartialMatch(t *testing.T) {
	// 子串也屏蔽 (与原 spec "部分匹配也屏蔽" 一致)
	m := New([]string{"longsecret"})
	require.Equal(t, "prefix-***-suffix",
		m.Mask("prefix-longsecret-suffix"))
	// 但短于 MinLen 不参与
	require.Equal(t, "abc", m.Mask("abc"))
	_ = strings.Repeat // touch import
}
