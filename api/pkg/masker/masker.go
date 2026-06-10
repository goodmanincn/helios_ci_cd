// Package masker — 日志/字符串中自动屏蔽 secret 值 (M2 T2.5.4)。
//
// 用法:
//   m := masker.New([]string{"abc123", "p@ss"})
//   m.Mask("login=abc123 ok")  → "login=*** ok"
//
// 也可包装 io.Writer:
//   masked := m.WrapWriter(os.Stdout)
//   fmt.Fprintln(masked, "key=abc123")   → stdout: "key=*** "
//
// 实现:
//   - 用 strings.NewReplacer 做单 pass 多值替换 (O(N) 不 O(N*K))
//   - 同时把 base64/URL-encoded 形式也加进替换表 (覆盖最常见的 leak 路径)
//   - 短值 (< 4 chars) 跳过 — 防止把 "true"/"a" 当 secret 误屏 (DoS log)
//   - 大小写区分 (secret 通常区分大小写, 不展开)
//
// 不做:
//   - 模糊匹配 / regex (性能 + 误屏)
//   - hex 化形式 (实际很少出现, 加进去 false-positive 太多)
package masker

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/url"
	"strings"
)

// Mask 占位字符串. 默认 "***".
const Mask = "***"

// MinLen 短于此长度的 secret 不参与替换 (避免误屏)。
const MinLen = 4

// Masker 一个已编译好的多值屏蔽器, 线程安全 (Replacer 是只读的).
type Masker struct {
	r *strings.Replacer
}

// New 用 secrets 列表构造. 重复 / 空 / 过短的值会被跳过。
func New(secrets []string) *Masker {
	pairs := buildPairs(secrets)
	if len(pairs) == 0 {
		return &Masker{r: strings.NewReplacer()} // no-op
	}
	return &Masker{r: strings.NewReplacer(pairs...)}
}

// Mask 替换字符串中的 secret 值.
func (m *Masker) Mask(s string) string {
	if m == nil || m.r == nil {
		return s
	}
	return m.r.Replace(s)
}

// MaskBytes 字节版本 (避免 string→[]byte 拷贝). 实现仍走 string Replacer
// (Go 没原生字节级 Replacer; 一般日志行不超过 KB 量级影响不大)。
func (m *Masker) MaskBytes(b []byte) []byte {
	return []byte(m.Mask(string(b)))
}

// WrapWriter 把任意 io.Writer 包成"先 mask 再写"的 writer.
// 按行缓冲, 避免 Mask 把 "abc" 跨 write 切断造成漏屏.
//
// 注意: caller 应在结束时调 Flush() 把 buffer 残留写出.
func (m *Masker) WrapWriter(w io.Writer) *Writer {
	return &Writer{m: m, w: w}
}

// Writer 行缓冲的 mask writer.
type Writer struct {
	m   *Masker
	w   io.Writer
	buf bytes.Buffer
}

func (mw *Writer) Write(p []byte) (int, error) {
	mw.buf.Write(p)
	// 按 \n 切, 完整的行先 mask 后下推
	for {
		data := mw.buf.Bytes()
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		line := data[:i+1]
		masked := mw.m.MaskBytes(line)
		if _, err := mw.w.Write(masked); err != nil {
			return 0, err
		}
		mw.buf.Next(i + 1)
	}
	return len(p), nil
}

// Flush 把 buffer 残留 (不带 \n 的尾部) 写出 (mask 后).
func (mw *Writer) Flush() error {
	if mw.buf.Len() == 0 {
		return nil
	}
	masked := mw.m.MaskBytes(mw.buf.Bytes())
	mw.buf.Reset()
	_, err := mw.w.Write(masked)
	return err
}

// ---- 编译 pairs ----

// buildPairs 把 secrets 列表展开成 (old, new) 对供 strings.Replacer 用.
//
// 展开规则: 原值 + base64(StdEncoding 去掉末尾 =) + url.QueryEscape 后的形式.
// 短值 / 重复 / 空跳过. 已去重。
func buildPairs(secrets []string) []string {
	seen := make(map[string]bool, len(secrets)*3)
	var pairs []string
	add := func(v string) {
		if v == "" || len(v) < MinLen {
			return
		}
		if seen[v] {
			return
		}
		seen[v] = true
		pairs = append(pairs, v, Mask)
	}
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		add(s)
		// base64 (StdEncoding, padding 去掉避免 "==" 尾巴误屏其它"==")
		b64 := strings.TrimRight(base64.StdEncoding.EncodeToString([]byte(s)), "=")
		add(b64)
		// URL-encoded (仅当不同于原值才有意义)
		ue := url.QueryEscape(s)
		if ue != s {
			add(ue)
		}
	}
	return pairs
}
