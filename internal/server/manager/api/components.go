// Package api 提供 HTTP API 处理器
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// ComponentsHandler 组件管理 API 处理器
type ComponentsHandler struct {
	db        *gorm.DB
	logger    *zap.Logger
	uploadDir string // 上传文件存储目录
	urlPrefix string // 文件访问 URL 前缀
}

// NewComponentsHandler 创建组件管理处理器
func NewComponentsHandler(db *gorm.DB, logger *zap.Logger, uploadDir, urlPrefix string) *ComponentsHandler {
	// 确保上传目录存在
	packagesDir := filepath.Join(uploadDir, "packages")
	if err := os.MkdirAll(packagesDir, 0755); err != nil {
		logger.Error("创建组件包上传目录失败", zap.Error(err))
	}
	pluginsDir := filepath.Join(uploadDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		logger.Error("创建插件上传目录失败", zap.Error(err))
	}
	return &ComponentsHandler{
		db:        db,
		logger:    logger,
		uploadDir: uploadDir,
		urlPrefix: urlPrefix,
	}
}

// ==================== 组件管理 API ====================

// CreateComponentRequest 创建组件请求
type CreateComponentRequest struct {
	Name        string `json:"name" binding:"required"`        // 组件名称
	Category    string `json:"category" binding:"required"`    // 分类: agent, plugin
	Description string `json:"description"`                    // 描述
}

// CreateComponent 创建组件
// POST /api/v1/components
func (h *ComponentsHandler) CreateComponent(c *gin.Context) {
	var req CreateComponentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 验证组件名称（只允许字母、数字、下划线、横线）
	if !isValidComponentName(req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "组件名称只能包含字母、数字、下划线和横线",
		})
		return
	}

	// 验证分类
	if req.Category != "agent" && req.Category != "plugin" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的组件分类，支持: agent, plugin",
		})
		return
	}

	// 检查是否已存在
	var existingCount int64
	h.db.Model(&model.Component{}).Where("name = ?", req.Name).Count(&existingCount)
	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": fmt.Sprintf("组件 %s 已存在", req.Name),
		})
		return
	}

	// 获取当前用户
	username := h.getCurrentUser(c)

	// 创建组件
	component := model.Component{
		Name:        req.Name,
		Category:    model.ComponentCategory(req.Category),
		Description: req.Description,
		CreatedBy:   username,
	}

	if err := h.db.Create(&component).Error; err != nil {
		h.logger.Error("创建组件失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建失败",
		})
		return
	}

	h.logger.Info("创建组件成功",
		zap.String("name", req.Name),
		zap.String("category", req.Category),
		zap.String("created_by", username),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "创建成功",
		"data":    component,
	})
}

// ListComponents 获取组件列表
// GET /api/v1/components
func (h *ComponentsHandler) ListComponents(c *gin.Context) {
	var components []model.Component
	if err := h.db.Order("created_at DESC").Find(&components).Error; err != nil {
		h.logger.Error("查询组件列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	// 构建带统计信息的响应
	var result []gin.H
	for _, comp := range components {
		// 获取最新版本
		var latestVersion model.ComponentVersion
		h.db.Where("component_id = ? AND is_latest = ?", comp.ID, true).First(&latestVersion)

		// 获取版本数量
		var versionCount int64
		h.db.Model(&model.ComponentVersion{}).Where("component_id = ?", comp.ID).Count(&versionCount)

		// 获取包数量
		var packageCount int64
		h.db.Model(&model.ComponentPackage{}).
			Joins("JOIN component_versions ON component_versions.id = component_packages.version_id").
			Where("component_versions.component_id = ?", comp.ID).
			Count(&packageCount)

		result = append(result, gin.H{
			"id":             comp.ID,
			"name":           comp.Name,
			"category":       comp.Category,
			"description":    comp.Description,
			"created_by":     comp.CreatedBy,
			"created_at":     comp.CreatedAt,
			"latest_version": latestVersion.Version,
			"version_count":  versionCount,
			"package_count":  packageCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": result,
	})
}

// GetComponent 获取组件详情
// GET /api/v1/components/:id
func (h *ComponentsHandler) GetComponent(c *gin.Context) {
	id := c.Param("id")

	var component model.Component
	if err := h.db.First(&component, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "组件不存在",
			})
			return
		}
		h.logger.Error("查询组件详情失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": component,
	})
}

