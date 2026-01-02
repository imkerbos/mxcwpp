# 系统配置 API 路由注册说明

## 概述

本文档说明如何在 Manager 的 main.go 中注册系统配置相关的 API 路由。

## 路由注册

在 Manager 的 main.go 文件中，需要添加以下代码来注册系统配置 API：

```go
package main

import (
	// ... 其他导入
	"github.com/imkerbos/mxsec-platform/internal/server/manager/api"
)

func main() {
	// ... 初始化配置、数据库、日志等

	// 创建 Gin 路由
	router := gin.New()
	
	// ... 配置中间件（CORS、日志、Recovery等）

	// 创建 API v1 路由组
	apiV1 := router.Group("/api/v1")

	// ... 注册其他 API（hosts、policies、tasks 等）

	// ===== 系统配置 API =====
	// 创建 SystemConfigHandler
	// 参数1: 上传目录（文件系统路径）- 项目根目录下的 uploads 文件夹
	// 参数2: HTTP 访问路径 - 通过 /uploads 访问上传的文件
	systemConfigHandler := api.NewSystemConfigHandler(
		db,
		logger,
		"./uploads",  // 文件保存在项目根目录下的 uploads 文件夹
		"/uploads",   // 通过 /uploads/文件名 访问
	)

	// 注册系统配置路由组
	systemConfig := apiV1.Group("/system-config")
	{
		// 站点配置
		systemConfig.GET("/site", systemConfigHandler.GetSiteConfig)
		systemConfig.PUT("/site", systemConfigHandler.UpdateSiteConfig)
		
		// Logo 上传
		systemConfig.POST("/upload-logo", systemConfigHandler.UploadLogo)
		
		// Kubernetes 镜像配置（如果已实现）
		systemConfig.GET("/kubernetes-image", systemConfigHandler.GetKubernetesImageConfig)
		systemConfig.PUT("/kubernetes-image", systemConfigHandler.UpdateKubernetesImageConfig)
	}

	// ===== 静态文件服务 =====
	// 配置静态文件服务，使上传的文件可以通过 HTTP 访问
	// 这样上传的 Logo 可以通过 /uploads/文件名 直接访问
	router.Static("/uploads", "./uploads")

	// ... 启动 HTTP Server
}
```

## 完整示例

假设你的 main.go 文件结构如下：

```go
package main

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	
	"github.com/imkerbos/mxsec-platform/internal/server/manager/api"
	// ... 其他导入
)

func main() {
	// 1. 初始化配置
	cfg := loadConfig()
	
	// 2. 初始化日志
	logger := initLogger(cfg)
	
	// 3. 初始化数据库
	db := initDatabase(cfg)
	
	// 4. 创建 Gin 路由
	router := gin.New()
	
	// 5. 配置中间件
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())
	router.Use(loggingMiddleware(logger))
	
	// 6. 创建 API v1 路由组
	apiV1 := router.Group("/api/v1")
	
	// 7. 注册其他 API
	// ... hosts, policies, tasks, results 等
	
	// 8. 注册系统配置 API
	systemConfigHandler := api.NewSystemConfigHandler(
		db,
		logger,
		"./uploads",  // 上传目录
		"/uploads",   // HTTP 访问路径
	)
	
	systemConfig := apiV1.Group("/system-config")
	{
		systemConfig.GET("/site", systemConfigHandler.GetSiteConfig)
		systemConfig.PUT("/site", systemConfigHandler.UpdateSiteConfig)
		systemConfig.POST("/upload-logo", systemConfigHandler.UploadLogo)
		systemConfig.GET("/kubernetes-image", systemConfigHandler.GetKubernetesImageConfig)
		systemConfig.PUT("/kubernetes-image", systemConfigHandler.UpdateKubernetesImageConfig)
	}
	
	// 9. 配置静态文件服务
	router.Static("/uploads", "./uploads")
	
	// 10. 启动 HTTP Server
	address := fmt.Sprintf("%s:%d", cfg.Server.HTTP.Host, cfg.Server.HTTP.Port)
	logger.Info("Manager HTTP Server starting", zap.String("address", address))
	if err := router.Run(address); err != nil {
		logger.Fatal("Failed to start HTTP Server", zap.Error(err))
	}
}
```

## API 端点说明

注册完成后，以下 API 端点将可用：

### 站点配置
- `GET /api/v1/system-config/site` - 获取站点配置
- `PUT /api/v1/system-config/site` - 更新站点配置

### Logo 上传
- `POST /api/v1/system-config/upload-logo` - 上传 Logo 文件

### Kubernetes 镜像配置
- `GET /api/v1/system-config/kubernetes-image` - 获取 Kubernetes 镜像配置
- `PUT /api/v1/system-config/kubernetes-image` - 更新 Kubernetes 镜像配置

### 静态文件访问
- `GET /uploads/logo_*.png` - 访问上传的 Logo 文件

## 注意事项

1. **上传目录**：确保 `./uploads` 目录存在且有写权限
2. **静态文件服务**：`router.Static("/uploads", "./uploads")` 必须在路由注册之后配置
3. **文件大小限制**：默认限制为 5MB，可在 `UploadLogo` 函数中修改
4. **文件类型限制**：仅支持图片格式（jpg、jpeg、png、gif、svg、webp）

## 故障排查

如果遇到 404 错误：
1. 检查路由是否正确注册
2. 检查 API 路径是否正确（应该是 `/api/v1/system-config/site`）
3. 检查 HTTP Server 是否正常启动
4. 检查中间件是否阻止了请求
