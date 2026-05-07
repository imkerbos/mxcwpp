// Package middleware 提供 HTTP 中间件
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// AuditLog 审计日志中间件，记录 POST/PUT/DELETE 操作
func AuditLog(db *gorm.DB, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		// 只记录写操作
		if method != "POST" && method != "PUT" && method != "DELETE" {
			c.Next()
			return
		}

		c.Next()

		// 从 context 获取当前用户（由 AuthMiddleware 注入）
		username, _ := c.Get("username")
		usernameStr, _ := username.(string)
		if usernameStr == "" {
			usernameStr = "unknown"
		}

		path := c.Request.URL.Path
		resourceType, resourceID := extractResource(path)

		log := &model.AuditLog{
			Username:     usernameStr,
			Action:       method,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			Path:         path,
			IP:           c.ClientIP(),
			StatusCode:   c.Writer.Status(),
		}

		if err := db.Create(log).Error; err != nil {
			logger.Warn("记录审计日志失败", zap.Error(err))
		}
	}
}

// extractResource 从路径提取资源类型和资源 ID
// 例如 /api/v1/hosts/abc123 -> ("hosts", "abc123")
//
//	/api/v1/alerts/batch/resolve -> ("alerts", "")
func extractResource(path string) (resourceType, resourceID string) {
	// 去掉 /api/v1/ 前缀
	path = strings.TrimPrefix(path, "/api/v1/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 0 {
		return "unknown", ""
	}
	resourceType = parts[0]
	if len(parts) >= 2 {
		// 如果第二段是数字或看起来像 ID，则作为资源 ID
		second := parts[1]
		if second != "" && second != "batch" && second != "statistics" &&
			second != "whitelist" && second != "resolve" && second != "ignore" {
			resourceID = second
		}
	}
	return resourceType, resourceID
}