// DeleteComponent 删除组件
// DELETE /api/v1/components/:id
func (h *ComponentsHandler) DeleteComponent(c *gin.Context) {
	id := c.Param("id")

	var component model.Component
	if err := h.db.First(&component, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "组件不存在",
			})
			return
		}
		h.logger.Error("查询组件失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	// 检查是否有版本
	var versionCount int64
	h.db.Model(&model.ComponentVersion{}).Where("component_id = ?", component.ID).Count(&versionCount)
	if versionCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("组件下有 %d 个版本，请先删除所有版本", versionCount),
		})
		return
	}

	// 删除组件
	if err := h.db.Delete(&component).Error; err != nil {
		h.logger.Error("删除组件失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	h.logger.Info("删除组件成功",
		zap.Uint("id", component.ID),
		zap.String("name", component.Name),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// ==================== 版本管理 API ====================

// ReleaseVersionRequest 发布版本请求
type ReleaseVersionRequest struct {
	Version   string `json:"version" binding:"required"` // 版本号
	Changelog string `json:"changelog"`                  // 更新日志
	SetLatest bool   `json:"set_latest"`                 // 是否设为最新版本
}

// ReleaseVersion 发布新版本（仅创建版本记录，包文件单独上传）
// POST /api/v1/components/:id/versions
func (h *ComponentsHandler) ReleaseVersion(c *gin.Context) {
	componentID := c.Param("id")

	var req ReleaseVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 验证版本号格式
	if !isValidVersion(req.Version) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "版本号格式不正确，例如: 1.0.0 或 1.8.5.31",
		})
		return
	}

	// 查找组件
	var component model.Component
	if err := h.db.First(&component, componentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "组件不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	// 检查版本是否已存在
	var existingCount int64
	h.db.Model(&model.ComponentVersion{}).
		Where("component_id = ? AND version = ?", component.ID, req.Version).
		Count(&existingCount)
	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": fmt.Sprintf("版本 %s 已存在", req.Version),
		})
		return
	}

	// 获取当前用户
	username := h.getCurrentUser(c)

	// 开始事务
	tx := h.db.Begin()

	// 如果设为最新版本，先将其他版本的 is_latest 设为 false
	if req.SetLatest {
		if err := tx.Model(&model.ComponentVersion{}).
			Where("component_id = ?", component.ID).
			Update("is_latest", false).Error; err != nil {
			tx.Rollback()
			h.logger.Error("更新最新版本状态失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "发布失败",
			})
			return
		}
	}

	// 创建版本
	version := model.ComponentVersion{
		ComponentID: component.ID,
		Version:     req.Version,
		Changelog:   req.Changelog,
		IsLatest:    req.SetLatest,
		CreatedBy:   username,
	}

	if err := tx.Create(&version).Error; err != nil {
		tx.Rollback()
		h.logger.Error("创建版本失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "发布失败",
		})
		return
	}

	tx.Commit()

	h.logger.Info("发布版本成功",
		zap.String("component", component.Name),
		zap.String("version", req.Version),
		zap.String("created_by", username),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "发布成功",
		"data":    version,
	})
}

