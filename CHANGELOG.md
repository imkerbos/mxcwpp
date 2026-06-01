# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [2.6.0] - 2026-06-01

漏洞数据精度全链路升级，VM 实测准确率 30%→98%，RHSA 覆盖 34→14962。

- 新增 NEVRA(epoch:version-release) 端到端 RPM 精确匹配
- 新增 dpkg 版本比较器（Debian/Ubuntu）
- 新增信创 OS 源 stub：openEuler / Anolis / Kylin / UOS
- 新增 host_vuln FP 统一清理（migration + sync 末尾双触发）
- RHSA 同步器去 50 条上限，并发 8 + skip-known
- matcher 双 gate 互斥（OS / ecosystem）+ NEVRA 三元组比对
- OSV 仅查语言包生态，NVD 不再生成 host_vuln
- OSS-Fuzz crash ID 不再当 CVE 入库

## [Unreleased]

### Added
- Security baseline: 9 checkers, 212 rules covering CIS Benchmark core items
- Asset center: 11 asset types collection (processes, ports, users, packages, containers, etc.)
- Vulnerability management: PURL collection + OSV.dev matching + CVSS v3.1 scoring + SBOM export
- Antivirus: ClamAV + YARA-X dual-engine scanning with quarantine
- File integrity monitoring: AIDE-based FIM with policy, event, and task management
- Runtime detection: Tetragon/eBPF event collection + CEL rule engine + MITRE ATT&CK mapping
- Container security: K8s cluster management, container CIS baseline (80 rules)
- Alert center: aggregation, whitelisting, auto-response, tracing timeline
- Threat intelligence: MISP IOC import + Redis cache + CEL real-time matching
- Embedded detection rules with builtin tagging system
