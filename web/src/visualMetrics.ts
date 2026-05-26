import { compactNumber } from './formatters'

export function chartPoints(rows: Record<string, unknown>[], key: string) {
  if (!rows.length) return ''
  const values = rows.map((row) => Number(row[key] || 0))
  const max = Math.max(1, ...values)
  const width = 520
  const height = 170
  return values.map((value, index) => {
    const x = rows.length === 1 ? width / 2 : (index / (rows.length - 1)) * width
    const y = height - (value / max) * height
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
}

export function chartPath(rows: Record<string, unknown>[], key: string) {
  if (!rows.length) return ''
  const values = rows.map((row) => Number(row[key] || 0))
  const max = Math.max(1, ...values)
  const width = 520
  const height = 170
  const points = values.map((value, index) => {
    const x = rows.length === 1 ? width / 2 : (index / (rows.length - 1)) * width
    const y = height - (value / max) * height
    return { x, y }
  })
  if (points.length === 1) {
    const p = points[0]
    return `M ${p.x.toFixed(1)} ${p.y.toFixed(1)}`
  }
  const path = [`M ${points[0].x.toFixed(1)} ${points[0].y.toFixed(1)}`]
  for (let i = 0; i < points.length - 1; i += 1) {
    const p0 = points[i - 1] || points[i]
    const p1 = points[i]
    const p2 = points[i + 1]
    const p3 = points[i + 2] || p2
    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = p1.y + (p2.y - p0.y) / 6
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = p2.y - (p3.y - p1.y) / 6
    path.push(`C ${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${p2.x.toFixed(1)} ${p2.y.toFixed(1)}`)
  }
  return path.join(' ')
}

export function chartX(index: number, rowsLength: number) {
  if (rowsLength <= 1) return 260
  return index / (rowsLength - 1) * 520
}

export function chartY(row: Record<string, unknown>, key: string, maxValue: number) {
  return 170 - Number(row[key] || 0) / maxValue * 170
}

export function chartAxisLabel(value: number) {
  return compactNumber(value)
}

export function usagePercentValue(value: unknown) {
  if (value === undefined || value === null || value === '') return null
  const numeric = Number(String(value).replace('%', '').trim())
  if (!Number.isFinite(numeric)) return null
  return Math.max(0, Math.min(100, numeric))
}

export function usagePercentText(value: number | null) {
  return value === null ? '未知' : `${value.toFixed(Number.isInteger(value) ? 0 : 1)}%`
}

export function usagePercentWidth(value: number | null) {
  return value === null ? 0 : value
}

export function rankingWidth(actualCost: unknown, maxRankingCost: number) {
  return usagePercentWidth(Number(actualCost || 0) / maxRankingCost * 100)
}

export function concurrencyPercent(row: Record<string, unknown>) {
  const current = Number(row.current_in_use || row.current_concurrency || 0)
  const max = Number(row.max_capacity || row.concurrency || 0)
  if (!Number.isFinite(current) || !Number.isFinite(max) || max <= 0) return 0
  return usagePercentWidth(current / max * 100)
}
