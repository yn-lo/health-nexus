package middleware

import (
	"testing"

	"health-nexus/internal/config"
)

// TestCORS_PanicsOnWildcardWithCredentials 验证 fail-fast：
// AllowedOrigins 含 "*" 且 AllowCredentials=true 时必须 panic，避免不安全配置上线。
func TestCORS_PanicsOnWildcardWithCredentials(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for AllowedOrigins=['*'] + AllowCredentials=true")
		}
	}()
	_ = CORS(config.CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true})
}

// TestCORS_NoPanicOnWildcardWithoutCredentials 验证 api_contract_test.go 的用法不触发 panic：
// AllowedOrigins=['*'] 且 AllowCredentials 缺省（false）时正常构造。
func TestCORS_NoPanicOnWildcardWithoutCredentials(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	_ = CORS(config.CORSConfig{AllowedOrigins: []string{"*"}})
}

// TestCORS_NoPanicOnSpecificOriginsWithCredentials 验证生产配置（具体 Origin + 凭证）正常构造。
func TestCORS_NoPanicOnSpecificOriginsWithCredentials(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	_ = CORS(config.CORSConfig{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowCredentials: true,
	})
}
