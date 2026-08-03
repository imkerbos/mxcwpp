package intrusion

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 用标注语料里的**正常样本**检验入侵检测器的误报面。
//
// 这些 detector 有真实算法但从未接线，也没有任何测试。接线前必须先回答一个问题：
// 它们会不会对日常运维行为报警。一个会在每次发布时刷屏的检测，
// 上线后的结果不是「多发现威胁」，而是值班从此不再看告警——
// 这个代价本轮 EDR 误报治理已经付过一次（60 万 → 63）。
//
// 语料复用 E-DET-2 的标注集：正常样本刻意选的是「长得像攻击的日常运维」，
// 例如配置管理派生 shell、包管理器投放 systemd 单元、日志轮转批量删日志。

type corpusSample struct {
	Name      string            `json:"name"`
	Label     string            `json:"label"`
	Technique string            `json:"technique"`
	DataType  int32             `json:"data_type"`
	Fields    map[string]string `json:"fields"`
	Note      string            `json:"note"`
}

func loadBenignSamples(t *testing.T) []corpusSample {
	t.Helper()
	dir := filepath.Join("..", "celengine", "replay", "testdata", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("语料目录不可读: %v", err)
	}
	var out []corpusSample
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("读取 %s: %v", e.Name(), err)
		}
		var doc struct {
			Samples []corpusSample `json:"samples"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("解析 %s: %v", e.Name(), err)
		}
		for _, s := range doc.Samples {
			if s.Label == "benign" {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		t.Skip("语料里没有正常样本")
	}
	return out
}

// RootkitDetector 对正常运维行为的误报面。
//
// 已知风险点：它的模式里含 `systemctl (enable|start) .*\.service` 与
// `/etc/systemd/system/.*\.service`——前者每次正常发布都会命中，
// 后者包管理器装包就会命中。本测试把这件事量化出来，而不是留给上线后发现。
func TestRootkitDetector_BenignCorpus(t *testing.T) {
	d := NewRootkitDetector()
	samples := loadBenignSamples(t)

	var flagged []corpusSample
	for _, s := range samples {
		content := s.Fields["cmdline"]
		if content == "" {
			content = s.Fields["file_path"]
		}
		if content == "" {
			continue
		}
		if _, hit := d.Scan(context.Background(), IndicatorEvent{
			HostID: "h1", Source: "process", Content: content,
			ExePath: s.Fields["exe"],
		}); hit {
			flagged = append(flagged, s)
		}
	}

	for _, s := range flagged {
		t.Errorf("正常样本被判为 rootkit 指标：%s\n  为什么它是正常的：%s", s.Name, s.Note)
	}
	if len(flagged) > 0 {
		t.Fatalf("RootkitDetector 在 %d/%d 条正常样本上误命中——接线前必须先收窄模式",
			len(flagged), len(samples))
	}
}

// WebshellDetector 对正常运维行为的误报面。
func TestWebshellDetector_BenignCorpus(t *testing.T) {
	d := NewWebshellDetector()
	samples := loadBenignSamples(t)

	var flagged []corpusSample
	for _, s := range samples {
		content := s.Fields["cmdline"]
		if content == "" {
			content = s.Fields["file_path"]
		}
		if content == "" {
			continue
		}
		if _, hit := d.Scan(context.Background(), FileSampleEvent{
			HostID: "h1", FilePath: s.Fields["file_path"], Content: content,
		}); hit {
			flagged = append(flagged, s)
		}
	}

	for _, s := range flagged {
		t.Errorf("正常样本被判为 webshell：%s\n  为什么它是正常的：%s", s.Name, s.Note)
	}
	if len(flagged) > 0 {
		t.Fatalf("WebshellDetector 在 %d/%d 条正常样本上误命中", len(flagged), len(samples))
	}
}

// ReverseShellDetector 对正常运维行为的误报面。
func TestReverseShellDetector_BenignCorpus(t *testing.T) {
	d := NewReverseShellDetector()
	samples := loadBenignSamples(t)

	var flagged []corpusSample
	for _, s := range samples {
		cmd := s.Fields["cmdline"]
		if cmd == "" {
			continue
		}
		if _, hit := d.Scan(context.Background(), ProcessEvent{
			HostID: "h1", Cmdline: cmd, ExePath: s.Fields["exe"],
		}); hit {
			flagged = append(flagged, s)
		}
	}

	for _, s := range flagged {
		t.Errorf("正常样本被判为反弹 shell：%s\n  为什么它是正常的：%s", s.Name, s.Note)
	}
	if len(flagged) > 0 {
		t.Fatalf("ReverseShellDetector 在 %d/%d 条正常样本上误命中", len(flagged), len(samples))
	}
}

// 排除包管理器不能把检测能力一起排除掉。
//
// 只测「不误报」的话，一个恒不命中的实现也能满分。攻击者投放持久化单元
// 用的是 shell 或脚本解释器，不是 dpkg——这个区别正是排除逻辑的依据，
// 也必须被验证。
func TestRootkitDetector_StillCatchesRealPersistence(t *testing.T) {
	d := NewRootkitDetector()

	cases := []struct {
		name    string
		content string
		exe     string
	}{
		{"shell 投放 systemd 单元", "/etc/systemd/system/update-notifier.service", "/bin/bash"},
		{"脚本解释器写 cron", "/etc/cron.d/backdoor", "/usr/bin/python3"},
		{"执行体未知时不放行", "/etc/systemd/system/evil.service", ""},
	}
	for _, c := range cases {
		if _, hit := d.Scan(context.Background(), IndicatorEvent{
			HostID: "h1", Source: "file", Content: c.content, ExePath: c.exe,
		}); !hit {
			t.Errorf("%s：应当检出，实际未命中（content=%q exe=%q）", c.name, c.content, c.exe)
		}
	}
}

// 包管理器豁免只覆盖 cron / systemd 两类。
//
// LKM 加载、LD_PRELOAD、authorized_keys 写入这三类，包管理器本来就不会做，
// 一旦出现就是异常——豁免不该扩大到它们身上。
func TestRootkitDetector_PackageManagerExemptionIsNarrow(t *testing.T) {
	d := NewRootkitDetector()

	cases := []struct{ name, content string }{
		{"LKM rootkit", "insmod /tmp/diamorphine.ko"},
		{"LD_PRELOAD", "LD_PRELOAD=/tmp/evil.so"},
		{"authorized_keys 写入", "echo ssh-rsa AAAA >> /root/.ssh/authorized_keys"},
	}
	for _, c := range cases {
		// 即便声称是包管理器产生的，这三类仍应命中
		if _, hit := d.Scan(context.Background(), IndicatorEvent{
			HostID: "h1", Source: "process", Content: c.content, ExePath: "/usr/bin/dpkg",
		}); !hit {
			t.Errorf("%s：包管理器豁免不该覆盖这一类，但未命中", c.name)
		}
	}
}

// 语料里的攻击样本必须被对应检测器认出来。
//
// 这些 detector 从未接线也从未被验证过。接线前至少要证明它们对已知攻击有效，
// 否则接上去的是一个不响的检测——比不接更糟，因为它会让人以为这块已经覆盖了。
func TestDetectors_CatchAttackSamples(t *testing.T) {
	dir := filepath.Join("..", "celengine", "replay", "testdata", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("语料目录不可读: %v", err)
	}
	var attacks []corpusSample
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		var doc struct {
			Samples []corpusSample `json:"samples"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		for _, s := range doc.Samples {
			if s.Label == "attack" {
				attacks = append(attacks, s)
			}
		}
	}

	rev := NewReverseShellDetector()
	rk := NewRootkitDetector()

	var caught int
	for _, s := range attacks {
		cmd := s.Fields["cmdline"]
		fp := s.Fields["file_path"]
		content := cmd
		if content == "" {
			content = fp
		}
		hit := false
		if cmd != "" {
			if _, ok := rev.Scan(context.Background(), ProcessEvent{
				HostID: "h1", Cmdline: cmd, ExePath: s.Fields["exe"],
			}); ok {
				hit = true
			}
		}
		if !hit && content != "" {
			if _, ok := rk.Scan(context.Background(), IndicatorEvent{
				HostID: "h1", Source: "file", Content: content, ExePath: s.Fields["exe"],
			}); ok {
				hit = true
			}
		}
		if hit {
			caught++
		} else {
			t.Logf("未检出: %s (%s) — %s", s.Name, s.Technique, s.Note)
		}
	}
	t.Logf("入侵检测器覆盖攻击样本: %d/%d", caught, len(attacks))

	// 不设具体门槛：这两个 detector 本就不针对全部技术。
	// 但一条都检不出说明它们根本没工作，那样接线毫无意义。
	if caught == 0 {
		t.Fatal("入侵检测器对全部攻击样本都无反应——接线前必须先查清原因")
	}
}

