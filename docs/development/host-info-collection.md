# 主机信息采集说明

本文档说明 Agent 如何采集磁盘和网卡信息，并在心跳中上报到 Server。

## 概述

Agent 在心跳（DataType=1000）中上报主机信息时，除了已有的 OS、硬件、网络基础信息外，还可以上报磁盘和网卡详细信息。这些信息以 JSON 格式存储在心跳数据的 `fields` map 中。

## 字段说明

### 磁盘信息（disk_info）

**字段名**：`disk_info`  
**类型**：JSON 字符串（数组）  
**说明**：包含主机的所有磁盘挂载点信息

**JSON 格式**：
```json
[
  {
    "device": "/dev/sda1",
    "mount_point": "/",
    "file_system": "ext4",
    "total_size": 107374182400,
    "used_size": 53687091200,
    "available_size": 53687091200,
    "usage_percent": 50.0
  },
  {
    "device": "/dev/sda2",
    "mount_point": "/home",
    "file_system": "xfs",
    "total_size": 214748364800,
    "used_size": 107374182400,
    "available_size": 107374182400,
    "usage_percent": 50.0
  }
]
```

**字段说明**：
- `device`：设备路径（如 `/dev/sda1`）
- `mount_point`：挂载点（如 `/`、`/home`）
- `file_system`：文件系统类型（如 `ext4`、`xfs`、`btrfs`）
- `total_size`：总大小（字节）
- `used_size`：已用大小（字节）
- `available_size`：可用大小（字节）
- `usage_percent`：使用率（百分比，0-100）

### 网卡信息（network_interfaces）

**字段名**：`network_interfaces`  
**类型**：JSON 字符串（数组）  
**说明**：包含主机的所有网络接口信息（排除回环接口）

**JSON 格式**：
```json
[
  {
    "interface_name": "eth0",
    "mac_address": "00:11:22:33:44:55",
    "ipv4_addresses": ["192.168.1.100"],
    "ipv6_addresses": ["fe80::211:22ff:fe33:4455"],
    "mtu": 1500,
    "state": "up"
  },
  {
    "interface_name": "ens33",
    "mac_address": "aa:bb:cc:dd:ee:ff",
    "ipv4_addresses": ["10.0.0.10"],
    "ipv6_addresses": [],
    "mtu": 1500,
    "state": "up"
  }
]
```

**字段说明**：
- `interface_name`：接口名称（如 `eth0`、`ens33`、`wlan0`）
- `mac_address`：MAC 地址（如 `00:11:22:33:44:55`）
- `ipv4_addresses`：IPv4 地址列表
- `ipv6_addresses`：IPv6 地址列表
- `mtu`：最大传输单元（Maximum Transmission Unit）
- `state`：接口状态（`up` 或 `down`）

## Agent 采集实现

### 使用已实现的采集函数

项目已提供采集函数，位于 `internal/agent/heartbeat/hostinfo.go`：

- `CollectDiskInfo(ctx context.Context, logger *zap.Logger) string`：采集磁盘信息，返回 JSON 字符串
- `CollectNetworkInterfaces(ctx context.Context, logger *zap.Logger) string`：采集网卡信息，返回 JSON 字符串

这两个函数在采集失败时返回空字符串（不返回错误），避免影响心跳的正常上报。

### 在心跳中使用采集函数

参考 `internal/agent/heartbeat/heartbeat_example.go` 的示例：

```go
import (
    "context"
    "time"
    
    "go.uber.org/zap"
    
    bridgeProto "github.com/imkerbos/mxsec-platform/api/proto/bridge"
    "github.com/imkerbos/mxsec-platform/internal/agent/heartbeat"
)

func SendHeartbeat(ctx context.Context, logger *zap.Logger, agentID string) (*bridgeProto.Record, error) {
    // 创建心跳数据的 fields map
    fields := make(map[string]string)
    
    // ... 采集其他主机信息（OS、硬件、网络基础信息等）...
    // fields["os_family"] = getOSFamily()
    // fields["kernel_version"] = getKernelVersion()
    // fields["cpu_info"] = getCPUInfo()
    // fields["memory_size"] = getMemorySize()
    
    // 采集磁盘信息
    diskInfoJSON := heartbeat.CollectDiskInfo(ctx, logger)
    if diskInfoJSON != "" {
        fields["disk_info"] = diskInfoJSON
    }
    
    // 采集网卡信息
    networkInterfacesJSON := heartbeat.CollectNetworkInterfaces(ctx, logger)
    if networkInterfacesJSON != "" {
        fields["network_interfaces"] = networkInterfacesJSON
    }
    
    // 构建 bridge.Record
    record := &bridgeProto.Record{
        DataType:  1000, // 心跳数据类型
        Timestamp: time.Now().UnixNano(),
        Data: &bridgeProto.Payload{
            Fields: fields,
        },
    }
    
    return record, nil
}
```

