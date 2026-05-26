import { accountStatusOptions, accountTypeOptions, optionLabel, platformOptions } from './appOptions'
import { formatDateTime } from './formatters'
import { usagePercentText, usagePercentValue } from './visualMetrics'

export type AccountRecord = Record<string, unknown>
export type GroupState = 'any' | 'include' | 'exclude'

export function normalizeAccounts(payload: any): AccountRecord[] {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload.data)) return payload.data
  if (payload.data && Array.isArray(payload.data.items)) return payload.data.items
  if (payload.data && Array.isArray(payload.data.accounts)) return payload.data.accounts
  if (Array.isArray(payload.items)) return payload.items
  if (Array.isArray(payload.accounts)) return payload.accounts
  return []
}

export function accountID(account: AccountRecord) {
  return String(account.id || '')
}

export function accountName(account: AccountRecord) {
  const extra = (account.extra || {}) as Record<string, unknown>
  return String(account.name || account.email || extra.name || extra.email || account.id || '未命名账号')
}

export function accountNote(account: AccountRecord) {
  const extra = (account.extra || {}) as Record<string, unknown>
  const value = account.note ?? account.remark ?? account.description ?? extra.note ?? extra.remark ?? extra.description
  return value === undefined || value === null ? '' : String(value).trim()
}

export function accountGroups(account: AccountRecord) {
  const names = accountGroupEntries(account).map((group) => group.name)
  return names.length ? names.join(' / ') : '未分组'
}

export function accountGroupPreview(account: AccountRecord) {
  const groups = accountGroupEntries(account)
  return {
    items: groups.slice(0, 2),
    extra: Math.max(0, groups.length - 2),
  }
}

export function groupPopoverKey(account: AccountRecord) {
  return accountID(account) || accountName(account)
}

export function accountGroupEntries(account: AccountRecord) {
  const groups = Array.isArray(account.groups) ? account.groups : []
  const accountGroups = Array.isArray(account.account_groups) ? account.account_groups : []
  const entries = groups
    .map((group: any) => ({ id: String(group?.id || '').trim(), name: String(group?.name || '').trim() }))
    .concat(accountGroups.map((item: any) => ({ id: String(item?.group?.id || item?.group_id || '').trim(), name: String(item?.group?.name || '').trim() })))
    .filter((group) => group.id || group.name)
  const unique = new Map<string, { id: string; name: string }>()
  entries.forEach((group) => {
    const id = group.id || group.name
    unique.set(id, { id, name: group.name || `分组 #${id}` })
  })
  return Array.from(unique.values())
}

export function accountGroupIDs(account: AccountRecord) {
  const ids = accountGroupEntries(account).map((group) => group.id).filter(Boolean)
  return new Set(ids)
}

export function groupStateLabel(state: GroupState) {
  if (state === 'include') return '包含'
  if (state === 'exclude') return '排除'
  return '随意'
}

export function accountProxy(account: AccountRecord) {
  const proxy = account.proxy as Record<string, unknown> | undefined
  if (!proxy) return account.proxy_id ? `代理 #${account.proxy_id}` : '无代理'
  return String(proxy.name || proxy.id || account.proxy_id || '未知代理')
}

export function accountPlatformLabel(account: AccountRecord) {
  const parts = [optionLabel(platformOptions, account.platform), optionLabel(accountTypeOptions, account.type)]
  return parts.filter((part) => part !== '未知').join(' / ') || '未知'
}

export function accountStatusLabel(account: AccountRecord) {
  return optionLabel(accountStatusOptions, account.status)
}

export function accountStatusTone(account: AccountRecord) {
  const status = String(account.status || '').toLowerCase()
  if (!status) return 'neutral'
  if (['active', 'enabled', 'ready', 'ok', 'success'].includes(status)) return 'success'
  if (['error', 'failed', 'disabled', 'blocked', 'inactive'].includes(status)) return 'danger'
  if (['pending', 'testing', 'processing'].includes(status)) return 'warning'
  return 'neutral'
}

export function accountScheduleTone(account: AccountRecord) {
  switch (accountScheduleKey(account)) {
    case 'ready': return 'success'
    case 'rate': return 'warning'
    case 'overload': return 'info'
    case 'temp':
    case 'blocked': return 'danger'
    default: return 'neutral'
  }
}