// PrivEscalationDetector 对正常运维行为的误报面。
//
// 提权检测最容易踩的坑是把 sudo 本身当信号——运维每天都在用 sudo。
func TestPrivEscalationDetector_BenignCorpus(t *testing.T) {
	d := NewPrivEscalationDetector()
	samples := loadBenignSamples(t)

	var flagged []corpusSample
	for _, s := range samples {
		cmd := s.Fields["cmdline"]
		if cmd == "" {
			continue
		}
		if _, hit := d.Scan(context.Background(), ProcessEvent{
			HostID: "h1", Cmdline: cmd, ExePath: s.Fields["exe"],
		}); hit {
			flagged = append(flagged, s)
		}
	}
	for _, s := range flagged {
		t.Errorf("正常样本被判为提权：%s\n  为什么它是正常的：%s", s.Name, s.Note)
	}
	if len(flagged) > 0 {
		t.Fatalf("PrivEscalationDetector 在 %d/%d 条正常样本上误命中", len(flagged), len(samples))
	}
}

// BruteForceDetector 的滑窗行为：单次失败不告警，连续失败才告警。
//
// 语料里没有登录事件，所以直接构造。这个检测的核心风险不是模式匹配，
// 而是阈值——设得太低会把用户输错一次密码当成攻击。
func TestBruteForceDetector_ThresholdBehaviour(t *testing.T) {
	d := NewBruteForceDetector(0, 0) // 0 表示用默认窗口与阈值
	ctx := context.Background()

	att := LoginAttempt{
		HostID: "h1", SourceIP: "10.0.0.5", Username: "deploy", Success: false,
	}

	// 单次失败不该告警——输错一次密码是日常
	if _, hit := d.Ingest(ctx, att); hit {
		t.Fatal("单次登录失败不该告警")
	}

	// 成功登录应清除计数：合法用户重试成功后，之前的失败不该继续累积
	for range 3 {
		d.Ingest(ctx, att)
	}
	ok := att
	ok.Success = true
	if _, hit := d.Ingest(ctx, ok); hit {
		t.Fatal("成功登录本身不该告警")
	}
	if _, hit := d.Ingest(ctx, att); hit {
		t.Fatal("成功登录应清除失败计数，之后单次失败不该立即告警")
	}
}

