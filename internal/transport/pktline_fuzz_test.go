package transport

import (
	"bytes"
	"testing"
)

// FuzzPktLineReader 对 Git pkt-line 解码器进行模糊测试，验证面对任意非可信输入时不发生 panic 或越界
func FuzzPktLineReader(f *testing.F) {
	// 1. 注册核心种子语料
	f.Add([]byte("0000"))                                 // flush-pkt
	f.Add([]byte("0001"))                                 // delim-pkt
	f.Add([]byte("0002"))                                 // response-end-pkt
	f.Add([]byte("0004"))                                 // empty line
	f.Add([]byte("000ahello\n"))                          // standard line
	f.Add([]byte("000bfoobar\n0000"))                     // multiple pkts
	f.Add([]byte("0009# service=git-upload-pack\n0000")) // protocol header
	f.Add([]byte("ffff"))                                 // length without payload
	f.Add([]byte("0003"))                                 // invalid length (< 4)
	f.Add([]byte("zzzz"))                                 // non-hex length
	f.Add([]byte(""))                                     // empty input

	// 2. Fuzz 目标
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		for {
			payload, pktType, err := ReadPacket(r)
			if err != nil {
				// 任何畸形输入应安全返回 error，绝对不能 panic
				break
			}
			_ = payload
			_ = pktType
		}
	})
}
