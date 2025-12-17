// Package server 提供 gRPC Server 创建和配置
package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/config"
)

// CreateGRPCServer 创建并配置 gRPC Server
func CreateGRPCServer(cfg *config.Config, logger *zap.Logger) (*grpc.Server, error) {
	var opts []grpc.ServerOption

	// 配置 mTLS（如果提供了证书）
	if cfg.MTLS.ServerCert != "" && cfg.MTLS.ServerKey != "" {
		// 加载服务器证书和密钥
		cert, err := tls.LoadX509KeyPair(cfg.MTLS.ServerCert, cfg.MTLS.ServerKey)
		if err != nil {
			return nil, fmt.Errorf("加载服务器证书失败: %w", err)
		}

		// 创建 TLS 配置
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		// 如果提供了 CA 证书，配置客户端证书验证（mTLS）
		if cfg.MTLS.CACert != "" {
			caCert, err := os.ReadFile(cfg.MTLS.CACert)
			if err != nil {
				return nil, fmt.Errorf("读取 CA 证书失败: %w", err)
			}

			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("解析 CA 证书失败")
			}

			tlsConfig.ClientCAs = caCertPool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert // 要求并验证客户端证书
			logger.Info("已启用 mTLS（客户端证书验证）",
				zap.String("cert", cfg.MTLS.ServerCert),
				zap.String("ca_cert", cfg.MTLS.CACert),
			)
		} else {
			logger.Warn("未配置 CA 证书，仅启用服务器 TLS（不验证客户端证书）")
		}

		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.Creds(creds))
	} else {
		logger.Warn("未配置 mTLS 证书，使用不安全连接（仅用于开发）")
	}

	return grpc.NewServer(opts...), nil
}
