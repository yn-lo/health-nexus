// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"

	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// rateKeyPrefix 限流 key 前缀。
const rateKeyPrefix = "rate"

// RateLimiter 基于 redis_rate 的固定窗口限流器。
//
// trustedProxies 为信任的反向代理 CIDR 列表（D-MED-02 修复）。
// 仅当 r.RemoteAddr 的 IP 命中此列表时才解析 X-Forwarded-For；
// 空列表表示不信任任何代理，直接用 RemoteAddr 作为客户端 IP（XFF 头被忽略）。
type RateLimiter struct {
	limiter        *redis_rate.Limiter
	rdb            *redis.Client // 用于读取热更新限流配置（rl_cfg:{scope}）
	trustedProxies []*net.IPNet
}

// NewRateLimiter 构造限流器。rdb 由调用方注入并管理生命周期。
// trustedProxiesCIDR 为可信代理 CIDR 字符串列表（如 "127.0.0.1/8"）；
// 单 IP（无 /前缀）自动补全为 /32（IPv4）或 /128（IPv6）。
// 非法条目静默跳过——di 层应在启动期校验，此处降级保证可用性。
func NewRateLimiter(rdb *redis.Client, trustedProxiesCIDR []string) *RateLimiter {
	return &RateLimiter{
		limiter:        redis_rate.NewLimiter(rdb),
		rdb:            rdb,
		trustedProxies: parseTrustedProxies(trustedProxiesCIDR),
	}
}

