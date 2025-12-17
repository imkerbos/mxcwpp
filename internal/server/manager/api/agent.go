// Package api 提供 HTTP API 处理器
package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AgentHandler 是 Agent 安装脚本 API 处理器
type AgentHandler struct {
	logger      *zap.Logger
	serverHost  string // AgentCenter gRPC 地址（例如：10.0.0.1:6751）
	httpAddress string // Manager HTTP 地址（例如：10.0.0.1:8080）
}

// NewAgentHandler 创建 Agent 安装脚本处理器
func NewAgentHandler(logger *zap.Logger, serverHost, httpAddress string) *AgentHandler {
	return &AgentHandler{
		logger:      logger,
		serverHost:  serverHost,
		httpAddress: httpAddress,
	}
}

// InstallScript 返回 Linux 安装脚本
// GET /agent/install.sh
func (h *AgentHandler) InstallScript(c *gin.Context) {
	// 读取安装脚本
	scriptContent, err := h.readInstallScript()
	if err != nil {
		h.logger.Error("读取安装脚本失败", zap.Error(err))
		c.String(http.StatusInternalServerError, "Failed to read install script")
		return
	}

	// 从请求中获取 HTTP 服务器地址（优先使用请求的 Host，否则使用配置的地址）
	httpHost := c.GetHeader("Host")
	if httpHost == "" {
		// 如果 Host 头为空，尝试从 X-Forwarded-Host 获取（代理场景）
		httpHost = c.GetHeader("X-Forwarded-Host")
	}
	if httpHost == "" {
		// 如果还是为空，使用配置的地址
		httpHost = h.httpAddress
		// 如果配置的地址是 localhost，尝试从请求中获取实际地址
		if strings.Contains(httpHost, "localhost") || strings.Contains(httpHost, "127.0.0.1") {
			// 从请求的 RemoteAddr 或 X-Forwarded-For 获取客户端 IP
			// 但这里我们需要的是服务器地址，不是客户端地址
			// 所以如果配置是 localhost，我们保持使用 Host 头（如果存在）
			// 否则使用配置的地址（可能是开发环境）
		}
	}
	// 确保有协议前缀
	if !strings.HasPrefix(httpHost, "http://") && !strings.HasPrefix(httpHost, "https://") {
		// 根据请求协议决定使用 http 还是 https
		scheme := "http"
		if c.GetHeader("X-Forwarded-Proto") == "https" || c.Request.TLS != nil {
			scheme = "https"
		}
		httpHost = scheme + "://" + httpHost
	}
	// 提取 host:port 部分（去掉协议）
	httpHostOnly := httpHost
	if strings.HasPrefix(httpHostOnly, "http://") {
		httpHostOnly = strings.TrimPrefix(httpHostOnly, "http://")
	} else if strings.HasPrefix(httpHostOnly, "https://") {
		httpHostOnly = strings.TrimPrefix(httpHostOnly, "https://")
	}

	// 替换脚本中的占位符
	// 将 SERVER_HOST 替换为实际的 gRPC Server 地址（用于 Agent 连接）
	scriptContent = strings.ReplaceAll(scriptContent, "${BLS_SERVER_HOST:-localhost:6751}", h.serverHost)
	// 将下载 URL 中的 SERVER_HOST 替换为实际的 HTTP 地址（用于下载安装包）
	scriptContent = strings.ReplaceAll(scriptContent, "http://${SERVER_HOST}/api/v1/agent/download", fmt.Sprintf("%s/api/v1/agent/download", httpHost))

	// 设置响应头
	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Content-Disposition", "inline; filename=install.sh")
	c.String(http.StatusOK, scriptContent)
}

// UninstallScript 返回 Linux 卸载脚本
// GET /agent/uninstall.sh
func (h *AgentHandler) UninstallScript(c *gin.Context) {
	// 读取卸载脚本
	scriptContent, err := h.readUninstallScript()
	if err != nil {
		h.logger.Error("读取卸载脚本失败", zap.Error(err))
		c.String(http.StatusInternalServerError, "Failed to read uninstall script")
		return
	}

	// 设置响应头
	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Content-Disposition", "inline; filename=uninstall.sh")
	c.String(http.StatusOK, scriptContent)
}

// readInstallScript 读取安装脚本内容
func (h *AgentHandler) readInstallScript() (string, error) {
	// 尝试从文件系统读取（相对于工作目录或可执行文件目录）
	possiblePaths := []string{
		"scripts/install.sh",
		"./scripts/install.sh",
		filepath.Join(filepath.Dir(os.Args[0]), "scripts/install.sh"),
	}

	for _, path := range possiblePaths {
		if data, err := os.ReadFile(path); err == nil {
			h.logger.Debug("成功读取安装脚本", zap.String("path", path))
			return string(data), nil
		}
	}

	// 如果都失败，返回默认脚本
	h.logger.Warn("无法从文件系统读取安装脚本，使用默认脚本")
	return h.getDefaultInstallScript(), nil
}

// readUninstallScript 读取卸载脚本内容
func (h *AgentHandler) readUninstallScript() (string, error) {
	// 尝试从文件系统读取
	possiblePaths := []string{
		"scripts/uninstall.sh",
		"./scripts/uninstall.sh",
		filepath.Join(filepath.Dir(os.Args[0]), "scripts/uninstall.sh"),
	}

	for _, path := range possiblePaths {
		if data, err := os.ReadFile(path); err == nil {
			h.logger.Debug("成功读取卸载脚本", zap.String("path", path))
			return string(data), nil
		}
	}

	// 如果文件不存在，返回默认卸载脚本
	h.logger.Warn("无法从文件系统读取卸载脚本，使用默认脚本")
	return h.getDefaultUninstallScript(), nil
}

// getDefaultInstallScript 返回默认安装脚本（如果文件读取失败时的后备方案）
func (h *AgentHandler) getDefaultInstallScript() string {
	return `#!/bin/bash
# Matrix Cloud Security Platform Agent 一键安装脚本
# 使用方法: curl -sS http://SERVER_IP:8080/agent/install.sh | bash

set -e

echo "Matrix Cloud Security Platform Agent Installer"
echo "Please ensure install.sh is properly configured."
`
}

// getDefaultUninstallScript 返回默认卸载脚本
func (h *AgentHandler) getDefaultUninstallScript() string {
	return `#!/bin/bash
# Matrix Cloud Security Platform Agent 卸载脚本
# 使用方法: curl -sS http://SERVER_IP:8080/agent/uninstall.sh | bash

set -e

echo "Matrix Cloud Security Platform Agent Uninstaller"
echo "Please ensure uninstall.sh is properly configured."
`
}
