// util.go — 行读取辅助。
package sshrunner

import (
	"bufio"
	"bytes"
	"io"
)

func readLines(r io.Reader, stream string, cb LineCallback) []byte {
	var buf bytes.Buffer
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		if cb != nil {
			cb(stream, line)
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
