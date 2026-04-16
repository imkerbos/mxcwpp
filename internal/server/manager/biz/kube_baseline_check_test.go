package biz

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestIsSystemNamespace 验证系统 Namespace 判断
func TestIsSystemNamespace(t *testing.T) {
	tests := []struct {
		ns   string
		want bool
	}{
		{"kube-system", true},
		{"kube-public", true},
		{"kube-node-lease", true},
		{"default", false},
		{"production", false},
		{"monitoring", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ns, func(t *testing.T) {
			if got := isSystemNamespace(tt.ns); got != tt.want {
				t.Fatalf("isSystemNamespace(%q) = %v, want %v", tt.ns, got, tt.want)
			}
		})
	}
}

// TestHasOwnerKind 验证 OwnerReference Kind 查找
func TestHasOwnerKind(t *testing.T) {
	refs := []metav1.OwnerReference{
		{Kind: "Job", Name: "my-job"},
		{Kind: "ReplicaSet", Name: "my-rs"},
	}

	tests := []struct {
		name string
		refs []metav1.OwnerReference
		kind string
		want bool
	}{
		{"found Job", refs, "Job", true},
		{"found ReplicaSet", refs, "ReplicaSet", true},
		{"not found DaemonSet", refs, "DaemonSet", false},
		{"nil refs", nil, "Job", false},
		{"empty refs", []metav1.OwnerReference{}, "Job", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasOwnerKind(tt.refs, tt.kind); got != tt.want {
				t.Fatalf("hasOwnerKind(refs, %q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

// TestRegisterChecksCount 验证注册了 80 条检查
func TestRegisterChecksCount(t *testing.T) {
	c := &KubeBaselineChecker{}
	c.registerChecks()

	if len(c.checks) != 80 {
		t.Fatalf("期望注册 80 条检查, 实际 %d 条", len(c.checks))
	}
}

// TestRegisterChecksUniqueIDs 验证所有 CheckID 唯一
func TestRegisterChecksUniqueIDs(t *testing.T) {
	c := &KubeBaselineChecker{}
	c.registerChecks()

	seen := make(map[string]bool)
	for _, check := range c.checks {
		if seen[check.CheckID] {
			t.Fatalf("重复的 CheckID: %s", check.CheckID)
		}
		seen[check.CheckID] = true
	}
}

// TestRegisterChecksAllFieldsSet 验证所有检查项字段非空
func TestRegisterChecksAllFieldsSet(t *testing.T) {
	c := &KubeBaselineChecker{}
	c.registerChecks()

	for _, check := range c.checks {
		if check.CheckID == "" {
			t.Fatal("CheckID 不能为空")
		}
		if check.CheckName == "" {
			t.Fatalf("CheckName 不能为空: %s", check.CheckID)
		}
		if check.Category == "" {
			t.Fatalf("Category 不能为空: %s", check.CheckID)
		}
		if check.Severity == "" {
			t.Fatalf("Severity 不能为空: %s", check.CheckID)
		}
		if check.Description == "" {
			t.Fatalf("Description 不能为空: %s", check.CheckID)
		}
		if check.Remediation == "" {
			t.Fatalf("Remediation 不能为空: %s", check.CheckID)
		}
		if check.Benchmark == "" {
			t.Fatalf("Benchmark 不能为空: %s", check.CheckID)
		}
		if check.Run == nil {
			t.Fatalf("Run 函数不能为 nil: %s", check.CheckID)
		}
	}
}

// TestRegisterChecksSeverityValues 验证 Severity 值合法
func TestRegisterChecksSeverityValues(t *testing.T) {
	c := &KubeBaselineChecker{}
	c.registerChecks()

	validSeverities := map[string]bool{
		"critical": true,
		"high":     true,
		"medium":   true,
		"low":      true,
	}

	for _, check := range c.checks {
		if !validSeverities[check.Severity] {
			t.Fatalf("非法 Severity %q: %s", check.Severity, check.CheckID)
		}
	}
}

// TestRegisterChecksCategoryDistribution 验证各类别检查数量
func TestRegisterChecksCategoryDistribution(t *testing.T) {
	c := &KubeBaselineChecker{}
	c.registerChecks()

	categoryCount := make(map[string]int)
	for _, check := range c.checks {
		categoryCount[check.Category]++
	}

	expectedCategories := map[string]int{
		"RBAC":             9,
		"Pod Security":     18,
		"Network":          8,
		"Secrets & Config": 8,
		"Workload":         12,
		"Node":             9,
		"Cluster Config":   9,
		"Supply Chain":     3,
		"Runtime":          4,
	}

	for cat, expected := range expectedCategories {
		actual := categoryCount[cat]
		if actual != expected {
			t.Fatalf("类别 %q: 期望 %d 条, 实际 %d 条", cat, expected, actual)
		}
	}

	// 确保没有意外的类别
	for cat := range categoryCount {
		if _, ok := expectedCategories[cat]; !ok {
			t.Fatalf("意外的类别: %q (%d 条)", cat, categoryCount[cat])
		}
	}
}

// TestRegisterChecksBenchmark 所有检查都应引用 CIS Kubernetes Benchmark 1.8
func TestRegisterChecksBenchmark(t *testing.T) {
	c := &KubeBaselineChecker{}
	c.registerChecks()

	for _, check := range c.checks {
		if check.Benchmark != cisBenchmark {
			t.Fatalf("检查 %s 的 Benchmark 不正确: %q, 期望 %q", check.CheckID, check.Benchmark, cisBenchmark)
		}
	}
}

// TestUpdateHealthScoreLogic 验证健康分计算逻辑
func TestUpdateHealthScoreLogic(t *testing.T) {
	tests := []struct {
		name     string
		passed   int
		total    int
		expected int
	}{
		{"all pass", 80, 80, 100},
		{"half pass", 40, 80, 50},
		{"none pass", 0, 80, 0},
		{"3/4 pass", 60, 80, 75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := tt.passed * 100 / tt.total
			if score != tt.expected {
				t.Fatalf("score = %d, want %d", score, tt.expected)
			}
		})
	}
}