// ListVersions 获取组件的版本列表
// GET /api/v1/components/:id/versions
func (h *ComponentsHandler) ListVersions(c *gin.Context) {
	componentID := c.Param("id")

	// 验证组件存在
	var component model.Component
	if err := h.db.First(&component, componentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "组件不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	// 查询版本列表
	var versions []model.ComponentVersion
	if err := h.db.Where("component_id = ?", component.ID).
		Order("created_at DESC").
		Find(&versions).Error; err != nil {
		h.logger.Error("查询版本列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	// 构建带包信息的响应
	var result []gin.H
	for _, ver := range versions {
		// 获取该版本的所有包
		var packages []model.ComponentPackage
		h.db.Where("version_id = ?", ver.ID).Find(&packages)

		// 构建包摘要
		packagesSummary := make([]gin.H, 0)
		for _, pkg := range packages {
			packagesSummary = append(packagesSummary, gin.H{
				"id":        pkg.ID,
				"arch":      pkg.Arch,
				"pkg_type":  pkg.PkgType,
				"file_size": pkg.FileSize,
				"sha256":    pkg.SHA256,
			})
		}

		result = append(result, gin.H{
			"id":               ver.ID,
			"version":          ver.Version,
			"changelog":        ver.Changelog,
			"is_latest":        ver.IsLatest,
			"created_by":       ver.CreatedBy,
			"created_at":       ver.CreatedAt,
			"packages":         packagesSummary,
			"packages_summary": buildPackagesSummary(packages),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"component": component,
			"versions":  result,
		},
	})
}

// GetVersion 获取版本详情
// GET /api/v1/components/:id/versions/:version_id
func (h *ComponentsHandler) GetVersion(c *gin.Context) {
	versionID := c.Param("version_id")

	var version model.ComponentVersion
	if err := h.db.Preload("Packages").First(&version, versionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "版本不存在",
			})
			return
		}
		h.logger.Error("查询版本详情失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": version,
	})
}

// SetLatestVersion 设置为最新版本
// PUT /api/v1/components/:id/versions/:version_id/set-latest
func (h *ComponentsHandler) SetLatestVersion(c *gin.Context) {
	componentID := c.Param("id")
	versionID := c.Param("version_id")

	var version model.ComponentVersion
	if err := h.db.First(&version, versionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "版本不存在",
			})
			return
		}
		h.logger.Error("查询版本失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "设置失败",
		})
		return
	}

	// 使用事务更新
	tx := h.db.Begin()

	// 将同组件的其他版本设为非最新
	if err := tx.Model(&model.ComponentVersion{}).
		Where("component_id = ?", componentID).
		Update("is_latest", false).Error; err != nil {
		tx.Rollback()
		h.logger.Error("更新最新版本状态失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "设置失败",
		})
		return
	}

	// 将当前版本设为最新
	if err := tx.Model(&version).Update("is_latest", true).Error; err != nil {
		tx.Rollback()
		h.logger.Error("设置最新版本失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "设置失败",
		})
		return
	}

	tx.Commit()

	// 同步更新插件配置（如果是插件）
	var component model.Component
	if err := h.db.First(&component, componentID).Error; err == nil {
		if component.Category == model.ComponentCategoryPlugin {
			h.syncPluginConfigForVersion(&version, component.Name)
		}
	}

	h.logger.Info("设置最新版本成功",
		zap.Uint("version_id", version.ID),
		zap.String("version", version.Version),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "设置成功",
	})
}

