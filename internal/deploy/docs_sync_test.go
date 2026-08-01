package deploy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 文档同步门禁。
//
// 文档腐烂不是靠自觉能防住的：这一轮清理挖出的东西——指向已删文件的链接、
// 描述早已不存在的目录结构的 README、把已完成的事写成"待办"的计划文档——
// 全都是当初写下时正确、之后代码变了而文档没跟上。
//
// 靠"记得改文档"防不住这类漂移，只有让它在构建时失败才防得住。

// trackedMarkdown 返回 git 跟踪的 markdown 文档（排除前端目录）。
//
// 只看受跟踪文件：本地草稿、插件市场缓存里的 md 不属于本仓交付物，
// 把它们纳入校验只会制造噪声，最后没人再看这个测试的输出。
func trackedMarkdown(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "*.md")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files 不可用: %v", err)
	}
	var docs []string
	for _, line := range strings.Fields(string(out)) {
		if strings.HasPrefix(line, "web/") {
			continue
		}
		docs = append(docs, line)
	}
	return docs
}

// TestDocLinksResolve 检查受跟踪文档里指向仓库内的路径引用是否真实存在。
//
// 只校验 markdown 链接（形如 [x](path)）中带路径分隔符的那些：
// 裸文件名（`service.go`）在正文里通常是简写指代，不是可点击的路径，
// 强制它们可解析只会逼着作者写一堆冗长路径，反而更难读。
func TestDocLinksResolve(t *testing.T) {
	root := repoRootFromDeploy(t)
	docs := trackedMarkdown(t, root)

	linkRe := regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	var broken []string

	for _, rel := range docs {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for _, m := range linkRe.FindAllStringSubmatch(string(data), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			if target == "" || !strings.Contains(target, "/") {
				continue // 裸文件名视为正文指代，不校验
			}
			abs := filepath.Join(root, filepath.Dir(rel), target)
			if _, err := os.Stat(abs); err == nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, target)); err == nil {
				continue
			}
			broken = append(broken, rel+" → "+target)
		}
	}

	if len(broken) > 0 {
		t.Fatalf("文档里有 %d 条链接指向不存在的路径：\n  %s\n\n"+
			"链接失效说明文档描述的结构已经变了。修链接，或者把那段描述改成现状。",
			len(broken), strings.Join(broken, "\n  "))
	}
}

// TestBaselinePolicyCountMatchesDoc 校验基线策略数量与 README 声称的一致。
//
// 策略集是对外承诺合规覆盖范围的东西。README 说 30 个而实际 12 个，
// 意味着售前材料和交付内容对不上——这种偏差没人会主动发现，
// 直到客户按文档验收。
func TestBaselinePolicyCountMatchesDoc(t *testing.T) {
	root := repoRootFromDeploy(t)
	cfgDir := filepath.Join(root, "plugins", "baseline", "config")

	var policies, rules int
	err := filepath.Walk(cfgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var doc struct {
			Rules []json.RawMessage `json:"rules"`
		}
		if json.Unmarshal(data, &doc) != nil {
			return nil
		}
		policies++
		rules += len(doc.Rules)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历基线策略失败: %v", err)
	}

	readme, err := os.ReadFile(filepath.Join(cfgDir, "README.md"))
	if err != nil {
		t.Fatalf("基线策略 README 缺失: %v", err)
	}

	// README 里写的是「**30 个策略，614 条规则**」这种形式。
	claimRe := regexp.MustCompile(`\*\*(\d+)\s*个策略[，,]\s*(\d+)\s*条规则\*\*`)
	m := claimRe.FindStringSubmatch(string(readme))
	if m == nil {
		t.Fatal("基线策略 README 里找不到「**N 个策略，M 条规则**」的声明，" +
			"无法校验它与实际文件是否一致")
	}
	claimPolicies, _ := strconv.Atoi(m[1])
	claimRules, _ := strconv.Atoi(m[2])

	if claimPolicies != policies || claimRules != rules {
		t.Fatalf("基线策略 README 与实际不符：\n"+
			"  README 声称: %d 个策略 / %d 条规则\n"+
			"  实际文件是: %d 个策略 / %d 条规则\n\n"+
			"策略集是对外承诺的合规覆盖范围，数字对不上意味着交付内容与文档不一致。",
			claimPolicies, claimRules, policies, rules)
	}
}

// TestClaudeMdReferencesExist 校验 CLAUDE.md 里引用的文档确实存在。
//
// CLAUDE.md 是每次会话都会被读的入口文档。它指向一份不存在的文件时，
// 读者（人或模型）会以为那里有权威说明，实际什么都没有。
func TestClaudeMdReferencesExist(t *testing.T) {
	root := repoRootFromDeploy(t)
	data, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Skip("CLAUDE.md 不存在")
	}

	linkRe := regexp.MustCompile(`\[[^\]]*\]\((docs/[^)\s#]+)\)`)
	var missing []string
	for _, m := range linkRe.FindAllStringSubmatch(string(data), -1) {
		if _, err := os.Stat(filepath.Join(root, m[1])); err != nil {
			missing = append(missing, m[1])
		}
	}
	if len(missing) > 0 {
		t.Fatalf("CLAUDE.md 引用了不存在的文档：%s\n\n"+
			"它是每次会话的入口文档，指向空气会让人以为那里有权威说明。",
			strings.Join(missing, ", "))
	}
}
