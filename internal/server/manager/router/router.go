// Package router 提供 HTTP 路由配置
package router

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/config"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/manager/api"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/manager/biz"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/manager/middleware"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/metrics"
)

// Setup 设置并返回配置好的 Gin 路由引擎
func Setup(db *gorm.DB, logger *zap.Logger, cfg *config.Config, scoreCache *biz.BaselineScoreCache, metricsService *biz.MetricsService) *gin.Engine {
	// 设置 Gin 模式
	if cfg.Log.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// 中间件
	router.Use(middleware.Logger(logger))
	router.Use(gin.Recovery())
	router.Use(middleware.CORS())

	// 健康检查（支持 GET 和 HEAD 方法，Docker healthcheck 可能使用 HEAD）
	healthHandler := api.NewHealthHandler(db, logger)
	router.GET("/health", healthHandler.Health)
	router.HEAD("/health", healthHandler.Health)

	// Prometheus metrics 端点
	router.GET("/metrics", gin.WrapH(metrics.Handler()))

	// Agent 安装脚本路由（不需要认证）
	agentHandler := api.NewAgentHandler(logger, cfg.Server.GRPC.Address(), cfg.Server.HTTP.Address())
	router.GET("/agent/install.sh", agentHandler.InstallScript)
	router.GET("/agent/uninstall.sh", agentHandler.UninstallScript)

	// 插件下载路由（不需要认证，Agent 直接下载）
	// 注意：现在由 componentsHandler 统一处理
	componentsHandler := api.NewComponentsHandler(db, logger, "./uploads", "/uploads")
	router.GET("/api/v1/plugins/download/:name", componentsHandler.DownloadPluginPackage)
	router.HEAD("/api/v1/plugins/download/:name", componentsHandler.DownloadPluginPackage)

	// Agent 安装包下载路由（不需要认证，安装脚本直接下载）
	router.GET("/api/v1/agent/download/:pkg_type/:arch", componentsHandler.DownloadAgentPackage)

	// 静态文件服务（用于访问上传的 Logo 等文件）
	router.Static("/uploads", "./uploads")

	// API 路由
	apiV1 := router.Group("/api/v1")

	// 认证相关路由（不需要认证）
	jwtSecret := cfg.Server.JWTSecret
	if jwtSecret == "" {
		jwtSecret = "default-secret-key-change-in-production" // 默认密钥，生产环境必须修改
		logger.Warn("使用默认 JWT 密钥，生产环境请修改配置")
	}
	authHandler := api.NewAuthHandler(db, logger, []byte(jwtSecret))
	apiV1.POST("/auth/login", authHandler.Login)
	apiV1.POST("/auth/logout", authHandler.Logout)
	apiV1.GET("/auth/me", authHandler.GetCurrentUser)
	apiV1.POST("/auth/change-password", authHandler.AuthMiddleware(), authHandler.ChangePassword)

	// 系统配置 - 获取站点配置（不需要认证，登录页面也需要显示站点名称）
	systemConfigHandler := api.NewSystemConfigHandler(db, logger, "./uploads", "/uploads")
	apiV1.GET("/system-config/site", systemConfigHandler.GetSiteConfig)

	// 需要认证的路由
	apiV1Auth := apiV1.Group("")
	apiV1Auth.Use(authHandler.AuthMiddleware())

	// 注册所有 API 路由
	setupAPIRoutes(apiV1Auth, db, logger, scoreCache, metricsService)

	return router
}

// setupAPIRoutes 注册所有需要认证的 API 路由
func setupAPIRoutes(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger, scoreCache *biz.BaselineScoreCache, metricsService *biz.MetricsService) {
	setupHostsAPI(router, db, logger, scoreCache, metricsService)
	setupPolicyGroupsAPI(router, db, logger)
	setupPoliciesAPI(router, db, logger)
	setupRulesAPI(router, db, logger)
	setupTasksAPI(router, db, logger)
	setupResultsAPI(router, db, logger)
	setupDashboardAPI(router, db, logger)
	setupUsersAPI(router, db, logger)
	setupAssetsAPI(router, db, logger)
	setupReportsAPI(router, db, logger)
	setupBusinessLinesAPI(router, db, logger)
	setupSystemConfigAPI(router, db, logger)
	setupNotificationsAPI(router, db, logger)
	setupAlertsAPI(router, db, logger)
	setupComponentsAPI(router, db, logger)
}