export function accountUsageShort(account: AccountRecord) {
  const parts = accountUsageMetrics(account).map((metric) => `${metric.label} ${metric.text}`).filter((item) => !item.endsWith('未知'))
  return parts.length ? parts.join(' / ') : '未知'
}

export function accountUsageMetrics(account: AccountRecord) {
  const extra = (account.extra || {}) as Record<string, unknown>
  const primary = usagePercentValue(extra.codex_primary_used_percent ?? extra.codex_5h_used_percent)
  const secondary = usagePercentValue(extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent)
  return [
    { label: '5h', value: primary, text: usagePercentText(primary) },
    { label: '7d', value: secondary, text: usagePercentText(secondary) },
  ]
}

export function accountLastUsedLabel(account: AccountRecord) {
  return formatDateTime(account.last_used_at)
}

export function accountSchedule(account: AccountRecord) {
  if (account.temp_unschedulable_until) return '临时不可调度'
  if (account.schedulable === false) return '不可调度'
  if (account.overload_until) return '过载冷却'
  if (account.rate_limit_reset_at) return '限流中'
  return '可调度'
}

export function accountScheduleKey(account: AccountRecord) {
  if (account.temp_unschedulable_until) return 'temp'
  if (account.schedulable === false) return 'blocked'
  if (account.overload_until) return 'overload'
  if (account.rate_limit_reset_at) return 'rate'
  return 'ready'
}

export function accountUsage(account: AccountRecord) {
  const extra = (account.extra || {}) as Record<string, unknown>
  const primary = extra.codex_primary_used_percent ?? extra.codex_5h_used_percent
  const secondary = extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent
  return [primary !== undefined ? `短窗 ${primary}%` : '', secondary !== undefined ? `长窗 ${secondary}%` : ''].filter(Boolean).join(' / ') || '未知'
}

export function displayValue(value: unknown) {
  if (value === undefined || value === null || value === '') return '未知'
  return String(value)
}

export function accountErrorText(account: AccountRecord) {
  return displayValue(account.error_message || account.error || account.last_error)
}

export function accountUsageRows(account: AccountRecord) {
  const extra = (account.extra || {}) as Record<string, unknown>
  return [
    ['5h 用量', extra.codex_primary_used_percent ?? extra.codex_5h_used_percent],
    ['7d 用量', extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent],
    ['窗口开始', account.session_window_start],
    ['窗口结束', account.session_window_end],
    ['窗口状态', account.session_window_status],
  ].map(([label, value]) => ({ label: String(label), value: displayValue(value) }))
}

export function accountScheduleRows(account: AccountRecord) {
  return [
    ['调度状态', accountSchedule(account)],
    ['Schedulable', account.schedulable === false ? 'false' : 'true'],
    ['限流时间', formatDateTime(account.rate_limited_at)],
    ['限流恢复', formatDateTime(account.rate_limit_reset_at)],
    ['过载冷却', formatDateTime(account.overload_until)],
    ['临时不可调度', formatDateTime(account.temp_unschedulable_until)],
    ['临时原因', account.temp_unschedulable_reason],
  ].map(([label, value]) => ({ label: String(label), value: displayValue(value) }))
}

export function accountProxyRows(account: AccountRecord) {
  const proxy = account.proxy as Record<string, unknown> | undefined
  return [
    ['代理', accountProxy(account)],
    ['代理 ID', account.proxy_id],
    ['代理名称', proxy?.name],
    ['代理类型', proxy?.type],
  ].map(([label, value]) => ({ label: String(label), value: displayValue(value) }))
}

export function redactAccountValue(key: string, value: unknown): unknown {
  if (/credential|token|secret|password|cookie|key|authorization/i.test(key)) return '[已隐藏]'
  if (Array.isArray(value)) return value.map((item) => redactAccountValue(key, item))
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([childKey, childValue]) => [childKey, redactAccountValue(childKey, childValue)]))
  }
  return value
}

export function accountDetailJSON(account: AccountRecord) {
  return JSON.stringify(redactAccountValue('account', account), null, 2)
}
