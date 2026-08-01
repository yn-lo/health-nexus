package entity

import "time"

// AIProvider 对应 ai_providers 表（合并版，provider_type 区分 llm/embedding/rerank）。
type AIProvider struct {
	ID              int64
	Name            string
	ProviderType    string
	APIURL          string // DB 列名 api_url；对外 JSON 字段为 api_base（契约 §6.1.1）
	IsFullURL       bool   // DB 列名 is_full_url；true 时后端原样使用 api_url，不自动拼接 /v1
	APIKeyEncrypted []byte // base64(nonce+ciphertext)，存 BYTEA
	APIKeyMasked    string
	ModelName       string
	Dimension       *int
	// Parameters 供应商扩展参数（temperature / top_p / max_tokens / response_format）。
	// NewClientFromProvider 构造时注入 llm.Client，chat completion 请求按 key 注入对应字段。
	Parameters   map[string]any
	DepartmentID *int
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
