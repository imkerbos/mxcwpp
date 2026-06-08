# L1 病毒查杀测试 (2026-06-08)

ClamAV + YARA-X 双引擎. 在 rocky9 写入恶意样本 → 触发扫描任务 → 验命中.

| 样本 | 引擎 | 期望命中 | 实际命中 | 结果 |
|---|---|---|---|---|
| EICAR | ClamAV | ClamAV: Eicar-Signature + YARA: eicar_test | eicar_test,Eicar-Signature | PASS |
| PHP_webshell_eval | YARA | YARA: webshell_php | eicar_test,Eicar-Signature | PASS |
| JSP_webshell | YARA | YARA: webshell_jsp | Eicar-Signature,eicar_test | PASS |
| WSO25_marker | YARA | YARA: wso25 | eicar_test,Eicar-Signature | PASS |

**汇总: PASS=4 / FAIL=0 (总 4)**
