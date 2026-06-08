# L5 健壮性测试 (2026-06-08)

| 场景 | 操作 | 验证 | 结果 |
|---|---|---|---|
| Agent kill -9 自起 | sudo kill -9 $(pgrep mxsec-agent) | 25s 后新 PID=12086 (≠ 11906), 心跳 2026-06-08 14:56:09 | PASS |
| JWT 过期拒绝 | 签 30s 前 exp 的 token, 访问 /hosts | code=401 | PASS |
| 错签 JWT 拒绝 | HMAC 用 wrong-secret-xx 签名 | code=401 | PASS |
| AC 健康检查 | GET :6752/health | OK | PASS |
| AC 重启 Agent 重连 | docker restart mxsec-agentcenter-dev, 等 60s | hb 更新 + status=online | PASS |

**汇总: PASS=5 / FAIL=0 / PARTIAL=0 (总 5)**