// DeleteVersion 删除版本
// DELETE /api/v1/components/:id/versions/:version_id
func (h *ComponentsHandler) DeleteVersion(c *gin.Context) {
	versionID := c.Param("version_id")

	var version model.ComponentVersion
	if err := h.db.First(&version, versionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "版本不存在",
			})
			return
		}
		h.logger.Error("查询版本失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	// 删除该版本的所有包文件
	var packages []model.ComponentPackage
	h.db.Where("version_id = ?", version.ID).Find(&packages)
	for _, pkg := range packages {
		if err := os.Remove(pkg.FilePath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn("删除包文件失败", zap.Error(err), zap.String("path", pkg.FilePath))
		}
	}

	// 删除数据库中的包记录
	h.db.Where("version_id = ?", version.ID).Delete(&model.ComponentPackage{})

	// 删除版本记录
	if err := h.db.Delete(&version).Error; err != nil {
		h.logger.Error("删除版本记录失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	h.logger.Info("删除版本成功",
		zap.Uint("id", version.ID),
		zap.String("version", version.Version),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// ==================== 包上传 API ====================

// UploadPackage 上传包文件到指定版本
// POST /api/v1/components/:id/versions/:version_id/packages
func (h *ComponentsHandler) UploadPackage(c *gin.Context) {
	componentID := c.Param("id")
	versionID := c.Param("version_id")

	// 获取表单参数
	pkgType := c.PostForm("pkg_type") // rpm, deb, binary
	arch := c.PostForm("arch")        // amd64, arm64

	// 验证参数
	if arch != "amd64" && arch != "arm64" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的架构，支持: amd64, arm64",
		})
		return
	}

	if pkgType != "rpm" && pkgType != "deb" && pkgType != "binary" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的包类型，支持: rpm, deb, binary",
		})
		return
	}

	// 查找组件
	var component model.Component
	if err := h.db.First(&component, componentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "组件不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	// Agent 必须是 rpm/deb，插件可以是 binary
	if component.Category == model.ComponentCategoryAgent && pkgType == "binary" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Agent 包类型必须是 rpm 或 deb",
		})
		return
	}

	// 查找版本
	var version model.ComponentVersion
	if err := h.db.First(&version, versionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "版本不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	// 检查是否已存在相同类型的包
	var existingCount int64
	h.db.Model(&model.ComponentPackage{}).
		Where("version_id = ? AND pkg_type = ? AND arch = ?", version.ID, pkgType, arch).
		Count(&existingCount)
	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": fmt.Sprintf("该版本已存在 %s/%s 包", pkgType, arch),
		})
		return
	}

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请上传文件",
		})
		return
	}
	defer file.Close()

	// 验证文件扩展名
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if pkgType == "rpm" && ext != ".rpm" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "RPM 包文件扩展名必须是 .rpm",
		})
		return
	}
	if pkgType == "deb" && ext != ".deb" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "DEB 包文件扩展名必须是 .deb",
		})
		return
	}

	// 生成存储路径
	var filePath, fileName string
	if pkgType == "binary" {
		// 二进制插件文件存储到 uploads/plugins/{component}/{version}/ 目录
		pluginsDir := filepath.Join(h.uploadDir, "plugins", component.Name, version.Version)
		if err := os.MkdirAll(pluginsDir, 0755); err != nil {
			h.logger.Error("创建插件目录失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "保存文件失败",
			})
			return
		}
		// 文件名格式：{name}_{arch}（例如 baseline_amd64）
		fileName = fmt.Sprintf("%s_%s", component.Name, arch)
		filePath = filepath.Join(pluginsDir, fileName)
	} else {
		// RPM/DEB 包存储到 uploads/packages/{component}/{version}/ 目录
		packagesDir := filepath.Join(h.uploadDir, "packages", component.Name, version.Version)
		if err := os.MkdirAll(packagesDir, 0755); err != nil {
			h.logger.Error("创建组件包目录失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "保存文件失败",
			})
			return
		}
		fileName = fmt.Sprintf("mxsec-%s-%s-linux-%s.%s", component.Name, version.Version, arch, pkgType)
		filePath = filepath.Join(packagesDir, fileName)
	}

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		h.logger.Error("创建文件失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存文件失败",
		})
		return
	}
	defer dst.Close()

	// 同时计算 SHA256 和写入文件
	hasher := sha256.New()
	writer := io.MultiWriter(dst, hasher)
	fileSize, err := io.Copy(writer, file)
	if err != nil {
		os.Remove(filePath)
		h.logger.Error("写入文件失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存文件失败",
		})
		return
	}

	sha256Sum := hex.EncodeToString(hasher.Sum(nil))

	// 获取当前用户
	username := h.getCurrentUser(c)

	// 创建数据库记录
	pkg := model.ComponentPackage{
		VersionID:  version.ID,
		OS:         "linux",
		Arch:       arch,
		PkgType:    model.PackageType(pkgType),
		FilePath:   filePath,
		FileName:   header.Filename,
		FileSize:   fileSize,
		SHA256:     sha256Sum,
		Enabled:    true,
		UploadedBy: username,
		UploadedAt: model.Now(),
	}

	if err := h.db.Create(&pkg).Error; err != nil {
		os.Remove(filePath)
		h.logger.Error("创建包记录失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存失败",
		})
		return
	}

	h.logger.Info("上传包成功",
		zap.String("component", component.Name),
		zap.String("version", version.Version),
		zap.String("pkg_type", pkgType),
		zap.String("arch", arch),
		zap.Int64("file_size", fileSize),
	)

	// 如果是插件且该版本是最新版本，同步更新插件配置
	if component.Category == model.ComponentCategoryPlugin && version.IsLatest {
		h.syncPluginConfigForVersion(&version, component.Name)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "上传成功",
		"data":    pkg,
	})
}

