// Package config 加载应用配置（env + yaml），结构化 Config struct。
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// 配置默认值（供 setDefaults 注册到 viper）。
const (
	defaultServerPort        = 5230
	defaultReadTimeout       = 15 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultPostgresMaxConns  = 25
	defaultPostgresMinConns  = 5
	defaultJWTAccessTTL      = 30 * time.Minute
	defaultJWTRefreshTTL     = 7 * 24 * time.Hour
	defaultLLMTimeout        = 60 * time.Second
	defaultArgon2Memory      = 64 * 1024
	defaultArgon2Iterations  = 3
	defaultArgon2Parallelism = 2
	defaultArgon2SaltLength  = 16
	defaultArgon2KeyLength   = 32

	// 限流默认值（次/分钟），运行时可通过 Redis rl_cfg:{scope} 热更新覆盖
	defaultAuthLoginRateLimit      = 10
	defaultAuthRegisterRateLimit   = 10
	defaultAuthRefreshRateLimit    = 10
	defaultChatStreamRateLimit     = 20
	defaultChatStreamAnonRateLimit = 5
)

// Config 应用配置根结构。
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Postgres  PostgresConfig  `mapstructure:"postgres"`
	Redis     RedisConfig     `mapstructure:"redis"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	LLM       LLMConfig       `mapstructure:"llm"`
	CORS      CORSConfig      `mapstructure:"cors"`
	Argon2    Argon2Config    `mapstructure:"argon2"`
	Security  SecurityConfig  `mapstructure:"security"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
}

// ServerConfig HTTP 服务器配置。
// WriteTimeout 未暴露为配置——SSE 流式响应需禁用 server 级写超时（R7-4），
// main.go 中硬编码 WriteTimeout=0；非流式端点由各 handler 的 ctx deadline 兜底（如 auth 10s）。
//
// TrustedProxies 信任的反向代理 CIDR 列表（D-MED-02 修复）。
// 仅当 RemoteAddr 命中此列表时才解析 X-Forwarded-For；空列表表示不信任任何代理，
// 直接用 RemoteAddr 作为客户端 IP（XFF 头被忽略，杜绝伪造绕过 IP 限流）。
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	TrustedProxies  []string      `mapstructure:"trusted_proxies"`
}

// PostgresConfig PostgreSQL 连接配置。
type PostgresConfig struct {
	DSN             string        `mapstructure:"dsn"`
	MaxConns        int32         `mapstructure:"max_conns"`
	MinConns        int32         `mapstructure:"min_conns"`
	MaxConnLifetime time.Duration `mapstructure:"max_conn_lifetime"`
}

// RedisConfig Redis 连接配置。
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// JWTConfig JWT 配置（HS256 对称签名）。
type JWTConfig struct {
	Secret     string        `mapstructure:"secret"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
	Issuer     string        `mapstructure:"issuer"`
}

// LLMConfig LLM 客户端配置。
// 主字段（BaseURL/APIKey/ChatModel/RewriteModel/EmbeddingModel/Timeout）作为 chat 默认 provider；
// Embedding/Rerank/Rewrite 子配置可选，零值字段回退到主配置（向后兼容）。
type LLMConfig struct {
	BaseURL        string        `mapstructure:"base_url"`
	APIKey         string        `mapstructure:"api_key"`
	ChatModel      string        `mapstructure:"chat_model"`
	RewriteModel   string        `mapstructure:"rewrite_model"`
	EmbeddingModel string        `mapstructure:"embedding_model"`
	Timeout        time.Duration `mapstructure:"timeout"`
	// 可选：分离的 provider 配置。零值字段回退到主配置。
	Embedding ProviderConfig `mapstructure:"embedding"`
	Rerank    ProviderConfig `mapstructure:"rerank"`
	Rewrite   ProviderConfig `mapstructure:"rewrite"`
}

// ProviderConfig 单个 LLM provider 配置（embedding/rerank/rewrite）。
// 零值字段表示继承主 LLMConfig 对应字段（BaseURL/APIKey/Timeout 通用，
// Model 按能力回退：Embedding→EmbeddingModel，Rewrite→RewriteModel，Rerank 无主字段回退）。
type ProviderConfig struct {
	BaseURL string        `mapstructure:"base_url"`
	APIKey  string        `mapstructure:"api_key"`
	Model   string        `mapstructure:"model"`
	Timeout time.Duration `mapstructure:"timeout"`
}

// CORSConfig 跨域配置。
// AllowCredentials 控制是否发送 Access-Control-Allow-Credentials: true。
// 安全约束：AllowedOrigins 含 "*" 时禁止 AllowCredentials=true（CORS 规范禁止，
// 且会让任意站点携带 cookie 跨站请求）；CORS 中间件构造阶段会 panic 拦截。
type CORSConfig struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
}

// Argon2Config 密码哈希参数（OWASP 2023 推荐）。
type Argon2Config struct {
	Memory      uint32 `mapstructure:"memory"`      // 64MB
	Iterations  uint32 `mapstructure:"iterations"`  // 3
	Parallelism uint8  `mapstructure:"parallelism"` // 2
	SaltLength  uint32 `mapstructure:"salt_length"` // 16
	KeyLength   uint32 `mapstructure:"key_length"`  // 32
}

// SecurityConfig 安全配置。
// EncryptionKey 用于 AES-GCM 字段级加密（API Key 等），必须显式配置，无默认值。
// 环境变量：HEALTH_NEXUS_SECURITY_ENCRYPTION_KEY
type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"`
}

