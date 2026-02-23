package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// parseState AIDE 输出解析状态
type parseState int

const (
	stateInit parseState = iota
	stateSummary
	stateAddedEntries
	stateRemovedEntries
	stateChangedEntries
	stateDetailedInfo
	stateFileDetail
)

// summaryRegex 匹配摘要行，如 "Total number of entries:	123"
var summaryRegex = regexp.MustCompile(`Total number of (?:entries|files):\s*(\d+)`)
var addedRegex = regexp.MustCompile(`Added (?:entries|files):\s*(\d+)`)
var removedRegex = regexp.MustCompile(`Removed (?:entries|files):\s*(\d+)`)
var changedRegex = regexp.MustCompile(`Changed (?:entries|files):\s*(\d+)`)

// entryLineRegex 匹配条目行，如 "added: /usr/bin/newfile" 或 "f > ... ..H.... : /etc/passwd"
var addedEntryRegex = regexp.MustCompile(`^(?:added|Added):\s+(.+)$`)
var removedEntryRegex = regexp.MustCompile(`^(?:removed|Removed):\s+(.+)$`)
var changedEntryRegex = regexp.MustCompile(`^(?:changed|Changed):\s+(.+)$`)

// AIDE 0.15 格式: "f > ... ..H.... : /path"
var aide015EntryRegex = regexp.MustCompile(`^[a-z]\s+[><=]\s+(\S+)\s*:\s+(.+)$`)

// detailLineRegex 匹配详情行，如 "Size     : 3907                             , 3931"
var detailLineRegex = regexp.MustCompile(`^\s+(\w[\w\s]*):\s+(.+)$`)

// fileHeaderRegex 匹配文件详情头，如 "File: /etc/passwd"
var fileHeaderRegex = regexp.MustCompile(`^(?:File|Entry):\s+(.+)$`)

// Parse 解析 AIDE --check 的输出，返回 AIDEReport
// 兼容 AIDE 0.15（CentOS 7）和 0.19（Rocky 9）
func Parse(output string) *AIDEReport {
	report := &AIDEReport{}
	lines := strings.Split(output, "\n")

	state := stateInit
	eventMap := make(map[string]*FIMEvent) // filePath -> event
	var currentDetailFile string
	eventCounter := 0

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")

		// 解析摘要信息（任何状态下都尝试）
		if parseSummaryLine(line, &report.Summary) {
			state = stateSummary
			continue
		}

		// 检测段落标题
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Added entries") ||
			strings.HasPrefix(trimmed, "Added files"):
			state = stateAddedEntries
			continue
		case strings.HasPrefix(trimmed, "Removed entries") ||
			strings.HasPrefix(trimmed, "Removed files"):
			state = stateRemovedEntries
			continue
		case strings.HasPrefix(trimmed, "Changed entries") ||
			strings.HasPrefix(trimmed, "Changed files"):
			state = stateChangedEntries
			continue
		case strings.HasPrefix(trimmed, "Detailed information") ||
			strings.HasPrefix(trimmed, "---"):
			if state == stateChangedEntries || state == stateAddedEntries || state == stateRemovedEntries {
				// 分隔线，保持当前状态
				continue
			}
			if strings.HasPrefix(trimmed, "Detailed information") {
				state = stateDetailedInfo
				continue
			}
		}

		// 根据状态解析
		switch state {
		case stateAddedEntries:
			if path := parseEntryPath(line, "added"); path != "" {
				eventCounter++
				ev := &FIMEvent{
					EventID:    fmt.Sprintf("evt-%06d", eventCounter),
					FilePath:   path,
					ChangeType: "added",
				}
				eventMap[path] = ev
			}
		case stateRemovedEntries:
			if path := parseEntryPath(line, "removed"); path != "" {
				eventCounter++
				ev := &FIMEvent{
					EventID:    fmt.Sprintf("evt-%06d", eventCounter),
					FilePath:   path,
					ChangeType: "removed",
				}
				eventMap[path] = ev
			}
		case stateChangedEntries:
			path, attrs := parseChangedEntry(line)
			if path != "" {
				eventCounter++
				ev := &FIMEvent{
					EventID:    fmt.Sprintf("evt-%06d", eventCounter),
					FilePath:   path,
					ChangeType: "changed",
					ChangeDetail: ChangeDetail{
						Attributes: attrs,
					},
				}
				parseAttributeFlags(attrs, &ev.ChangeDetail)
				eventMap[path] = ev
			}

		case stateDetailedInfo, stateFileDetail:
			// 检查文件详情头
			if m := fileHeaderRegex.FindStringSubmatch(trimmed); m != nil {
				currentDetailFile = strings.TrimSpace(m[1])
				state = stateFileDetail
				continue
			}
			// 解析详情行
			if currentDetailFile != "" {
				parseDetailLine(trimmed, eventMap[currentDetailFile])
			}
		}
	}

	// 收集所有事件
	for _, ev := range eventMap {
		report.Events = append(report.Events, *ev)
	}

	return report
}

