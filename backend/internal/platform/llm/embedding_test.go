package llm

import (
	"math"
	"testing"
)

// TestValidateEmbedding 供应商异常时可能返回 HTTP 200 + 无效向量，
// 必须在出口处拦截：否则检索会得到 0 命中并被当成"知识库没有内容"。
func TestValidateEmbedding(t *testing.T) {
	tests := []struct {
		name    string
		vec     []float32
		wantErr bool
	}{
		{"正常向量_通过", []float32{0.1, -0.2, 0.3}, false},
		{"含单个非零_通过", []float32{0, 0, 0.5}, false},
		{"空向量_报错", nil, true},
		{"零长度切片_报错", []float32{}, true},
		{"全零向量_报错", []float32{0, 0, 0}, true},
		{"含NaN_报错", []float32{0.1, float32(math.NaN())}, true},
		{"含正无穷_报错", []float32{0.1, float32(math.Inf(1))}, true},
		{"含负无穷_报错", []float32{float32(math.Inf(-1))}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmbedding(tt.vec)
			if tt.wantErr && err == nil {
				t.Errorf("期望 error，实际 nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("期望 nil error，实际 %v", err)
			}
		})
	}
}
