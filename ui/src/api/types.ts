// API 响应类型定义

export interface ApiResponse<T = any> {
  code: number
  message?: string
  data: T
}

export interface PaginatedResponse<T> {
  total: number
  items: T[]
}

// 主机相关类型
export interface Host {
  host_id: string
  hostname: string
  os_family: string
  os_version: string
  kernel_version: string
  arch: string
  ipv4: string[]
  status: 'online' | 'offline'
  last_heartbeat: string
  created_at: string
  updated_at: string
  baseline_score?: number
  baseline_pass_rate?: number
  tags?: string[]
  is_container?: boolean // 是否为容器环境
  container_id?: string // 容器ID
  business_line?: string // 业务线
}

// 磁盘信息类型（用于 Host 的 disk_info 字段）
export interface DiskInfo {
  device: string // /dev/sda1
  mount_point: string // /、/home 等
  file_system: string // ext4、xfs 等
  total_size: number // 总大小（字节）
  used_size: number // 已用大小（字节）
  available_size: number // 可用大小（字节）
  usage_percent: number // 使用率（百分比）
}

// 网卡信息类型（用于 Host 的 network_interfaces 字段）
export interface NetworkInterfaceInfo {
  interface_name: string // eth0、ens33 等
  mac_address?: string
  ipv4_addresses?: string[]
  ipv6_addresses?: string[]
  mtu?: number
  state?: string // up、down
}

export interface HostDetail extends Host {
  baseline_results: ScanResult[]
  device_model?: string
  manufacturer?: string
  system_load?: string
  cpu_info?: string
  memory_size?: string
  default_gateway?: string
  network_mode?: string
  cpu_usage?: string
  memory_usage?: string
  dns_servers?: string[]
  device_serial?: string
  device_id?: string
  public_ipv4?: string[]
  public_ipv6?: string[]
  business_line?: string
  system_boot_time?: string
  agent_start_time?: string
  last_active_time?: string
  tags?: string[]
  disk_info?: string // JSON 字符串，解析后为 DiskInfo[]
  network_interfaces?: string // JSON 字符串，解析后为 NetworkInterfaceInfo[]
}

// 策略组相关类型
export interface PolicyGroup {
  id: string
  name: string
  description: string
  icon?: string
  color?: string
  sort_order: number
  enabled: boolean
  created_at: string
  updated_at: string
  policies?: Policy[]
  // 统计数据
  policy_count?: number
  rule_count?: number
  pass_rate?: number
  host_count?: number
}

export interface PolicyGroupStatistics {
  group_id: string
  policy_count: number
  rule_count: number
  host_count: number
  pass_rate: number
  pass_count: number
  fail_count: number
  risk_count: number
  last_check_time?: string
}

// OS 版本要求类型
export interface OSRequirement {
  os_family: string   // rocky, centos, debian 等
  min_version?: string // 最小版本（含）
  max_version?: string // 最大版本（含）
}

// 策略相关类型
export interface Policy {
  id: string
  name: string
  version: string
  description: string
  os_family: string[]
  os_version: string
  os_requirements?: OSRequirement[] // 详细 OS 版本要求
  target_type?: 'host' | 'container' | 'all' // 适用目标：主机/容器/全部
  enabled: boolean
  group_id?: string // 所属策略组ID
  rule_count?: number
  rules?: Rule[]
  created_at: string
  updated_at: string
}

export interface Rule {
  rule_id: string
  policy_id: string
  category: string
  title: string
  description: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  enabled: boolean
  target_type?: 'host' | 'container' | 'all' // 适用目标：主机/容器/全部
  check_config: CheckConfig
  fix_config: FixConfig
  created_at: string
  updated_at: string
}

export interface CheckConfig {
  condition?: 'all' | 'any'
  rules?: CheckRule[]
  type?: string
  [key: string]: any
}

export interface CheckRule {
  type: string
  param: string[]
  result?: string
}

export interface FixConfig {
  suggestion?: string
  command?: string
  [key: string]: any
}

// 任务相关类型
export interface ScanTask {
  task_id: string
  name: string
  type: 'manual' | 'scheduled' | 'baseline'
  target_type: 'all' | 'host_ids' | 'os_family'
  target_config: {
    host_ids?: string[]
    os_family?: string[]
  }
  target_hosts?: string[] // 目标主机 ID 列表
  matched_host_count?: number // 匹配的主机数量（在线）
  total_host_count?: number // 总目标主机数量（包括离线）
  policy_id: string
  rule_ids?: string[]
  status: 'pending' | 'running' | 'completed' | 'failed'
  created_at: string
  executed_at?: string
  completed_at?: string
  updated_at: string
}