// setupHostsAPI 设置主机 API 路由
func setupHostsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger, scoreCache *biz.BaselineScoreCache, metricsService *biz.MetricsService) {
	handler := api.NewHostsHandler(db, logger, scoreCache, metricsService)
	router.GET("/hosts", handler.ListHosts)
	router.GET("/hosts/:host_id", handler.GetHost)
	router.GET("/hosts/:host_id/metrics", handler.GetHostMetrics)
	router.GET("/hosts/:host_id/risk-statistics", handler.GetHostRiskStatistics)
	router.PUT("/hosts/:host_id/tags", handler.UpdateHostTags)
	router.PUT("/hosts/:host_id/business-line", handler.UpdateHostBusinessLine)
	router.GET("/hosts/status-distribution", handler.GetHostStatusDistribution)
	router.GET("/hosts/risk-distribution", handler.GetHostRiskDistribution)
}

// setupPolicyGroupsAPI 设置策略组 API 路由
func setupPolicyGroupsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewPolicyGroupsHandler(db, logger)
	router.GET("/policy-groups", handler.ListPolicyGroups)
	router.GET("/policy-groups/:id", handler.GetPolicyGroup)
	router.GET("/policy-groups/:id/statistics", handler.GetPolicyGroupStatistics)
	router.POST("/policy-groups", handler.CreatePolicyGroup)
	router.PUT("/policy-groups/:id", handler.UpdatePolicyGroup)
	router.DELETE("/policy-groups/:id", handler.DeletePolicyGroup)
}

// setupPoliciesAPI 设置策略 API 路由
func setupPoliciesAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewPoliciesHandler(db, logger)
	router.GET("/policies", handler.ListPolicies)
	router.GET("/policies/:policy_id", handler.GetPolicy)
	router.GET("/policies/:policy_id/statistics", handler.GetPolicyStatistics)
	router.POST("/policies", handler.CreatePolicy)
	router.PUT("/policies/:policy_id", handler.UpdatePolicy)
	router.DELETE("/policies/:policy_id", handler.DeletePolicy)
}

// setupRulesAPI 设置规则 API 路由
func setupRulesAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewRulesHandler(db, logger)
	router.GET("/policies/:policy_id/rules", handler.ListRules)
	router.POST("/policies/:policy_id/rules", handler.CreateRule)
	router.GET("/rules/:rule_id", handler.GetRule)
	router.PUT("/rules/:rule_id", handler.UpdateRule)
	router.DELETE("/rules/:rule_id", handler.DeleteRule)
}

// setupTasksAPI 设置任务 API 路由
func setupTasksAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewTasksHandler(db, logger)
	router.GET("/tasks", handler.ListTasks)
	router.GET("/tasks/:task_id", handler.GetTask)
	router.POST("/tasks", handler.CreateTask)
	router.POST("/tasks/:task_id/run", handler.RunTask)
	router.POST("/tasks/:task_id/cancel", handler.CancelTask)
	router.DELETE("/tasks/:task_id", handler.DeleteTask)
}

// setupResultsAPI 设置结果 API 路由
func setupResultsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewResultsHandler(db, logger)
	router.GET("/results", handler.ListResults)
	router.GET("/results/:result_id", handler.GetResult)
	router.GET("/results/host/:host_id/score", handler.GetHostBaselineScore)
	router.GET("/results/host/:host_id/summary", handler.GetHostBaselineSummary)
}

// setupDashboardAPI 设置 Dashboard API 路由
func setupDashboardAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewDashboardHandler(db, logger)
	router.GET("/dashboard/stats", handler.GetDashboardStats)
}

// setupUsersAPI 设置用户管理 API 路由
func setupUsersAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewUsersHandler(db, logger)
	router.GET("/users", handler.ListUsers)
	router.GET("/users/:id", handler.GetUser)
	router.POST("/users", handler.CreateUser)
	router.PUT("/users/:id", handler.UpdateUser)
	router.DELETE("/users/:id", handler.DeleteUser)
}

