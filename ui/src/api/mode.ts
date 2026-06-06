/**
 * 运行模式管理 API (Sprint 4 PR69)
 *
 * v2.0 引入两阶段哲学:
 *   - observe (默认): 仅观察 + 告警, 不阻断业务
 *   - protect:        observe + 自动阻断, 需 6 闸门 (G1-G6) admission 才能切换
 *
 * 4 级覆盖优先级 (高→低):
 *   rule > host_label > tenant > global
 */
import apiClient from './client'
import type { ApiResponse } from './types'

const { get, post } = apiClient

export type RunningMode = 'observe' | 'protect'

export interface GlobalMode {
  mode: RunningMode
  updated_by: string
  updated_at: string
}

export interface TenantMode {
  tenant_id: string
  mode: RunningMode
  updated_by: string
  updated_at: string
}

export interface HostLabelOverride {
  id: number
  tenant_id: string
  label_selector: string
  mode: RunningMode
  reason: string
  expires_at: string | null
}

export interface RuleOverride {
  id: number
  tenant_id: string
  rule_id: string
  mode: RunningMode
  reason: string
}

/** 6 闸门状态 (G1-G6 admission). */
export interface AdmissionGate {
  id: string                // G1..G6
  name: string
  description: string
  status: 'pass' | 'fail' | 'pending'
  detail: string
  evidence: string | null
  last_checked_at: string
}

export interface AdmissionSummary {
  tenant_id: string
  target_mode: RunningMode
  ready: boolean            // 全部 G1-G6 pass 才 ready
  gates: AdmissionGate[]
  blocking_reasons: string[]
}

export const ModeAPI = {
  /** 取全局默认模式. */
  getGlobal(): Promise<ApiResponse<GlobalMode>> {
    return get('/mode/global')
  },

  /** 全局模式切换 (需 admin + 6 闸门通过). */
  setGlobal(mode: RunningMode, reason: string): Promise<ApiResponse<GlobalMode>> {
    return post('/mode/global', { mode, reason })
  },

  /** 取租户模式. */
  getTenant(tenantId: string): Promise<ApiResponse<TenantMode>> {
    return get(`/mode/tenant/${tenantId}`)
  },

  /** 租户模式切换. */
  setTenant(tenantId: string, mode: RunningMode, reason: string): Promise<ApiResponse<TenantMode>> {
    return post(`/mode/tenant/${tenantId}`, { mode, reason })
  },

  /** 查 host_label 覆盖列表. */
  listHostLabelOverrides(): Promise<ApiResponse<HostLabelOverride[]>> {
    return get('/mode/overrides/host-label')
  },

  /** 新增/更新 host_label 覆盖 (e.g. env=staging 强制 observe). */
  upsertHostLabelOverride(payload: Omit<HostLabelOverride, 'id'>): Promise<ApiResponse<HostLabelOverride>> {
    return post('/mode/overrides/host-label', payload)
  },

  /** 查 rule 覆盖列表. */
  listRuleOverrides(): Promise<ApiResponse<RuleOverride[]>> {
    return get('/mode/overrides/rule')
  },

  upsertRuleOverride(payload: Omit<RuleOverride, 'id'>): Promise<ApiResponse<RuleOverride>> {
    return post('/mode/overrides/rule', payload)
  },

  /** 查 6 闸门当前状态 (切 protect 前必须 ready=true). */
  checkAdmission(targetMode: RunningMode): Promise<ApiResponse<AdmissionSummary>> {
    return get(`/mode/admission?target=${targetMode}`)
  },
}
