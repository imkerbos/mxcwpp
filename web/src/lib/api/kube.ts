import { get, post, put, del } from "./client";
import type {
  KubeCluster,
  KubeClusterList,
  KubeAlarmList,
  KubeEvent,
  KubeBaselineList,
  KubeBaselineAlertList,
  KubeBaselineRule,
  KubeBaselineRuleList,
  KubeWhitelist,
  KubeImageScan,
  KubeImageVulnerability,
  Paged,
} from "./types";

interface PageParams { page?: number; page_size?: number; [k: string]: unknown; }

export const kubeApi = {
  // 集群管理
  listClusters: (params?: PageParams) => get<KubeClusterList>("/kube/clusters", params),
  getCluster: (id: number) => get<KubeCluster>(`/kube/clusters/${id}`),
  createCluster: (body: Partial<KubeCluster>) => post<KubeCluster>("/kube/clusters", body),
  updateCluster: (id: number, body: Partial<KubeCluster>) => put<KubeCluster>(`/kube/clusters/${id}`, body),
  deleteCluster: (id: number) => del<void>(`/kube/clusters/${id}`),
  regenerateClusterToken: (id: number) => post<{ token: string }>(`/kube/clusters/${id}/regenerate-token`),

  // 安全告警
  listAlarms: (params?: PageParams) => get<KubeAlarmList>("/kube/alarms", params),
  processAlarm: (id: number, body?: object) => post<void>(`/kube/alarms/${id}/process`, body),
  batchProcessAlarms: (body: { ids: number[] }) => post<void>("/kube/alarms/batch-process", body),
  batchIgnoreAlarms: (body: { ids: number[] }) => post<void>("/kube/alarms/batch-ignore", body),

  // 安全事件
  listEvents: (params?: PageParams) => get<Paged<KubeEvent>>("/kube/events", params),
  handleEvent: (id: number, body?: object) => post<void>(`/kube/events/${id}/handle`, body),

  // 基线检查
  listBaseline: (params?: PageParams) => get<KubeBaselineList>("/kube/baseline", params),
  runBaselineDetect: (body?: object) => post<void>("/kube/baseline/detect", body),
  listBaselineAlerts: (params?: PageParams) => get<KubeBaselineAlertList>("/kube/baseline-alerts", params),
  ignoreBaselineAlert: (id: number, body?: object) => post<void>(`/kube/baseline-alerts/${id}/ignore`, body),
  batchIgnoreBaselineAlerts: (body: { ids: number[] }) => post<void>("/kube/baseline-alerts/batch-ignore", body),

  // 基线规则
  listBaselineRules: (params?: PageParams) => get<KubeBaselineRuleList>("/kube/baseline-rules", params),
  getBaselineRule: (id: number) => get<KubeBaselineRule>(`/kube/baseline-rules/${id}`),
  createBaselineRule: (body: Partial<KubeBaselineRule>) => post<KubeBaselineRule>("/kube/baseline-rules", body),
  updateBaselineRule: (id: number, body: Partial<KubeBaselineRule>) => put<KubeBaselineRule>(`/kube/baseline-rules/${id}`, body),
  deleteBaselineRule: (id: number) => del<void>(`/kube/baseline-rules/${id}`),
  toggleBaselineRule: (id: number) => put<void>(`/kube/baseline-rules/${id}/toggle`),

  // 告警白名单
  listWhitelist: (params?: PageParams) => get<Paged<KubeWhitelist>>("/kube/whitelist", params),
  createWhitelist: (body: Partial<KubeWhitelist>) => post<KubeWhitelist>("/kube/whitelist", body),
  updateWhitelist: (id: number, body: Partial<KubeWhitelist>) => put<KubeWhitelist>(`/kube/whitelist/${id}`, body),
  deleteWhitelist: (id: number) => del<void>(`/kube/whitelist/${id}`),

  // 镜像扫描
  listImageScans: (params?: PageParams) => get<Paged<KubeImageScan>>("/images/scans", params),
  getImageScan: (id: number) => get<KubeImageScan>(`/images/scans/${id}`),
  getImageScanVulns: (id: number) => get<KubeImageVulnerability[]>(`/images/scans/${id}/vulns`),
  scanImage: (image: string) => post<KubeImageScan>("/images/scan", { image }),
};