// 检测结果相关类型
export interface ScanResult {
  result_id: string
  host_id: string
  rule_id: string
  policy_id: string
  task_id?: string
  category: string
  title: string
  description: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  status: 'pass' | 'fail' | 'error' | 'na'
  actual?: string
  expected?: string
  fix_suggestion?: string
  checked_at: string
}

export interface BaselineScore {
  host_id: string
  baseline_score: number
  pass_rate: number
  total_rules: number
  pass_count: number
  fail_count: number
  error_count: number
  na_count: number
  calculated_at: string
}

export interface BaselineSummary {
  host_id: string
  by_severity: {
    critical: { pass: number; fail: number; error: number; na: number }
    high: { pass: number; fail: number; error: number; na: number }
    medium: { pass: number; fail: number; error: number; na: number }
    low: { pass: number; fail: number; error: number; na: number }
  }
  by_category: Record<string, { pass: number; fail: number; error: number; na: number }>
}

// 资产数据相关类型
export interface Process {
  id: string
  host_id: string
  pid: string
  ppid: string
  cmdline: string
  exe: string
  exe_hash?: string
  container_id?: string
  uid: string
  gid: string
  username?: string
  groupname?: string
  collected_at: string
}

export interface Port {
  id: string
  host_id: string
  protocol: string // tcp/udp
  port: number
  state?: string // LISTEN/ESTABLISHED 等
  pid?: string
  process_name?: string
  container_id?: string
  collected_at: string
}

export interface AssetUser {
  id: string
  host_id: string
  username: string
  uid: string
  gid: string
  groupname?: string
  home_dir: string
  shell: string
  comment?: string
  has_password: boolean
  collected_at: string
}

export interface Software {
  id: string
  host_id: string
  name: string
  version?: string
  architecture?: string
  package_type: string // rpm、deb、pip、npm、jar 等
  vendor?: string
  install_time?: string
  collected_at: string
}

export interface Container {
  id: string
  host_id: string
  container_id: string
  container_name?: string
  image?: string
  image_id?: string
  runtime?: string // docker、containerd
  status?: string // running、stopped 等
  created_at?: string
  collected_at: string
}

export interface App {
  id: string
  host_id: string
  app_type: string // mysql、redis、nginx、kafka 等
  app_name?: string
  version?: string
  port?: number
  process_id?: string
  config_path?: string
  data_path?: string
  collected_at: string
}

export interface NetInterface {
  id: string
  host_id: string
  interface_name: string // eth0、ens33 等
  mac_address?: string
  ipv4_addresses?: string[]
  ipv6_addresses?: string[]
  mtu?: number
  state?: string // up、down
  collected_at: string
}

export interface Volume {
  id: string
  host_id: string
  device?: string // /dev/sda1
  mount_point?: string // /、/home 等
  file_system?: string // ext4、xfs 等
  total_size?: number // 总大小（字节）
  used_size?: number // 已用大小（字节）
  available_size?: number // 可用大小（字节）
  usage_percent?: number // 使用率（百分比）
  collected_at: string
}

export interface Kmod {
  id: string
  host_id: string
  module_name: string
  size?: number // 模块大小（字节）
  used_by?: number // 引用计数
  state?: string // Live、Loading、Unloading
  collected_at: string
}

export interface Service {
  id: string
  host_id: string
  service_name: string
  service_type?: string // systemd、sysv
  status?: string // active、inactive、failed 等
  enabled?: boolean // 是否开机自启
  description?: string
  collected_at: string
}

export interface Cron {
  id: string
  host_id: string
  user: string // root、username
  schedule: string // 调度表达式（* * * * *）
  command: string // 执行的命令
  cron_type?: string // crontab、systemd-timer
  enabled?: boolean // 是否启用
  collected_at: string
}

// 主机监控数据相关类型
export interface HostMetrics {
  host_id: string
  latest?: LatestMetrics
  time_series?: TimeSeriesMetrics
  source: 'mysql' | 'prometheus'
}

export interface LatestMetrics {
  cpu_usage?: number
  mem_usage?: number
  disk_usage?: number
  net_bytes_sent?: number
  net_bytes_recv?: number
  collected_at?: string
}

export interface TimeSeriesMetrics {
  cpu_usage?: TimeSeriesPoint[]
  mem_usage?: TimeSeriesPoint[]
  disk_usage?: TimeSeriesPoint[]
}

export interface TimeSeriesPoint {
  timestamp: string
  value: number
}

// 策略统计信息相关类型
export interface PolicyStatistics {
  policy_id: string
  rule_count: number
  host_count: number
  pass_rate: number
  pass_count: number
  fail_count: number
  risk_count: number
  last_check_time?: string
  by_severity?: {
    critical: { pass: number; fail: number }
    high: { pass: number; fail: number }
    medium: { pass: number; fail: number }
    low: { pass: number; fail: number }
  }
}