### 实现细节

采集函数的实现参考了 `plugins/collector/engine/handlers/volume.go` 和 `plugins/collector/engine/handlers/network.go`：

**磁盘信息采集**：
- 读取 `/proc/mounts` 获取挂载信息
- 过滤虚拟文件系统和非块设备
- 使用 `df -B1` 命令获取磁盘使用情况
- 解析输出并计算使用率

**网卡信息采集**：
- 使用 `net.Interfaces()` 获取所有网络接口
- 跳过回环接口
- 获取每个接口的 IPv4/IPv6 地址、MAC 地址、MTU、状态等信息

## 心跳上报示例

在 Agent 的心跳数据中，使用采集函数并将结果添加到 `bridge.Record.Data.Fields` map 中：

```go
import (
    "context"
    "time"
    
    "go.uber.org/zap"
    
    bridgeProto "github.com/imkerbos/mxsec-platform/api/proto/bridge"
    "github.com/imkerbos/mxsec-platform/internal/agent/heartbeat"
)

func SendHeartbeat(ctx context.Context, logger *zap.Logger, agentID string) (*bridgeProto.Record, error) {
    fields := make(map[string]string)
    
    // ... 其他字段（os_family、kernel_version、cpu_info 等）...
    
    // 采集并添加磁盘信息
    diskInfoJSON := heartbeat.CollectDiskInfo(ctx, logger)
    if diskInfoJSON != "" {
        fields["disk_info"] = diskInfoJSON
    }
    
    // 采集并添加网卡信息
    networkInterfacesJSON := heartbeat.CollectNetworkInterfaces(ctx, logger)
    if networkInterfacesJSON != "" {
        fields["network_interfaces"] = networkInterfacesJSON
    }
    
    // 构建 bridge.Record
    record := &bridgeProto.Record{
        DataType:  1000, // 心跳数据类型
        Timestamp: time.Now().UnixNano(),
        Data: &bridgeProto.Payload{
            Fields: fields,
        },
    }
    
    return record, nil
}
```

**完整示例代码**：参考 `internal/agent/heartbeat/heartbeat_example.go`

## Server 端处理

Server 端在 `internal/server/agentcenter/transfer/service.go` 的 `handleHeartbeat` 方法中：

1. 从心跳记录的 `fields` map 中提取 `disk_info` 和 `network_interfaces` 字段
2. 直接存储到 `Host` 模型的 `DiskInfo` 和 `NetworkInterfaces` 字段（JSON 字符串）
3. 前端可以通过解析这些 JSON 字符串来展示磁盘和网卡信息

## 注意事项

1. **虚拟文件系统**：采集磁盘信息时，应跳过虚拟文件系统（如 `proc`、`sysfs`、`tmpfs` 等）
2. **回环接口**：采集网卡信息时，应跳过回环接口（`lo`）
3. **错误处理**：如果采集失败，不应影响心跳的正常上报，可以记录日志但不中断流程
4. **性能考虑**：磁盘信息采集需要执行 `df` 命令，可能有一定延迟，建议在心跳采集时使用超时控制
5. **数据大小**：JSON 字符串可能较大，确保心跳数据包不超过 gRPC 的最大消息大小限制

## 参考实现

- 磁盘采集：`plugins/collector/engine/handlers/volume.go`
- 网卡采集：`plugins/collector/engine/handlers/network.go`
- Server 心跳处理：`internal/server/agentcenter/transfer/service.go`
