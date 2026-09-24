package mask

import (
	"reflect"
	"testing"
)

// TestSanitizePII 覆盖出站脱敏的三类行为：
//  1. 能挡住的标识（手机号 / 身份证 / 邮箱 / 长数字串）；
//  2. 不误伤的医学数值（这是"按长度兜底"必须自证的安全边界）；
//  3. 边界输入（空串 / 无 PII / 混合）。
func TestSanitizePII(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// ── 手机号：兼容常见变体写法 ──────────────────────────────
		{name: "手机号-纯数字", in: "我的电话是13812345678", want: "我的电话是[手机号]"},
		{name: "手机号-空格分隔", in: "我的电话是138 1234 5678", want: "我的电话是[手机号]"},
		{name: "手机号-横线分隔", in: "我的电话是138-1234-5678", want: "我的电话是[手机号]"},

		// ── 身份证 ────────────────────────────────────────────
		{name: "身份证-纯数字", in: "身份证110101199001011234", want: "身份证[身份证]"},
		{name: "身份证-校验位X", in: "身份证11010119900101123X", want: "身份证[身份证]"},

		// ── 邮箱 ──────────────────────────────────────────────
		{name: "邮箱", in: "我的邮箱 zhangsan@example.com 谢谢", want: "我的邮箱 [邮箱] 谢谢"},

		// ── 长数字兜底：院内编号格式各院自定，只能按长度兜 ──────
		{name: "银行卡19位", in: "卡号6222021234567890123", want: "卡号[数字]"},
		{name: "住院号12位", in: "住院号202609230012", want: "住院号[数字]"},
		{name: "检验单条码11位", in: "样本号 20260923001", want: "样本号 [数字]"},

		// ── 不误伤医学数值 ────────────────────────────────────
		{name: "血压与心率", in: "血压 180/110，心率 72", want: "血压 180/110，心率 72"},
		{name: "血糖", in: "空腹血糖 7.8 mmol/L", want: "空腹血糖 7.8 mmol/L"},
		{name: "剂量频次", in: "每天 3 次，每次 2 片", want: "每天 3 次，每次 2 片"},
		{name: "日期时间", in: "我 2026-09-23 10:30 开始疼", want: "我 2026-09-23 10:30 开始疼"},
		{name: "十位数字不触发", in: "编号 1234567890", want: "编号 1234567890"},

		// ── 边界 ──────────────────────────────────────────────
		{name: "空字符串", in: "", want: ""},
		{name: "无PII", in: "高血压患者应该低盐饮食", want: "高血压患者应该低盐饮食"},
		{name: "多个标识混合", in: "我叫张三，电话13812345678，邮箱a@b.com",
			want: "我叫张三，电话[手机号]，邮箱[邮箱]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizePII(tc.in); got != tc.want {
				t.Errorf("SanitizePII(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizePIIAll 批量脱敏：逐条处理、不修改入参、空输入原样返回。
func TestSanitizePIIAll(t *testing.T) {
	in := []string{"我的电话13812345678", "血压 180/110"}
	want := []string{"我的电话[手机号]", "血压 180/110"}

	got := SanitizePIIAll(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SanitizePIIAll(%q) = %q，期望 %q", in, got, want)
	}
	if in[0] != "我的电话13812345678" {
		t.Errorf("SanitizePIIAll 不应修改入参，实际 %q", in[0])
	}
	if got := SanitizePIIAll(nil); got != nil {
		t.Errorf("SanitizePIIAll(nil) 应返回 nil，实际 %v", got)
	}
}