// setupAssetsAPI 设置资产 API 路由
func setupAssetsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewAssetsHandler(db, logger)
	router.GET("/assets/processes", handler.ListProcesses)
	router.GET("/assets/ports", handler.ListPorts)
	router.GET("/assets/users", handler.ListUsers)
	router.GET("/assets/software", handler.ListSoftware)
	router.GET("/assets/containers", handler.ListContainers)
	router.GET("/assets/apps", handler.ListApps)
	router.GET("/assets/network-interfaces", handler.ListNetInterfaces)
	router.GET("/assets/volumes", handler.ListVolumes)
	router.GET("/assets/kmods", handler.ListKmods)
	router.GET("/assets/services", handler.ListServices)
	router.GET("/assets/crons", handler.ListCrons)
}

// setupReportsAPI 设置报表 API 路由
func setupReportsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewReportsHandler(db, logger)
	router.GET("/reports/stats", handler.GetStats)
	router.GET("/reports/baseline-score-trend", handler.GetBaselineScoreTrend)
	router.GET("/reports/check-result-trend", handler.GetCheckResultTrend)
}

// setupBusinessLinesAPI 设置业务线 API 路由
func setupBusinessLinesAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewBusinessLinesHandler(db, logger)
	router.GET("/business-lines", handler.ListBusinessLines)
	router.GET("/business-lines/:id", handler.GetBusinessLine)
	router.POST("/business-lines", handler.CreateBusinessLine)
	router.PUT("/business-lines/:id", handler.UpdateBusinessLine)
	router.DELETE("/business-lines/:id", handler.DeleteBusinessLine)
}

// setupSystemConfigAPI 设置系统配置 API 路由（需要认证）
func setupSystemConfigAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewSystemConfigHandler(db, logger, "./uploads", "/uploads")

	// Kubernetes 镜像配置
	router.GET("/system-config/kubernetes-image", handler.GetKubernetesImageConfig)
	router.PUT("/system-config/kubernetes-image", handler.UpdateKubernetesImageConfig)

	// 站点配置（更新和上传需要认证）
	router.PUT("/system-config/site", handler.UpdateSiteConfig)

	// Logo 上传
	router.POST("/system-config/upload-logo", handler.UploadLogo)
}

// setupNotificationsAPI 设置通知管理 API 路由
func setupNotificationsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewNotificationsHandler(db, logger)
	router.GET("/notifications", handler.ListNotifications)
	router.GET("/notifications/:id", handler.GetNotification)
	router.POST("/notifications", handler.CreateNotification)
	router.PUT("/notifications/:id", handler.UpdateNotification)
	router.DELETE("/notifications/:id", handler.DeleteNotification)
	router.POST("/notifications/test", handler.TestNotification)
}

// setupAlertsAPI 设置告警管理 API 路由
func setupAlertsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewAlertsHandler(db, logger)
	router.GET("/alerts", handler.ListAlerts)
	router.GET("/alerts/statistics", handler.GetAlertStatistics)
	router.GET("/alerts/:id", handler.GetAlert)
	router.POST("/alerts/:id/resolve", handler.ResolveAlert)
	router.POST("/alerts/:id/ignore", handler.IgnoreAlert)
}

// setupComponentsAPI 设置组件管理 API 路由
func setupComponentsAPI(router *gin.RouterGroup, db *gorm.DB, logger *zap.Logger) {
	handler := api.NewComponentsHandler(db, logger, "./uploads", "/uploads")

	// 组件管理
	router.GET("/components", handler.ListComponents)
	router.POST("/components", handler.CreateComponent)
	router.GET("/components/plugin-status", handler.GetPluginSyncStatus)
	router.GET("/components/:id", handler.GetComponent)
	router.DELETE("/components/:id", handler.DeleteComponent)

	// 版本管理
	router.GET("/components/:id/versions", handler.ListVersions)
	router.POST("/components/:id/versions", handler.ReleaseVersion)
	router.GET("/components/:id/versions/:version_id", handler.GetVersion)
	router.PUT("/components/:id/versions/:version_id/set-latest", handler.SetLatestVersion)
	router.DELETE("/components/:id/versions/:version_id", handler.DeleteVersion)

	// 包上传
	router.POST("/components/:id/versions/:version_id/packages", handler.UploadPackage)
	router.DELETE("/packages/:id", handler.DeletePackage)
}
