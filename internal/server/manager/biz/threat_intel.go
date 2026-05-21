// Package biz - C3: 威胁情报（内置 Feed 直拉）
package biz

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

const (
	iocRedisKeyPrefix = "mxsec:ioc:"
	iocTTL            = 24 * time.Hour
)

// feedSource 内置 Feed 数据源定义
type feedSource struct {
	Name    string // 来源名称
	URL     string // Feed URL
	IOCType string // IOC 类型：ip / url / hash
}

// 内置免费公开 Feed 列表
var builtinFeeds = []feedSource{
	{Name: "abuse.ch Feodo IP", URL: "https://feodotracker.abuse.ch/downloads/ipblocklist.txt", IOCType: "ip"},
	{Name: "abuse.ch URLhaus", URL: "https://urlhaus.abuse.ch/downloads/text/", IOCType: "url"},
	{Name: "abuse.ch MalwareBazaar MD5", URL: "https://bazaar.abuse.ch/export/txt/md5/recent/", IOCType: "hash"},
}

// ThreatIntel 威胁情报服务
type ThreatIntel struct {
	db          *gorm.DB
	redisClient *redis.Client
	logger      *zap.Logger
	httpClient  *http.Client
}

// NewThreatIntel 创建威胁情报服务
func NewThreatIntel(db *gorm.DB, redisClient *redis.Client, logger *zap.Logger) *ThreatIntel {
	return &ThreatIntel{
		db:          db,
		redisClient: redisClient,
		logger:      logger,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// IOC 威胁指标
type IOC struct {
	Type  string   `json:"type"` // ip, domain, hash, url
	Value string   `json:"value"`
	Tags  []string `json:"tags,omitempty"`
}

// SyncIOCs 拉取内置 Feed 最新 IOC 并写入 Redis Set
func (t *ThreatIntel) SyncIOCs(ctx context.Context) error {
	startedAt := time.Now()

	// 插入 running 记录
	record := model.SecurityDBSyncRecord{
		DBType:    "threat-intel",
		Status:    "running",
		StartedAt: startedAt,
	}
	t.db.Create(&record)

	err := t.doSyncIOCs(ctx)

	duration := int(time.Since(startedAt).Seconds())
	updates := map[string]interface{}{
		"duration": duration,
	}

	if err != nil {
		updates["status"] = "failed"
		updates["error_msg"] = err.Error()
	} else {
		updates["status"] = "success"
		updates["version"] = time.Now().Format("20060102.150405")

		// 统计各类型 IOC 数量
		if t.redisClient != nil {
			var totalIOC int64
			for _, iocType := range []string{"ip", "hash", "domain", "url"} {
				if n, e := t.redisClient.SCard(ctx, iocRedisKeyPrefix+iocType).Result(); e == nil {
					totalIOC += n
				}
			}
			updates["file_size"] = totalIOC // 复用 file_size 存储 IOC 总数
		}
	}

	if dbErr := t.db.Model(&record).Updates(updates).Error; dbErr != nil {
		t.logger.Error("更新同步记录失败", zap.Error(dbErr))
	}

	return err
}

// doSyncIOCs 遍历内置 Feed 列表，逐个拉取并写入 Redis
func (t *ThreatIntel) doSyncIOCs(ctx context.Context) error {
	if t.redisClient == nil {
		return fmt.Errorf("Redis 不可用")
	}

	t.logger.Info("开始同步威胁情报", zap.Int("feeds", len(builtinFeeds)))

	var totalCount int
	var lastErr error

	for _, feed := range builtinFeeds {
		count, err := t.fetchFeed(ctx, feed)
		if err != nil {
			t.logger.Warn("拉取 Feed 失败",
				zap.String("name", feed.Name),
				zap.Error(err))
			lastErr = err
			continue
		}
		totalCount += count
		t.logger.Info("Feed 拉取完成",
			zap.String("name", feed.Name),
			zap.Int("count", count))
	}

	if totalCount == 0 && lastErr != nil {
		return fmt.Errorf("所有 Feed 拉取失败，最后错误: %w", lastErr)
	}

	t.logger.Info("威胁情报同步完成", zap.Int("total", totalCount))

	// Export snapshot to DB for AgentCenter to broadcast to agents.
	if err := t.exportSnapshot(ctx); err != nil {
		t.logger.Warn("IOC 快照导出失败", zap.Error(err))
	}

	return nil
}

// fetchFeed 拉取单个 Feed 并写入 Redis Set
func (t *ThreatIntel) fetchFeed(ctx context.Context, feed feedSource) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feed.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	key := iocRedisKeyPrefix + feed.IOCType
	pipe := t.redisClient.Pipeline()
	count := 0

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pipe.SAdd(ctx, key, line)
		count++

		// 每 1000 条执行一次 pipeline
		if count%1000 == 0 {
			if _, err := pipe.Exec(ctx); err != nil {
				return count, fmt.Errorf("Redis pipeline 写入失败: %w", err)
			}
			pipe = t.redisClient.Pipeline()
		}
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("读取 Feed 响应失败: %w", err)
	}

	// 刷新剩余 + 设置 TTL
	pipe.Expire(ctx, key, iocTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return count, fmt.Errorf("Redis 最终写入失败: %w", err)
	}

	return count, nil
}

