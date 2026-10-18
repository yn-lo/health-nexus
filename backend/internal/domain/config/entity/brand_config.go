package entity

import "time"

// BrandConfig 对应 brand_configs 表（单例，id 固定为 1）。
// 供前端展示品牌标识：Logo、品牌名、字标、单位名称、系统标语、官网、站点标题、favicon。
// OrgName / Slogan / SiteURL / LogoURL / FaviconURL 为空表示不覆盖/不展示。
type BrandConfig struct {
	ID         int64
	Name       string
	Wordmark   string
	OrgName    string
	Slogan     string
	SiteURL    string
	LogoURL    string
	SiteTitle  string
	FaviconURL string
	UpdatedAt  time.Time
}

// 品牌配置字段长度上限（按 rune 计，信任边界校验用；与 SQL VARCHAR 对齐）。
const (
	BrandNameMaxLen      = 50
	BrandWordmarkMaxLen  = 50
	BrandOrgNameMaxLen   = 100
	BrandSloganMaxLen    = 50
	BrandSiteTitleMaxLen = 100
	BrandURLMaxLen       = 500
)

// DefaultBrandConfig 是品牌配置缺失时的默认值（与 SQL DEFAULT 对齐）。
var DefaultBrandConfig = BrandConfig{
	ID:         1,
	Name:       "Health Nexus",
	Wordmark:   "HEALTH NEXUS",
	OrgName:    "",
	Slogan:     "",
	SiteURL:    "",
	LogoURL:    "",
	SiteTitle:  "健康宣教平台",
	FaviconURL: "",
}