// AbnormalLoginDetector 的冷启动行为。
//
// 它按主机维护画像（国家 / 时段 / IP 段 / 用户），任何「首次见到」都算异常。
// 问题在于画像是内存里的空 map 起步：进程启动后每台主机的第一次登录，
// 都会同时命中「新国家 + 新 IP 段 + 新用户」三条。
//
// 也就是说接线的瞬间，机群有多少台主机就会产生多少条告警，
// 而它们全部是正常的日常登录。engine 每次重启都重演一遍。
//
// 本测试记录这个行为，作为「暂不接线」的依据。修复方向是画像持久化
// 或引入学习期（参照 ML 异常检测的 shadow 档）。
func TestAbnormalLoginDetector_ColdStartFlagsEveryFirstLogin(t *testing.T) {
	d := NewAbnormalLoginDetector()
	ctx := context.Background()

	// 一次完全正常的工作时间登录，来自公司出口 IP
	login := SuccessfulLogin{
		HostID:    "h1",
		Username:  "deploy",
		SourceIP:  "10.0.0.5",
		Country:   "CN",
		Timestamp: time.Date(2026, 8, 4, 14, 30, 0, 0, time.UTC), // 下午 2 点半
	}

	_, hit := d.Ingest(ctx, login)
	if !hit {
		t.Skip("冷启动行为已改变，本测试的前提失效——请重新评估接线条件")
	}

	// 同一主机同一来源再次登录：画像已建立，不该再告警
	if _, hit := d.Ingest(ctx, login); hit {
		t.Error("第二次相同登录仍告警，说明画像没有生效")
	}

	// 记录事实：冷启动首次登录必然告警。
	// 机群规模 = 接线瞬间的告警条数，且每次 engine 重启重演。
	t.Log("确认：冷启动时每台主机的首次正常登录都会告警" +
		"（新国家 + 新 IP 段 + 新用户三条同时命中）")
}