// RateLimitConfig 限流默认值（启动时从 config.yaml 读取，运行时可通过 Redis 热更新覆盖）。
// 零值字段使用 setDefaults 的硬编码默认值。
type RateLimitConfig struct {
	AuthLogin      int `mapstructure:"auth_login"`
	AuthRegister   int `mapstructure:"auth_register"`
	AuthRefresh    int `mapstructure:"auth_refresh"`
	ChatStream     int `mapstructure:"chat_stream"`
	ChatStreamAnon int `mapstructure:"chat_stream_anon"`
}

// Load 加载配置：先读 yaml，再用环境变量覆盖。
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/health-nexus")

	// 环境变量覆盖（HEALTH_NEXUS_SERVER_PORT 等）。
	// SetEnvKeyReplacer 将嵌套 key 的 "." 替换为 "_"，使 security.encryption_key
	// 能被 HEALTH_NEXUS_SECURITY_ENCRYPTION_KEY 覆盖；缺少此行会导致所有嵌套配置
	// 的环境变量覆盖静默失效。
	v.SetEnvPrefix("health_nexus")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// 配置文件缺失时仅用环境变量+默认值
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", defaultServerPort)
	v.SetDefault("server.read_timeout", defaultReadTimeout)
	v.SetDefault("server.shutdown_timeout", defaultShutdownTimeout)

	v.SetDefault("postgres.max_conns", defaultPostgresMaxConns)
	v.SetDefault("postgres.min_conns", defaultPostgresMinConns)
	v.SetDefault("postgres.max_conn_lifetime", time.Hour)

	v.SetDefault("redis.db", 0)

	v.SetDefault("jwt.secret", "dev-jwt-secret-change-in-production")
	v.SetDefault("jwt.access_ttl", defaultJWTAccessTTL)
	v.SetDefault("jwt.refresh_ttl", defaultJWTRefreshTTL)
	v.SetDefault("jwt.issuer", "health-nexus")

	v.SetDefault("llm.timeout", defaultLLMTimeout)

	// OWASP 2023 推荐的 argon2id 参数
	v.SetDefault("argon2.memory", defaultArgon2Memory)
	v.SetDefault("argon2.iterations", defaultArgon2Iterations)
	v.SetDefault("argon2.parallelism", defaultArgon2Parallelism)
	v.SetDefault("argon2.salt_length", defaultArgon2SaltLength)
	v.SetDefault("argon2.key_length", defaultArgon2KeyLength)

	// 限流默认值
	v.SetDefault("rate_limit.auth_login", defaultAuthLoginRateLimit)
	v.SetDefault("rate_limit.auth_register", defaultAuthRegisterRateLimit)
	v.SetDefault("rate_limit.auth_refresh", defaultAuthRefreshRateLimit)
	v.SetDefault("rate_limit.chat_stream", defaultChatStreamRateLimit)
	v.SetDefault("rate_limit.chat_stream_anon", defaultChatStreamAnonRateLimit)
}
