package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// deployScriptPath 返回真实 deploy.sh 的绝对路径，不存在则跳过。
func deployScriptPath(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 不可用，跳过")
	}
	abs, err := filepath.Abs(filepath.Join("..", "..", "deploy", "deploy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("deploy.sh 不存在: %v", err)
	}
	return abs
}

// ensureTwiceBody 抽取 deploy.sh 真实的 is_strong_secret/persist/ensure 三函数并 eval，
// 执行两次（第二次模拟全新 shell 从 .env 重载），断言：达标、不轮换、唯一一行、权限 0600、
// EXPECT 非空时值被保留。不打印任何密钥值，失败仅输出非敏感原因。
const ensureTwiceBody = `set -euo pipefail
eval "$(sed -n '/^is_strong_secret() {/,/^}/p; /^persist_internal_secret() {/,/^}/p; /^ensure_internal_secret() {/,/^}/p' "$SCRIPT")"
log_warn(){ :; }; log_info(){ :; }
INTERNAL_SECRET=""
set +u; . "$ENV_FILE" || true; set -u
ensure_internal_secret
v1="${INTERNAL_SECRET:-}"
unset INTERNAL_SECRET
. "$ENV_FILE"
ensure_internal_secret
v2="${INTERNAL_SECRET:-}"
is_strong_secret "$v1" || { echo "FAIL:not-strong"; exit 1; }
[ "$v1" = "$v2" ] || { echo "FAIL:rotated"; exit 1; }
n="$(grep -c '^INTERNAL_SECRET=' "$ENV_FILE" || true)"
[ "$n" = "1" ] || { echo "FAIL:lines=$n"; exit 1; }
perm="$(stat -f '%Lp' "$ENV_FILE" 2>/dev/null || stat -c '%a' "$ENV_FILE")"
[ "$perm" = "600" ] || { echo "FAIL:perm=$perm"; exit 1; }
if [ -n "${EXPECT:-}" ]; then [ "$v1" = "$EXPECT" ] || { echo "FAIL:not-preserved"; exit 1; }; fi
echo "OK"`

// TestDeployEnsureInternalSecret 覆盖 .env 无键/空键/弱值/过短/重复键/强值多场景，
// 每种执行两次，断言 ensure_internal_secret 幂等（不轮换、唯一一行、0600），
// 强值被保留、其它行不被破坏。
func TestDeployEnsureInternalSecret(t *testing.T) {
	script := deployScriptPath(t)
	const strong = "0123456789abcdef0123456789abcdef" // 32 hex

	cases := []struct {
		name       string
		initial    string
		expect     string // 非空 → 断言 secret 被保留为该值
		otherLines bool
	}{
		{"no-key", "", "", false},
		{"empty-key", "INTERNAL_SECRET=\n", "", false},
		{"weak-value", "INTERNAL_SECRET=changeme\n", "", false},
		{"short-value", "INTERNAL_SECRET=abc\n", "", false},
		{"duplicate-weak", "INTERNAL_SECRET=\nINTERNAL_SECRET=alsoweak\n", "", false},
		{"strong-preserved", "INTERNAL_SECRET=" + strong + "\n", strong, false},
		{"strong-with-other-lines", "FOO=bar\nINTERNAL_SECRET=" + strong + "\nBAZ=qux\n", strong, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			envFile := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(envFile, []byte(c.initial), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", ensureTwiceBody)
			cmd.Env = append(os.Environ(), "ENV_FILE="+envFile, "SCRIPT="+script)
			if c.expect != "" {
				cmd.Env = append(cmd.Env, "EXPECT="+c.expect)
			}
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "OK") {
				t.Fatalf("非幂等/校验失败: %v\n%s", err, out)
			}
			if c.otherLines {
				data, _ := os.ReadFile(envFile)
				if !strings.Contains(string(data), "FOO=bar") || !strings.Contains(string(data), "BAZ=qux") {
					t.Errorf("persist 破坏了非 INTERNAL_SECRET 行")
				}
			}
		})
	}
}
