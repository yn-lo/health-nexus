// Go build: !e2e
//
// 本文件仅用于在未显式指定 build tag 时保持 tests/e2e_api 包可编译。
// 真正的 e2e 测试（涉及真实外部 API：真机 HTTP / LLM 调用）由 -tags e2e 编译：
//
//	go test ./tests/...                 # 不编译外部 API 测试（本包为空，仅此占位）
//	go test -tags e2e ./tests/e2e_api/  # 真机 HTTP e2e + 真实 LLM 调用（手动）
//
//go:build !e2e

package e2e_api_test

// no tests: 默认构建（无 e2e tag）下本包仅编译此占位文件，不执行任何外部调用。
