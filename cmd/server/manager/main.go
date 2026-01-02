// Package main 是 Manager HTTP API Server 主程序入口
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/router"
	"github.com/imkerbos/mxsec-platform/internal/server/manager/setup"
)

var (
	configPath = flag.String("config", "", "配置文件路径（默认：./configs/server.yaml）")
	version    = flag.Bool("version", false, "显示版本信息")
)

func main() {
	flag.Parse()

	if *version {
		printVersion()
		return
	}

	// 初始化所有服务组件
	services, err := setup.Initialize(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer services.Cleanup()

	// 设置路由
	httpRouter := router.Setup(services.DB, services.Logger, services.Config, services.ScoreCache, services.MetricsService)

	// 创建 HTTP Server
	server := &http.Server{
		Addr:         services.Config.Server.HTTP.Address(),
		Handler:      httpRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	services.Logger.Info("Manager HTTP API Server 启动成功", zap.String("address", services.Config.Server.HTTP.Address()))

	// 启动服务器（goroutine）
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			services.Logger.Fatal("HTTP Server 启动失败", zap.Error(err))
		}
	}()

	// 信号处理
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)

	services.Logger.Info("Manager HTTP API Server 运行中，等待关闭信号...")

	// 等待信号
	sig := <-signalCh
	services.Logger.Info("收到关闭信号", zap.String("signal", sig.String()))

	// 优雅关闭
	services.Logger.Info("正在关闭 Manager HTTP API Server...")
	// TODO: 实现优雅关闭（使用 context 和 server.Shutdown）

	services.Logger.Info("Manager HTTP API Server 已关闭")
}

func printVersion() {
	fmt.Println("mxsec-manager version dev")
	fmt.Println("Build time: unknown")
}
