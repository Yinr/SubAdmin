export function unwrapAPIData(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') return {}
  const raw = value as Record<string, unknown>
  if (raw.data && typeof raw.data === 'object') return raw.data as Record<string, unknown>
  return raw
}

export function statValue(source: Record<string, unknown>, key: string) {
  const value = source[key]
  if (value === undefined || value === null || value === '') return '暂无'
  if (typeof value === 'number') return value.toLocaleString('zh-CN')
  return String(value)
}

export function compactNumber(value: unknown) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return '暂无'
  const abs = Math.abs(numeric)
  if (abs >= 1_000_000_000) return `${(numeric / 1_000_000_000).toFixed(2)}B`
  if (abs >= 1_000_000) return `${(numeric / 1_000_000).toFixed(2)}M`
  if (abs >= 1_000) return `${(numeric / 1_000).toFixed(2)}K`
  return numeric.toLocaleString('zh-CN')
}

export function tokenValue(source: Record<string, unknown>, key: string) {
  return compactNumber(source[key])
}

export function statCost(source: Record<string, unknown>, key: string) {
  const value = Number(source[key])
  if (!Number.isFinite(value)) return '暂无'
  return `$${value.toFixed(2)}`
}

export function formatDateInput(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

export function formatDateTime(value: unknown) {
  if (!value) return '未知'
  let date: Date
  if (typeof value === 'number') {
    date = new Date(value < 1_000_000_000_000 ? value * 1000 : value)
  } else if (typeof value === 'string' && /^\d+$/.test(value.trim())) {
    const numeric = Number(value)
    date = new Date(numeric < 1_000_000_000_000 ? numeric * 1000 : numeric)
  } else {
    date = new Date(String(value))
  }
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleString('zh-CN', { hour12: false })
}

export function payloadTotal(payload: any) {
  return Number(payload.total || payload.data?.total || 0)
}