// DeletePackage 删除包
// DELETE /api/v1/packages/:id
func (h *ComponentsHandler) DeletePackage(c *gin.Context) {
	id := c.Param("id")

	var pkg model.ComponentPackage
	if err := h.db.First(&pkg, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "包不存在",
			})
			return
		}
		h.logger.Error("查询包失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	// 删除文件
	if err := os.Remove(pkg.FilePath); err != nil && !os.IsNotExist(err) {
		h.logger.Warn("删除包文件失败", zap.Error(err), zap.String("path", pkg.FilePath))
	}

	// 删除数据库记录
	if err := h.db.Delete(&pkg).Error; err != nil {
		h.logger.Error("删除包记录失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	h.logger.Info("删除包成功", zap.Uint("id", pkg.ID))

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// ==================== 下载 API ====================

// DownloadAgentPackage 下载 Agent 安装包 (无需认证)
// GET /api/v1/agent/download/:pkg_type/:arch
func (h *ComponentsHandler) DownloadAgentPackage(c *gin.Context) {
	pkgType := c.Param("pkg_type")
	arch := c.Param("arch")

	// 验证参数
	if pkgType != "rpm" && pkgType != "deb" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的包类型，支持: rpm, deb",
		})
		return
	}

	if arch != "amd64" && arch != "arm64" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的架构，支持: amd64, arm64",
		})
		return
	}

	// 查找 agent 组件
	var component model.Component
	if err := h.db.Where("name = ?", "agent").First(&component).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent 组件不存在",
		})
		return
	}

	// 查找最新版本
	var latestVersion model.ComponentVersion
	if err := h.db.Where("component_id = ? AND is_latest = ?", component.ID, true).First(&latestVersion).Error; err != nil {
		// 如果没有标记为最新的，尝试获取最新上传的
		if err := h.db.Where("component_id = ?", component.ID).
			Order("created_at DESC").First(&latestVersion).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "未找到 Agent 版本",
			})
			return
		}
	}

	// 查找对应的包
	var pkg model.ComponentPackage
	if err := h.db.Where("version_id = ? AND pkg_type = ? AND arch = ? AND enabled = ?",
		latestVersion.ID, pkgType, arch, true).First(&pkg).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": fmt.Sprintf("未找到 Agent %s 包 (%s)", pkgType, arch),
		})
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(pkg.FilePath); os.IsNotExist(err) {
		h.logger.Error("Agent 包文件不存在", zap.String("path", pkg.FilePath))
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "文件不存在",
		})
		return
	}

	// 设置下载响应头
	fileName := fmt.Sprintf("mxsec-agent-%s.%s", latestVersion.Version, pkgType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(pkg.FileSize, 10))
	c.Header("X-Package-Version", latestVersion.Version)
	c.Header("X-Package-SHA256", pkg.SHA256)

	// 发送文件
	c.File(pkg.FilePath)

	h.logger.Info("Agent 包下载",
		zap.String("version", latestVersion.Version),
		zap.String("pkg_type", pkgType),
		zap.String("arch", arch),
		zap.String("client_ip", c.ClientIP()),
	)
}

