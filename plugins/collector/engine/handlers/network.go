// Package handlers 提供各类资产采集器的实现
package handlers

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"

	"github.com/imkerbos/mxsec-platform/plugins/collector/engine"
)

// NetInterfaceHandler 是网络接口采集器
type NetInterfaceHandler struct {
	Logger *zap.Logger
}

// Collect 采集网络接口信息
func (h *NetInterfaceHandler) Collect(ctx context.Context) ([]interface{}, error) {
	var interfaces []interface{}

	// 获取所有网络接口
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range ifaces {
		select {
		case <-ctx.Done():
			return interfaces, ctx.Err()
		default:
		}

		// 跳过回环接口
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 获取接口地址
		addrs, err := iface.Addrs()
		if err != nil {
			h.Logger.Debug("failed to get addresses for interface",
				zap.String("interface", iface.Name),
				zap.Error(err))
			continue
		}

		var ipv4Addrs []string
		var ipv6Addrs []string

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			if ip.To4() != nil {
				ipv4Addrs = append(ipv4Addrs, ip.String())
			} else {
				ipv6Addrs = append(ipv6Addrs, ip.String())
			}
		}

		// 获取 MTU
		mtu := iface.MTU

		// 获取状态
		state := "down"
		if iface.Flags&net.FlagUp != 0 {
			state = "up"
		}

		netInterface := &engine.NetInterfaceAsset{
			Asset: engine.Asset{
				CollectedAt: time.Now(),
			},
			InterfaceName: iface.Name,
			MACAddress:    iface.HardwareAddr.String(),
			IPv4Addresses: ipv4Addrs,
			IPv6Addresses: ipv6Addrs,
			MTU:           mtu,
			State:         state,
		}

		interfaces = append(interfaces, netInterface)
	}

	return interfaces, nil
}
