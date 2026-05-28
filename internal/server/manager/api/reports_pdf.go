// Package api 提供 HTTP API 处理器。
//
// reports_pdf.go 提供报告 PDF 导出 endpoint。
// 流程: client 触发 → manager 签 token → Gotenberg 拉 /reports/print/:type?token=...
//
//	→ Chromium 渲染 → 返回 PDF stream → client 下载。
//
// 优势:
//   - 矢量文本可搜索可复制 (合规)
//   - 大数据集渲染不卡 (浏览器 jsPDF 万行级会崩)
//   - 可被 cron / scheduler 后台调用
package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
)

// ReportPDFHandler 处理报告 PDF 导出。
type ReportPDFHandler struct {
	pdfService  *biz.PDFService
	internalURL string // manager 内部 URL 给 Gotenberg 拉，如 http://manager:8080
	jwtSecret   []byte
	logger      *zap.Logger
}

// NewReportPDFHandler 创建处理器。
//
// gotenbergURL: 如 http://gotenberg:3000 (sidecar)
// internalURL:  Gotenberg 容器访问 manager 的内部地址 (如 http://manager:8080)
// jwtSecret:    用于签短期 token 让打印页面免登录
func NewReportPDFHandler(gotenbergURL, internalURL string, jwtSecret []byte, logger *zap.Logger) *ReportPDFHandler {
	return &ReportPDFHandler{
		pdfService:  biz.NewPDFService(gotenbergURL, logger),
		internalURL: internalURL,
		jwtSecret:   jwtSecret,
		logger:      logger,
	}
}

// ExportEDRReportPDF GET /api/v1/reports/edr/pdf?start_time=&end_time=&landscape=
//
// 流程:
//  1. 当前 user 的 username/role 签一个 60s short-lived token
//  2. 调 Gotenberg POST /forms/chromium/convert/url
//     url = ${internalURL}/reports/print/edr?token=${token}&start_time=...&end_time=...
//  3. 把 PDF stream 转发给 client，并设 Content-Disposition 触发下载
func (h *ReportPDFHandler) ExportEDRReportPDF(c *gin.Context) {
	if !h.pdfService.HasGotenberg() {
		BadRequest(c, "PDF 服务未配置 (Gotenberg sidecar 未部署)")
		return
	}
	startTime := c.Query("start_time")
	endTime := c.Query("end_time")
	landscape := c.Query("landscape") == "true"

	username := c.GetString("username")
	if username == "" {
		username = "admin"
	}
	role := c.GetString("role")
	if role == "" {
		role = "admin"
	}

	// 1. 签 60s token 给打印页用
	token, err := h.signPrintToken(username, role, 60*time.Second)
	if err != nil {
		InternalError(c, "签 PDF token 失败")
		return
	}

	// 2. 构造打印页 URL（含 token 与时间范围）
	printURL := fmt.Sprintf("%s/reports/print/edr?token=%s&start_time=%s&end_time=%s",
		h.internalURL, token, startTime, endTime)
	opts := biz.DefaultPDFOptions(printURL)
	opts.Landscape = landscape

	// 3. 调 Gotenberg 渲染
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	pdf, err := h.pdfService.RenderURL(ctx, opts)
	if err != nil {
		h.logger.Error("EDR 报告 PDF 渲染失败", zap.Error(err))
		InternalError(c, fmt.Sprintf("PDF 渲染失败: %s", err.Error()))
		return
	}

	// 4. 流式返回
	filename := fmt.Sprintf("EDR-Report-%s.pdf", time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Length", fmt.Sprintf("%d", len(pdf)))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(pdf)
}

// signPrintToken 签短期 JWT 给打印页面免认证使用。
// 与 manager 主 JWT 同 secret 但 claim 不一样，方便审计区分。
func (h *ReportPDFHandler) signPrintToken(username, role string, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"username": username,
		"role":     role,
		"iss":      "mxsec-platform",
		"sub":      "print",
		"exp":      time.Now().Add(ttl).Unix(),
		"iat":      time.Now().Unix(),
	}
	tk := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tk.SignedString(h.jwtSecret)
}