// DownloadPluginPackage 下载插件包 (供 Agent 调用)
// GET /api/v1/plugins/download/:name
func (h *ComponentsHandler) DownloadPluginPackage(c *gin.Context) {
	name := c.Param("name")
	arch := c.DefaultQuery("arch", "amd64")

	// 验证架构
	if arch != "amd64" && arch != "arm64" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的架构，支持: amd64, arm64",
		})
		return
	}

	// 查找插件组件
	var component model.Component
	if err := h.db.Where("name = ? AND category = ?", name, model.ComponentCategoryPlugin).First(&component).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": fmt.Sprintf("插件 %s 不存在", name),
		})
		return
	}

	// 查找最新版本
	var latestVersion model.ComponentVersion
	if err := h.db.Where("component_id = ? AND is_latest = ?", component.ID, true).First(&latestVersion).Error; err != nil {
		if err := h.db.Where("component_id = ?", component.ID).
			Order("created_at DESC").First(&latestVersion).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": fmt.Sprintf("插件 %s 没有可用版本", name),
			})
			return
		}
	}

	// 查找对应架构的二进制包
	var pkg model.ComponentPackage
	if err := h.db.Where("version_id = ? AND pkg_type = ? AND arch = ? AND enabled = ?",
		latestVersion.ID, "binary", arch, true).First(&pkg).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": fmt.Sprintf("插件 %s 没有 %s 架构的包", name, arch),
		})
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(pkg.FilePath); os.IsNotExist(err) {
		h.logger.Error("插件包文件不存在", zap.String("path", pkg.FilePath))
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "文件不存在",
		})
		return
	}

	// 设置下载响应头 - 文件名使用插件名（Agent 下载后可直接使用）
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", name))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(pkg.FileSize, 10))
	c.Header("X-Plugin-Name", name)
	c.Header("X-Plugin-Version", latestVersion.Version)
	c.Header("X-Plugin-SHA256", pkg.SHA256)

	// 发送文件
	c.File(pkg.FilePath)

	h.logger.Info("插件包下载",
		zap.String("name", name),
		zap.String("version", latestVersion.Version),
		zap.String("arch", arch),
		zap.String("client_ip", c.ClientIP()),
	)
}

// ==================== 插件状态 API ====================

// GetPluginSyncStatus 获取插件同步状态
// GET /api/v1/components/plugin-status
func (h *ComponentsHandler) GetPluginSyncStatus(c *gin.Context) {
	type PluginStatus struct {
		Name           string   `json:"name"`
		Type           string   `json:"type"`
		ConfigVersion  string   `json:"config_version"`
		ConfigSHA256   string   `json:"config_sha256"`
		ConfigEnabled  bool     `json:"config_enabled"`
		DownloadURLs   []string `json:"download_urls"`
		Description    string   `json:"description"`
		HasPackage     bool     `json:"has_package"`
		PackageVersion string   `json:"package_version,omitempty"`
		PackageArch    string   `json:"package_arch,omitempty"`
		Status         string   `json:"status"`
	}

	var statuses []PluginStatus

	// 查询所有插件配置
	var pluginConfigs []model.PluginConfig
	if err := h.db.Find(&pluginConfigs).Error; err != nil {
		h.logger.Error("查询插件配置失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	for _, pc := range pluginConfigs {
		status := PluginStatus{
			Name:          pc.Name,
			Type:          string(pc.Type),
			ConfigVersion: pc.Version,
			ConfigSHA256:  pc.SHA256,
			ConfigEnabled: pc.Enabled,
			DownloadURLs:  []string(pc.DownloadURLs),
			Description:   pc.Description,
			Status:        "missing_package",
		}

		// 查找对应的组件
		var component model.Component
		if err := h.db.Where("name = ?", pc.Name).First(&component).Error; err == nil {
			// 查找最新版本
			var latestVersion model.ComponentVersion
			if err := h.db.Where("component_id = ? AND is_latest = ?", component.ID, true).First(&latestVersion).Error; err == nil {
				// 查找包
				var packages []model.ComponentPackage
				h.db.Where("version_id = ? AND enabled = ?", latestVersion.ID, true).Find(&packages)

				if len(packages) > 0 {
					status.HasPackage = true
					status.PackageVersion = latestVersion.Version

					// 拼接架构信息
					archs := make([]string, len(packages))
					for i, pkg := range packages {
						archs[i] = pkg.Arch
					}
					status.PackageArch = strings.Join(archs, ", ")

					// 检查版本是否匹配
					if latestVersion.Version == pc.Version {
						status.Status = "ready"
					} else {
						status.Status = "outdated"
					}
				}
			}
		}

		// 检查是否是默认配置
		if status.Status == "missing_package" && len(pc.DownloadURLs) > 0 && strings.HasPrefix(pc.DownloadURLs[0], "file://") {
			status.Status = "default_config"
		}

		statuses = append(statuses, status)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": statuses,
	})
}

