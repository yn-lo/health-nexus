// Package arch 提供 ArchUnit 风格的架构约束测试（约束层的第一道防线）。
//
// 这些测试用代码验证代码的架构合规性——将 .harness/specs/architecture/boundaries.md
// 中的分层依赖方向规则转为可执行检查。每条规则对应一个 AC-ARCH-* 编号，
// 与 .golangci.yml 的 lint 规则互补：
//   - 结构性依赖方向（谁能 import 谁）→ 本文件（AST/import 路径检查）
//   - 代码风格（行数/函数长度/错误处理/死代码）→ .golangci.yml
//
// 规则反映代码库的"实际"架构（service 可 import 本域 repository 以共享 filter/error 类型，
// service 可 import platform/crypto 等基础设施），目标是捕获未来的回归而非重写历史。
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot 是 go.mod 中声明的模块路径。
const moduleRoot = "health-nexus"

// internalRoot 是被扫描的源码根目录（相对模块根）。
const internalRoot = "internal"

// layerInfo 描述一个 .go 文件所属的层与域。
type layerInfo struct {
	file    string // 相对模块根的路径（用于错误输出）
	layer   string // handler / service / repository / entity / platform / shared / middleware / adapter / di / config / rag / unknown
	domain  string // 域名（auth/base/chat/config/wiki），非域层为空
	imports []string
}

// classify 按文件路径推断层与域。dir 为相对 internal 的路径（如 domain/auth/handler）。
func classify(dir string) (layer, domain string) {
	parts := strings.Split(filepath.ToSlash(dir), "/")
	if len(parts) == 0 {
		return "unknown", ""
	}
	switch parts[0] {
	case "domain":
		if len(parts) < 3 {
			return "domain-root", parts[1]
		}
		domain = parts[1]
		switch parts[2] {
		case "entity":
			return "entity", domain
		case "repository":
			return "repository", domain
		case "service":
			return "service", domain
		case "handler":
			return "handler", domain
		case "rag":
			return "rag", domain // chat 域的 RAG 组件，按 service 规则约束
		default:
			return "domain-other", domain
		}
	case "platform":
		return "platform", ""
	case "shared":
		return "shared", ""
	case "middleware":
		return "middleware", ""
	case "adapter":
		return "adapter", ""
	case "di":
		return "di", ""
	case "config":
		return "config", ""
	}
	return "unknown", ""
}

// findModuleRoot 从当前工作目录向上查找 go.mod 所在目录。
// go test 会在被测包目录下执行（如 internal/harness/arch/），因此必须先定位模块根。
func findModuleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getcwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("未找到 go.mod（从 %s 向上遍历至卷根）", cwd)
		}
		dir = parent
	}
}