// parseSummaryLine 尝试从行中提取摘要数据，返回是否匹配
func parseSummaryLine(line string, summary *AIDESummary) bool {
	if m := summaryRegex.FindStringSubmatch(line); m != nil {
		summary.TotalEntries, _ = strconv.Atoi(m[1])
		return true
	}
	if m := addedRegex.FindStringSubmatch(line); m != nil {
		summary.AddedEntries, _ = strconv.Atoi(m[1])
		return true
	}
	if m := removedRegex.FindStringSubmatch(line); m != nil {
		summary.RemovedEntries, _ = strconv.Atoi(m[1])
		return true
	}
	if m := changedRegex.FindStringSubmatch(line); m != nil {
		summary.ChangedEntries, _ = strconv.Atoi(m[1])
		return true
	}
	return false
}

// parseEntryPath 从 added/removed 条目行提取文件路径
func parseEntryPath(line, changeType string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}

	var re *regexp.Regexp
	if changeType == "added" {
		re = addedEntryRegex
	} else {
		re = removedEntryRegex
	}

	if m := re.FindStringSubmatch(trimmed); m != nil {
		return strings.TrimSpace(m[1])
	}

	// 尝试 AIDE 0.15 格式
	if m := aide015EntryRegex.FindStringSubmatch(trimmed); m != nil {
		return strings.TrimSpace(m[2])
	}

	// 简单路径行（以 / 开头）
	if strings.HasPrefix(trimmed, "/") {
		return strings.Fields(trimmed)[0]
	}

	return ""
}

// parseChangedEntry 从 changed 条目行提取路径和属性标记
func parseChangedEntry(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", ""
	}

	if m := changedEntryRegex.FindStringSubmatch(trimmed); m != nil {
		return strings.TrimSpace(m[1]), ""
	}

	// AIDE 0.15 格式: "f > ... ..H.... : /path"
	if m := aide015EntryRegex.FindStringSubmatch(trimmed); m != nil {
		return strings.TrimSpace(m[2]), strings.TrimSpace(m[1])
	}

	// 简单路径行
	if strings.HasPrefix(trimmed, "/") {
		return strings.Fields(trimmed)[0], ""
	}

	return "", ""
}

// parseAttributeFlags 从 AIDE 属性标记字符串解析变更详情
// 标记格式如 "..H...." 其中每个位置代表不同属性
// 位置: p=permissions, i=inode, n=links, u=user, g=group, s=size, m=mtime, c=ctime, H=hash, ...
func parseAttributeFlags(attrs string, detail *ChangeDetail) {
	if attrs == "" {
		return
	}
	for _, ch := range attrs {
		switch ch {
		case 'H', 'C', 'h': // hash changed
			detail.HashChanged = true
		case 'p', 'a': // permissions / acl
			detail.PermissionChanged = true
		case 'u', 'g': // user / group
			detail.OwnerChanged = true
		}
	}
}

// parseDetailLine 解析详情行并更新对应事件
// 格式: "  Size     : 3907                             , 3931"
// 或:   "  Size     : 3907 | 3931"
func parseDetailLine(line string, event *FIMEvent) {
	if event == nil || line == "" {
		return
	}

	m := detailLineRegex.FindStringSubmatch(line)
	if m == nil {
		return
	}

	key := strings.TrimSpace(m[1])
	value := strings.TrimSpace(m[2])

	// 分割前后值（支持 "," 和 "|" 分隔符）
	var before, after string
	for _, sep := range []string{" | ", ", ", " , "} {
		if parts := strings.SplitN(value, sep, 2); len(parts) == 2 {
			before = strings.TrimSpace(parts[0])
			after = strings.TrimSpace(parts[1])
			break
		}
	}
	if before == "" {
		before = value
	}

	switch strings.ToLower(key) {
	case "size":
		event.ChangeDetail.SizeBefore = before
		event.ChangeDetail.SizeAfter = after
	case "sha512", "sha256", "md5":
		event.ChangeDetail.HashChanged = true
	case "perm", "permissions":
		event.ChangeDetail.PermissionChanged = true
	case "uid", "gid", "user", "group":
		event.ChangeDetail.OwnerChanged = true
	}
}