// ==================== 辅助函数 ====================

// getCurrentUser 获取当前用户
func (h *ComponentsHandler) getCurrentUser(c *gin.Context) string {
	if username, exists := c.Get("username"); exists {
		return fmt.Sprintf("%v", username)
	}
	if userID, exists := c.Get("user_id"); exists {
		return fmt.Sprintf("%v", userID)
	}
	return "admin"
}

// isValidComponentName 验证组件名称
func isValidComponentName(name string) bool {
	if len(name) == 0 || len(name) > 32 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// isValidVersion 验证版本号格式
func isValidVersion(version string) bool {
	if len(version) == 0 || len(version) > 32 {
		return false
	}
	// 支持格式：1.0.0, 1.0.0-beta, 1.8.5.31 等
	parts := strings.Split(version, "-")
	mainPart := parts[0]

	// 检查主版本号部分
	segments := strings.Split(mainPart, ".")
	if len(segments) < 2 || len(segments) > 4 {
		return false
	}
	for _, seg := range segments {
		if len(seg) == 0 {
			return false
		}
		for _, c := range seg {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// buildPackagesSummary 构建包摘要
func buildPackagesSummary(packages []model.ComponentPackage) string {
	if len(packages) == 0 {
		return "无"
	}

	var parts []string
	for _, pkg := range packages {
		parts = append(parts, fmt.Sprintf("%s/%s", pkg.Arch, pkg.PkgType))
	}
	return strings.Join(parts, ", ")
}

// syncPluginConfigForVersion 同步插件配置
func (h *ComponentsHandler) syncPluginConfigForVersion(version *model.ComponentVersion, componentName string) {
	// 获取该版本的包（优先 amd64）
	var pkg model.ComponentPackage
	if err := h.db.Where("version_id = ? AND arch = ?", version.ID, "amd64").First(&pkg).Error; err != nil {
		// 如果没有 amd64，取任意一个
		if err := h.db.Where("version_id = ?", version.ID).First(&pkg).Error; err != nil {
			h.logger.Warn("同步插件配置失败：没有找到包", zap.String("name", componentName))
			return
		}
	}

	// 确定插件类型
	var pluginType model.PluginType
	switch componentName {
	case "baseline":
		pluginType = model.PluginTypeBaseline
	case "collector":
		pluginType = model.PluginTypeCollector
	default:
		pluginType = model.PluginType(componentName)
	}

	// 构建下载 URL
	downloadURL := fmt.Sprintf("/api/v1/plugins/download/%s", componentName)

	// 查找或创建插件配置
	var pluginConfig model.PluginConfig
	err := h.db.Where("name = ?", componentName).First(&pluginConfig).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新的插件配置
		pluginConfig = model.PluginConfig{
			Name:    componentName,
			Type:    pluginType,
			Version: version.Version,
			SHA256:  pkg.SHA256,
			DownloadURLs: model.StringArray{
				downloadURL,
			},
			Detail:      fmt.Sprintf(`{"updated_at": "%s"}`, time.Now().Format(time.RFC3339)),
			Enabled:     true,
			Description: fmt.Sprintf("%s 插件 v%s", componentName, version.Version),
		}
		if err := h.db.Create(&pluginConfig).Error; err != nil {
			h.logger.Error("创建插件配置失败", zap.Error(err))
			return
		}
	} else if err == nil {
		// 更新已存在的插件配置
		updates := map[string]interface{}{
			"version":       version.Version,
			"sha256":        pkg.SHA256,
			"download_urls": model.StringArray{downloadURL},
			"detail":        fmt.Sprintf(`{"updated_at": "%s"}`, time.Now().Format(time.RFC3339)),
			"description":   fmt.Sprintf("%s 插件 v%s", componentName, version.Version),
		}
		if err := h.db.Model(&pluginConfig).Updates(updates).Error; err != nil {
			h.logger.Error("更新插件配置失败", zap.Error(err))
			return
		}
	}

	h.logger.Info("同步插件配置成功",
		zap.String("name", componentName),
		zap.String("version", version.Version),
	)
}
