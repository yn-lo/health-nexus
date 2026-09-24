// Package mask 提供 PII 脱敏工具（AC-SEC-04）。
package mask

import (
	"regexp"
	"strings"
)

const (
	apiKeyMinLen = 8
)

// APIKey 密钥脱敏：sk-****xxxx
func APIKey(s string) string {
	if len(s) < apiKeyMinLen {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + "****" + s[len(s)-4:]
}

// ── 出站 PII 脱敏 ────────────────────────────────────────────────────────────
//
// 用途：把发往外部模型服务（LLM / embedding / rerank）的文本中的身份标识替换为占位符，
// 实现数据最小化。覆盖范围与已知边界见 SanitizePII 注释。

// PII 占位符。保留"原本属于哪类标识"的语义，帮助模型理解上下文
// （"我的电话是[手机号]"比"我的电话是[数字]"更易理解）。
const (
	piiPlaceholderEmail  = "[邮箱]"
	piiPlaceholderIDCard = "[身份证]"
	piiPlaceholderPhone  = "[手机号]"
	piiPlaceholderNumber = "[数字]"
)

const (
	// piiMobileLen 大陆手机号位数。
	piiMobileLen = 11
	// piiIDCardLen 大陆身份证位数。
	piiIDCardLen = 18
)

// piiEmailRe 邮箱地址。
var piiEmailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// piiNumberRunRe 长数字串：≥11 位数字，允许内部分隔符（空格/横线），兼容
// "138 1234 5678"、"138-1234-5678" 等变体写法；末尾可选身份证校验位 X/x。
//
// 阈值取 11：手机号恰好 11 位——取 10 会多误伤固话号等 10 位数，取 12 则会漏掉手机号本身。
// 正则写法为 `\d` + `(?:[-\s]?\d){10,}`，即 1 + 至少 10 = 至少 11 位数字。
//
// 采用"一个正则匹配整串 + 事后判定类型"，而非"多条精确正则依次替换"：
// 后者会把 19 位银行卡切成"18 位身份证 + 1 位数字"残片。
var piiNumberRunRe = regexp.MustCompile(`\d(?:[-\s]?\d){10,}[\dXx]?`)

// SanitizePII 出站 PII 脱敏：把文本中可识别的身份标识替换为占位符。
//
// 能挡住：
//   - 邮箱、手机号、身份证；
//   - 所有 ≥11 位数字串——银行卡，以及就诊卡号 / 住院号 / 检验单条码号等院内编号
//     （这类格式各院自定，无法用精确正则覆盖，故按长度兜底）。
//
// 挡不住（务必知晓，避免虚假安全感）：
//   - 自然语言中的姓名与地址（如"张三，56 岁，住 XX 路 3 栋"）；
//   - 刻意规避格式的写法（"一三八…"）。
//
// 另外本函数只覆盖"出站"这一跳——同样的信息在数据库中仍然存在，不在其职责范围。
//
// 不误伤医学数值：血压 / 血糖 / 心率 / 剂量均为 2-4 位，达不到 11 位阈值。
//
// 取舍：宁可误伤（替换掉一个长数字）也不漏放（身份证号出境后不可撤回）。
// 11 位以上的数字几乎不可能是"必须让模型看到的医学数值"，误伤损失接近零。
//
// ponytail: 纯正则实现——零依赖、零延迟、结果可预测；不引入 NLP 实体识别
// （成本高、延迟大，且中文姓名识别准确率不足以支撑合规要求）。
// 升级路径：需要更高覆盖率时，在出站前接入专用 PII 识别服务；
// 或从产品层收敛（输入框提示不填写身份信息——不采集即不泄露）。
func SanitizePII(s string) string {
	if s == "" {
		return s
	}
	s = piiEmailRe.ReplaceAllString(s, piiPlaceholderEmail)
	return piiNumberRunRe.ReplaceAllStringFunc(s, piiPlaceholderFor)
}

// SanitizePIIAll 对一组文本逐条脱敏，返回新切片（不修改入参）。
// 用于 embedding 等批量出站场景。
func SanitizePIIAll(texts []string) []string {
	if len(texts) == 0 {
		return texts
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = SanitizePII(t)
	}
	return out
}

// piiPlaceholderFor 按数字串的位数与格式判定占位符类型。
func piiPlaceholderFor(run string) string {
	digits := onlyDigits(run)
	if isMobileNumber(digits) {
		return piiPlaceholderPhone
	}
	// 身份证：18 位纯数字，或 17 位数字 + 校验位 X/x。
	if len(digits) == piiIDCardLen || (len(digits) == piiIDCardLen-1 && hasCheckLetterX(run)) {
		return piiPlaceholderIDCard
	}
	return piiPlaceholderNumber
}

// hasCheckLetterX 判定数字串是否以身份证校验位 X/x 结尾。
func hasCheckLetterX(s string) bool {
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	return last == 'X' || last == 'x'
}

// onlyDigits 去掉分隔符，仅保留数字字符。
func onlyDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isMobileNumber 判定是否为大陆手机号：11 位，1 开头，第 2 位 3-9。
func isMobileNumber(d string) bool {
	return len(d) == piiMobileLen && d[0] == '1' && d[1] >= '3' && d[1] <= '9'
}