// GetLatestSyncStatus 查询最近一条同步记录
func (t *ThreatIntel) GetLatestSyncStatus() (*model.SecurityDBSyncRecord, error) {
	var record model.SecurityDBSyncRecord
	err := t.db.Where("db_type = ?", "threat-intel").Order("id DESC").First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// GetSyncHistory 分页查询同步历史记录
func (t *ThreatIntel) GetSyncHistory(page, pageSize int) ([]model.SecurityDBSyncRecord, int64, error) {
	var total int64
	query := t.db.Model(&model.SecurityDBSyncRecord{}).Where("db_type = ?", "threat-intel")
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []model.SecurityDBSyncRecord
	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&records).Error
	return records, total, err
}

// CheckIOC 检查值是否在 IOC 集合中
func (t *ThreatIntel) CheckIOC(ctx context.Context, iocType, value string) bool {
	if t.redisClient == nil {
		return false
	}
	key := iocRedisKeyPrefix + iocType
	result, err := t.redisClient.SIsMember(ctx, key, value).Result()
	if err != nil {
		return false
	}
	return result
}

// ── IOC Snapshot Export ─────────────────────────────────────

// iocData is the JSON structure for IOC snapshots.
type iocData struct {
	IP   []string `json:"ip"`
	Hash []string `json:"hash"`
	URL  []string `json:"url"`
}

// exportSnapshot reads all IOC data from Redis, computes diff against previous
// snapshot, and writes a new IOCSnapshot record for AgentCenter to distribute.
func (t *ThreatIntel) exportSnapshot(ctx context.Context) error {
	if t.redisClient == nil {
		return fmt.Errorf("Redis 不可用")
	}

	// 1. Read all IOC sets from Redis.
	current := iocData{}
	for _, entry := range []struct {
		key  string
		dest *[]string
	}{
		{iocRedisKeyPrefix + "ip", &current.IP},
		{iocRedisKeyPrefix + "hash", &current.Hash},
		{iocRedisKeyPrefix + "url", &current.URL},
	} {
		members, err := t.redisClient.SMembers(ctx, entry.key).Result()
		if err != nil {
			return fmt.Errorf("读取 Redis Set %s 失败: %w", entry.key, err)
		}
		sort.Strings(members)
		*entry.dest = members
	}

	// 2. Serialize full data.
	fullJSON, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("序列化 IOC 数据失败: %w", err)
	}

	version := fmt.Sprintf("%x", sha256.Sum256(fullJSON))[:16]
	totalCount := len(current.IP) + len(current.Hash) + len(current.URL)

	// 3. Load previous snapshot for diff.
	var prev model.IOCSnapshot
	prevExists := t.db.Order("id DESC").First(&prev).Error == nil

	var diffAdded, diffRemoved iocData
	prevVersion := ""
	if prevExists {
		prevVersion = prev.Version
		if prevVersion == version {
			t.logger.Info("IOC 快照无变化，跳过导出", zap.String("version", version))
			return nil
		}

		var prevData iocData
		if err := json.Unmarshal([]byte(prev.Data), &prevData); err == nil {
			diffAdded.IP, diffRemoved.IP = diffSets(prevData.IP, current.IP)
			diffAdded.Hash, diffRemoved.Hash = diffSets(prevData.Hash, current.Hash)
			diffAdded.URL, diffRemoved.URL = diffSets(prevData.URL, current.URL)
		}
	} else {
		// First snapshot: everything is "added".
		diffAdded = current
	}

	addedJSON, _ := json.Marshal(diffAdded)
	removedJSON, _ := json.Marshal(diffRemoved)

	// 4. Write snapshot.
	snapshot := model.IOCSnapshot{
		Version:   version,
		Data:      string(fullJSON),
		DiffAdded: string(addedJSON),
		DiffRemov: string(removedJSON),
		PrevVer:   prevVersion,
		Count:     totalCount,
	}
	if err := t.db.Create(&snapshot).Error; err != nil {
		return fmt.Errorf("写入 IOC 快照失败: %w", err)
	}

	// 5. Clean up old snapshots (keep 5).
	var old []model.IOCSnapshot
	if err := t.db.Order("id DESC").Offset(5).Find(&old).Error; err == nil && len(old) > 0 {
		ids := make([]uint, len(old))
		for i, o := range old {
			ids[i] = o.ID
		}
		t.db.Delete(&model.IOCSnapshot{}, ids)
	}

	t.logger.Info("IOC 快照导出完成",
		zap.String("version", version),
		zap.Int("total", totalCount),
		zap.Int("added", len(diffAdded.IP)+len(diffAdded.Hash)+len(diffAdded.URL)),
		zap.Int("removed", len(diffRemoved.IP)+len(diffRemoved.Hash)+len(diffRemoved.URL)),
	)
	return nil
}

// diffSets returns (added, removed) between old and new sorted string slices.
func diffSets(old, cur []string) (added, removed []string) {
	oldSet := make(map[string]struct{}, len(old))
	for _, v := range old {
		oldSet[v] = struct{}{}
	}
	curSet := make(map[string]struct{}, len(cur))
	for _, v := range cur {
		curSet[v] = struct{}{}
	}

	for _, v := range cur {
		if _, ok := oldSet[v]; !ok {
			added = append(added, v)
		}
	}
	for _, v := range old {
		if _, ok := curSet[v]; !ok {
			removed = append(removed, v)
		}
	}
	return
}