// collectFiles 遍历 internal/ 收集所有非测试 .go 文件的层信息与 import。
func collectFiles(t *testing.T) []layerInfo {
	t.Helper()
	modRoot := findModuleRoot(t)
	root := filepath.Join(modRoot, internalRoot)
	fset := token.NewFileSet()
	var files []layerInfo
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			// 跳过隐藏目录、vendor、testdata
			if name == "" || (name[0] == '.' || name[0] == '_') && name != "." {
				return filepath.SkipDir
			}
			if name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(rel)
		layer, domain := classify(dir)
		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// 解析失败不阻塞（fail-open），仅跳过该文件
			return nil
		}
		var imps []string
		for _, im := range parsed.Imports {
			imps = append(imps, strings.Trim(im.Path.Value, `"`))
		}
		relMod, _ := filepath.Rel(filepath.Dir(root), path)
		files = append(files, layerInfo{
			file:    filepath.ToSlash(relMod),
			layer:   layer,
			domain:  domain,
			imports: imps,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	return files
}

// violation 是一条架构违规记录。
type violation struct {
	rule      string // AC-ARCH-XX
	file      string
	badImport string
	reason    string
	fix       string
}

// internalImport 提取 health-nexus/internal/... 内部 import 的后缀，非内部返回空。
func internalImport(imp string) string {
	prefix := moduleRoot + "/internal/"
	if strings.HasPrefix(imp, prefix) {
		return strings.TrimPrefix(imp, prefix)
	}
	return ""
}

// isDomainImport 判断 import 是否指向 domain/<d>/...（d 为空时匹配任意域）。
func isDomainImport(suffix, d string) bool {
	if !strings.HasPrefix(suffix, "domain/") {
		return false
	}
	if d == "" {
		return true
	}
	rest := strings.TrimPrefix(suffix, "domain/")
	return strings.HasPrefix(rest, d+"/") || rest == d
}

// domainOfImport 返回 import 指向的域名（非域 import 返回空）。
func domainOfImport(suffix string) string {
	if !strings.HasPrefix(suffix, "domain/") {
		return ""
	}
	rest := strings.TrimPrefix(suffix, "domain/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// sublayerOfImport 返回域 import 的子层（entity/repository/service/handler/rag）。
func sublayerOfImport(suffix string) string {
	rest := strings.TrimPrefix(suffix, "domain/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		rest = rest[idx+1:]
		if idx2 := strings.Index(rest, "/"); idx2 >= 0 {
			return rest[:idx2]
		}
		return rest
	}
	return ""
}

// checkRules 对所有文件应用规则集，返回违规列表。
func checkRules(files []layerInfo) []violation {
	var vs []violation
	for _, f := range files {
		for _, imp := range f.imports {
			vs = append(vs, checkOne(f, imp)...)
		}
	}
	return vs
}

// checkOne 对单个 import 按文件所在层检查是否违规。
func checkOne(f layerInfo, imp string) []violation {
	suffix := internalImport(imp)
	if suffix == "" {
		return nil // 非内部 import，不约束
	}
	var vs []violation
	add := func(rule, reason, fix string) {
		vs = append(vs, violation{rule: rule, file: f.file, badImport: imp, reason: reason, fix: fix})
	}

	switch f.layer {
	case "handler":
		// AC-ARCH-01: handler 不得 import repository（任何域）
		if isDomainImport(suffix, "") && sublayerOfImport(suffix) == "repository" {
			add("AC-ARCH-01", "handler 不得直接依赖 repository",
				"在 service 中编排数据访问，handler 仅持有 service 引用；参见 .harness/specs/architecture/boundaries.md")
		}
		// AC-ARCH-02: handler 不得 import platform
		if strings.HasPrefix(suffix, "platform/") {
			add("AC-ARCH-02", "handler 不得直接依赖 platform 基础设施",
				"通过 service 定义的消费者接口反转依赖，由 di 注入；参见 .harness/specs/architecture/boundaries.md")
		}
		// AC-ARCH-13: handler 不得跨域 import（仅可 import 本域）
		if isDomainImport(suffix, "") {
			if d := domainOfImport(suffix); d != "" && d != f.domain {
				add("AC-ARCH-13", "handler 不得跨域 import domain/"+d,
					"跨域协作通过 internal/adapter 桥接；参见 .harness/specs/conventions/di.md")
			}
		}

	case "service", "rag":
		// AC-ARCH-03: service 不得 import net/http（保持 HTTP 无关）
		if imp == "net/http" {
			add("AC-ARCH-03", "service 不得 import net/http",
				"用 shared/errors 的状态码常量替代 net/http.StatusXxx；参见 .harness/specs/conventions/error-handling.md")
		}
		// AC-ARCH-04: service 不得 import handler（任何域）
		if isDomainImport(suffix, "") && sublayerOfImport(suffix) == "handler" {
			add("AC-ARCH-04", "service 不得依赖 handler",
				"依赖方向为 handler → service，禁止反向；参见 .harness/specs/architecture/boundaries.md")
		}
		// AC-ARCH-13: service 不得跨域 import（仅可 import 本域 entity/repository）
		if isDomainImport(suffix, "") {
			if d := domainOfImport(suffix); d != "" && d != f.domain {
				add("AC-ARCH-13", "service 不得跨域 import domain/"+d,
					"跨域协作通过 internal/adapter 桥接；参见 .harness/specs/conventions/di.md")
			}
		}

	case "repository":
		// AC-ARCH-05: repository 不得 import service / handler
		if isDomainImport(suffix, "") {
			sl := sublayerOfImport(suffix)
			if sl == "service" || sl == "handler" {
				add("AC-ARCH-05", "repository 不得依赖 "+sl,
					"依赖方向为 service → repository，禁止反向；参见 .harness/specs/architecture/boundaries.md")
			}
			// AC-ARCH-13: repository 不得跨域 import
			if d := domainOfImport(suffix); d != "" && d != f.domain {
				add("AC-ARCH-13", "repository 不得跨域 import domain/"+d,
					"跨域协作通过 internal/adapter 桥接；参见 .harness/specs/conventions/di.md")
			}
		}
		// AC-ARCH-05b: repository 不得 import middleware
		if strings.HasPrefix(suffix, "middleware") {
			add("AC-ARCH-05", "repository 不得依赖 middleware",
				"middleware 是 HTTP 层关注点，repository 应保持协议无关；参见 .harness/specs/architecture/boundaries.md")
		}
		// AC-ARCH-03: repository 不得 import net/http
		if imp == "net/http" {
			add("AC-ARCH-03", "repository 不得 import net/http",
				"repository 应保持 HTTP 无关；参见 .harness/specs/conventions/error-handling.md")
		}

	case "entity":
		// AC-ARCH-07: entity 不得依赖 repository/service/handler（含本域）
		if isDomainImport(suffix, "") {
			sl := sublayerOfImport(suffix)
			if sl == "repository" || sl == "service" || sl == "handler" {
				add("AC-ARCH-07", "entity 不得依赖 "+sl,
					"entity 是纯数据层，不依赖任何业务层；参见 .harness/specs/architecture/boundaries.md")
			}
			// AC-ARCH-13: entity 不得跨域 import
			if d := domainOfImport(suffix); d != "" && d != f.domain {
				add("AC-ARCH-13", "entity 不得跨域 import domain/"+d,
					"跨域共享通过 internal/shared 原语；参见 .harness/specs/architecture/boundaries.md")
			}
		}
		// AC-ARCH-07: entity 不得依赖 platform / middleware
		if strings.HasPrefix(suffix, "platform/") {
			add("AC-ARCH-07", "entity 不得依赖 platform",
				"entity 是纯数据结构，不依赖基础设施；参见 .harness/specs/architecture/boundaries.md")
		}
		if strings.HasPrefix(suffix, "middleware") {
			add("AC-ARCH-07", "entity 不得依赖 middleware",
				"entity 是纯数据结构，不依赖 HTTP 层；参见 .harness/specs/architecture/boundaries.md")
		}
		// AC-ARCH-03: entity 不得 import net/http
		if imp == "net/http" {
			add("AC-ARCH-03", "entity 不得 import net/http",
				"entity 应保持框架无关；参见 .harness/specs/architecture/boundaries.md")
		}

	case "platform":
		// AC-ARCH-09: platform 不得反向 import domain
		if strings.HasPrefix(suffix, "domain/") {
			add("AC-ARCH-09", "platform 不得反向依赖 domain",
				"platform 是基础设施层，不感知业务；接口断言在 di 层完成；参见 .harness/specs/architecture/boundaries.md")
		}

	case "shared":
		// AC-ARCH-11: shared 是叶子层，不得依赖业务/基础设施层
		if strings.HasPrefix(suffix, "domain/") || strings.HasPrefix(suffix, "platform/") ||
			strings.HasPrefix(suffix, "middleware") || strings.HasPrefix(suffix, "adapter") {
			add("AC-ARCH-11", "shared 叶子层不得依赖 "+suffix,
				"shared 是跨域原语，不应反向依赖业务/基础设施层；参见 .harness/specs/architecture/boundaries.md")
		}

	case "middleware":
		// AC-ARCH-12: middleware 不得 import domain 的 service/repository/handler
		if isDomainImport(suffix, "") {
			sl := sublayerOfImport(suffix)
			if sl == "service" || sl == "repository" || sl == "handler" {
				add("AC-ARCH-12", "middleware 不得依赖 domain/"+sl,
					"中间件应保持协议层职责，不承载业务；参见 .harness/specs/architecture/boundaries.md")
			}
		}

	case "adapter":
		// AC-ARCH-14: adapter 不得 import handler（仅桥接 service/repository/entity）
		if isDomainImport(suffix, "") && sublayerOfImport(suffix) == "handler" {
			add("AC-ARCH-14", "adapter 不得依赖 handler",
				"adapter 仅桥接数据/业务层，不接触协议层；参见 .harness/specs/conventions/di.md")
		}
	}
	return vs
}

// reportViolations 按 harness.md "约束输出只保留错误" 原则输出违规。
// 全部通过时输出一行确认。
func reportViolations(t *testing.T, vs []violation) {
	t.Helper()
	if len(vs) == 0 {
		t.Logf("All architecture checks passed")
		return
	}
	for _, v := range vs {
		t.Errorf("\n  ✗ %s: %s\n    import: %s\n    FIX: %s\n    See: .harness/specs/architecture/boundaries.md",
			v.file, v.reason, v.badImport, v.fix)
	}
}

// TestArch_HandlerMustNotDependOnRepository AC-ARCH-01: handler 层不得直接 import 任何域的 repository 包。
func TestArch_HandlerMustNotDependOnRepository(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		if f.layer != "handler" {
			continue
		}
		for _, imp := range f.imports {
			suffix := internalImport(imp)
			if suffix != "" && isDomainImport(suffix, "") && sublayerOfImport(suffix) == "repository" {
				vs = append(vs, violation{file: f.file, badImport: imp,
					reason: "handler 不得直接依赖 repository",
					fix:    "在 service 中编排数据访问，handler 仅持有 service 引用"})
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_HandlerMustNotDependOnPlatform AC-ARCH-02: handler 层不得直接 import platform 基础设施。
func TestArch_HandlerMustNotDependOnPlatform(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		if f.layer != "handler" {
			continue
		}
		for _, imp := range f.imports {
			suffix := internalImport(imp)
			if strings.HasPrefix(suffix, "platform/") {
				vs = append(vs, violation{file: f.file, badImport: imp,
					reason: "handler 不得直接依赖 platform",
					fix:    "通过 service 消费者接口反转依赖，由 di 注入"})
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_ServiceMustNotImportNetHTTP AC-ARCH-03: service/repository/entity 层不得 import net/http。
func TestArch_ServiceMustNotImportNetHTTP(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		if f.layer != "service" && f.layer != "rag" && f.layer != "repository" && f.layer != "entity" {
			continue
		}
		for _, imp := range f.imports {
			if imp == "net/http" {
				vs = append(vs, violation{file: f.file, badImport: imp,
					reason: f.layer + " 不得 import net/http",
					fix:    "用 shared/errors 状态码常量替代 net/http.StatusXxx"})
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_NoReverseDependencies AC-ARCH-04/05/07: 禁止反向依赖
// （service→handler, repository→service/handler, entity→repository/service/handler/platform）。
func TestArch_NoReverseDependencies(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		for _, imp := range f.imports {
			suffix := internalImport(imp)
			if suffix == "" || !isDomainImport(suffix, "") {
				continue
			}
			sl := sublayerOfImport(suffix)
			switch f.layer {
			case "service", "rag":
				if sl == "handler" {
					vs = append(vs, violation{file: f.file, badImport: imp,
						reason: "service 不得依赖 handler（反向依赖）", fix: "依赖方向 handler → service 单向"})
				}
			case "repository":
				if sl == "service" || sl == "handler" {
					vs = append(vs, violation{file: f.file, badImport: imp,
						reason: "repository 不得依赖 " + sl + "（反向依赖）", fix: "依赖方向 service → repository 单向"})
				}
			case "entity":
				if sl == "repository" || sl == "service" || sl == "handler" {
					vs = append(vs, violation{file: f.file, badImport: imp,
						reason: "entity 不得依赖 " + sl, fix: "entity 是纯数据层"})
				}
			}
		}
		// entity 不得依赖 platform/middleware
		if f.layer == "entity" {
			for _, imp := range f.imports {
				suffix := internalImport(imp)
				if strings.HasPrefix(suffix, "platform/") || strings.HasPrefix(suffix, "middleware") {
					vs = append(vs, violation{file: f.file, badImport: imp,
						reason: "entity 不得依赖基础设施层", fix: "entity 应保持框架无关"})
				}
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_PlatformMustNotImportDomain AC-ARCH-09: platform 不得反向 import domain。
func TestArch_PlatformMustNotImportDomain(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		if f.layer != "platform" {
			continue
		}
		for _, imp := range f.imports {
			suffix := internalImport(imp)
			if strings.HasPrefix(suffix, "domain/") {
				vs = append(vs, violation{file: f.file, badImport: imp,
					reason: "platform 不得反向依赖 domain（AC-ARCH-09）",
					fix:    "在 di 层完成接口断言；platform 不感知业务"})
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_NoCrossDomainImports AC-ARCH-13: 域内各层不得直接 import 另一个域
// （跨域协作必须经 internal/adapter 桥接）。
func TestArch_NoCrossDomainImports(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		if f.domain == "" {
			continue
		}
		for _, imp := range f.imports {
			suffix := internalImport(imp)
			if !isDomainImport(suffix, "") {
				continue
			}
			if d := domainOfImport(suffix); d != "" && d != f.domain {
				vs = append(vs, violation{file: f.file, badImport: imp,
					reason: f.layer + " 不得跨域 import domain/" + d,
					fix:    "跨域协作通过 internal/adapter 桥接；参见 .harness/specs/conventions/di.md"})
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_SharedIsLeafLayer AC-ARCH-11: shared 是叶子层，不得依赖业务/基础设施层。
func TestArch_SharedIsLeafLayer(t *testing.T) {
	files := collectFiles(t)
	var vs []violation
	for _, f := range files {
		if f.layer != "shared" {
			continue
		}
		for _, imp := range f.imports {
			suffix := internalImport(imp)
			if strings.HasPrefix(suffix, "domain/") || strings.HasPrefix(suffix, "platform/") ||
				strings.HasPrefix(suffix, "middleware") || strings.HasPrefix(suffix, "adapter") {
				vs = append(vs, violation{file: f.file, badImport: imp,
					reason: "shared 叶子层不得依赖 " + suffix,
					fix:    "shared 是跨域原语，不应反向依赖"})
			}
		}
	}
	reportViolations(t, vs)
}

// TestArch_FullScan 运行完整规则集（兜底，确保无遗漏）。
func TestArch_FullScan(t *testing.T) {
	files := collectFiles(t)
	if len(files) == 0 {
		t.Fatal("未收集到任何源文件，扫描可能配置错误")
	}
	vs := checkRules(files)
	reportViolations(t, vs)
}