// parseTrustedProxies 解析 CIDR 列表为 []*net.IPNet；非法条目静默跳过。
func parseTrustedProxies(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// 单 IP（无 /前缀）补全为 /32 或 /128，方便配置 "127.0.0.1" 等回环地址。
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil {
				if ip.To4() != nil {
					c += "/32"
				} else {
					c += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Middleware 按 scope 限流，key 含 user_id（已认证）或 IP（匿名），超限返回 429。
// limit 为周期内允许的请求数，period 为窗口时长；Burst 与 limit 一致以简化语义。
func (rl *RateLimiter) Middleware(scope string, limit int, period time.Duration) func(http.Handler) http.Handler {
	rateLimit := redis_rate.Limit{Rate: limit, Period: period, Burst: limit}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := buildRateKey(r, scope, rl.trustedProxies)
			res, err := rl.limiter.Allow(r.Context(), key, rateLimit)
			if err != nil {
				response.WriteError(w, r, apperrors.ServiceUnavailable("RATE_LIMITER_UNAVAILABLE", "限流服务暂不可用，请稍后重试"))
				return
			}
			if res.Allowed == 0 {
				retryAfter := int(res.RetryAfter.Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				response.WriteError(w, r, apperrors.RateLimited("RATE_LIMITED", "请求过于频繁，请稍后重试"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateCfgPrefix 热更新限流配置的 Redis key 前缀。
const rateCfgPrefix = "rl_cfg:"

// HotReloadMiddleware 从 Redis 实时读取限流阈值（热更新），fallback 到 defaultLimit。
// Redis key 格式：rl_cfg:{scope}（值为整数）。读取失败或值无效时降级使用 defaultLimit。
// 修改 Redis 值后即时生效（下一次请求），无需重启服务。
func (rl *RateLimiter) HotReloadMiddleware(
	scope string, defaultLimit int, period time.Duration,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := rl.resolveLimit(r.Context(), scope, defaultLimit)
			rateLimit := redis_rate.Limit{Rate: limit, Period: period, Burst: limit}
			key := buildRateKey(r, scope, rl.trustedProxies)
			res, err := rl.limiter.Allow(r.Context(), key, rateLimit)
			if err != nil {
				response.WriteError(w, r, apperrors.ServiceUnavailable("RATE_LIMITER_UNAVAILABLE", "限流服务暂不可用，请稍后重试"))
				return
			}
			if res.Allowed == 0 {
				retryAfter := int(res.RetryAfter.Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				response.WriteError(w, r, apperrors.RateLimited("RATE_LIMITED", "请求过于频繁，请稍后重试"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveLimit 从 Redis 读取实时限流配置，fallback 到 defaultLimit。
func (rl *RateLimiter) resolveLimit(ctx context.Context, scope string, defaultLimit int) int {
	val, err := rl.rdb.Get(ctx, rateCfgPrefix+scope).Int()
	if err != nil || val <= 0 {
		return defaultLimit
	}
	return val
}

// globalScopePrefix 全局共享限流桶前缀：scope 以此开头时所有请求共享同一个桶
// （不按 user/device/IP 区分），用于匿名端点的总量保护——
// 防止攻击者批量伪造 device_id 绕过单设备限流。
const globalScopePrefix = "global:"

// buildRateKey 构造限流 key：global: 前缀 scope 返回全局共享桶；
// 否则已认证用 user_id，匿名用 device_id，兜底用客户端 IP。
func buildRateKey(r *http.Request, scope string, trustedProxies []*net.IPNet) string {
	if strings.HasPrefix(scope, globalScopePrefix) {
		return rateKeyPrefix + ":" + scope
	}
	if uid, ok := r.Context().Value(contextkeys.UserID).(int64); ok && uid > 0 {
		return fmt.Sprintf("%s:%s:%d", rateKeyPrefix, scope, uid)
	}
	if did, ok := r.Context().Value(contextkeys.DeviceID).(string); ok && did != "" {
		return fmt.Sprintf("%s:%s:%s", rateKeyPrefix, scope, did)
	}
	return fmt.Sprintf("%s:%s:%s", rateKeyPrefix, scope, clientIP(r, trustedProxies))
}

// clientIP 提取客户端 IP，基于可信代理链校验（D-MED-02 修复）。
//
// 算法：
//  1. 取 r.RemoteAddr 的 IP（去端口）。
//  2. 若该 IP 不在 trustedProxies 内 → 直接返回（不可信来源的 XFF 完全被忽略，杜绝伪造绕过）。
//  3. 若该 IP 在 trustedProxies 内 → 解析 X-Forwarded-For，从右向左跳过可信 IP，返回第一个不可信 IP。
//  4. 无 XFF 或全部 IP 可信 → 返回 RemoteAddr IP。
//
// ponytail: 仅校验 RemoteAddr 一层可信代理 + XFF 链右起跳过可信段，折中；
// 上限——若攻击者能从可信代理网段内发起请求，仍可伪造 XFF 中段的不可信 IP。
// 升级路径：完整代理链反查（每跳校验 TCP 源地址）或采用 Forwarded 标准头 + 签名。
// 不可信来源的 XFF 完全被忽略，已杜绝 D-MED-02 的"伪造首段绕过 IP 限流"攻击。
func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteIP := remoteIPFromAddr(r.RemoteAddr)
	if !ipInTrusted(remoteIP, trustedProxies) {
		return remoteIP
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteIP
	}
	parts := strings.Split(xff, ",")
	// 从右向左跳过可信代理 IP，返回第一个不可信 IP（标准做法）。
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !ipInTrusted(ip, trustedProxies) {
			return ip
		}
	}
	// 所有 IP 都可信（链路全在可信网段，罕见）→ 返回最左侧非空 IP。
	for _, p := range parts {
		if ip := strings.TrimSpace(p); ip != "" {
			return ip
		}
	}
	return remoteIP
}

// remoteIPFromAddr 从 host:port 提取 IP（兼容 IPv6 [::1]:8080 格式）。
func remoteIPFromAddr(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// ClientIP 提取客户端 IP（导出版本，不解析 X-Forwarded-For）。
//
// 仅供非安全敏感场景使用（如 wiki 阅读量防刷 D-LOW-08）；安全敏感场景（限流）请走
// RateLimiter.Middleware，其基于 trustedProxies 严格校验 XFF 链，杜绝伪造绕过。
//
// ponytail: 直接取 RemoteAddr IP，部署在反代后会按代理 IP 聚合，折中——对 view_count
// 防刷上限可接受（最坏情况是同代理下多用户共享一个去重桶，阅读量略偏低）；
// 升级路径：注入 trustedProxies 配置并复用 clientIP(r, trustedProxies)。
func ClientIP(r *http.Request) string {
	return remoteIPFromAddr(r.RemoteAddr)
}

// ipInTrusted 判断 IP 是否在任一可信 CIDR 内。解析失败返回 false（保守：视为不可信）。
func ipInTrusted(ipStr string, trusted []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
