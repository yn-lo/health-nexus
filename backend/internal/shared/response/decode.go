package response

import (
	"encoding/json"
	"net/http"
)

// maxJSONBodyBytes JSON 请求体统一上限 1MB，防大报文耗尽内存。
const maxJSONBodyBytes = 1 << 20

// DecodeStrictJSON 严格解析 JSON 请求体：限制 1MB、拒绝未知字段。
// 返回原始错误（含 io.EOF），由各域按自身的错误码契约映射。
func DecodeStrictJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
