<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api } from '../apiClient'
import { usagePercentValue, usagePercentText } from '../visualMetrics'

const props = defineProps<{ activeSiteId: number | null }>()

type QuotaAccount = {
  id: number
  name: string
  usedPercent: number
  windowMinutes: number
  resetAfterSeconds: number
  cycleCost: number
  estimatedTotalQuota: number
  remainingUsd: number
}

type QuotaBucket = {
  range: string
  minPercent: number
  maxPercent: number
  count: number
  remainingUsd: number
}

type QuotaResponse = {
  group: string
  groupId: number
  totalAccounts: number
  totalQuotaUsd: number
  usedQuotaUsd: number
  remainingUsd: number
  defaultQuotaPerAccount: number
  accurateEstimate: boolean
  cached: boolean
  cachedAt: number
  buckets: QuotaBucket[]
  accounts: QuotaAccount[]
}

const loading = ref(false)
const error = ref('')
const groupName = ref('free')
const defaultQuota = ref(2)
const data = ref<QuotaResponse | null>(null)
const sortBy = ref<'usedPercent' | 'remainingUsd' | 'name'>('usedPercent')
const sortDesc = ref(true)
const activeTab = ref<'chart' | 'table'>('chart')

async function loadQuota(refresh = false) {
  if (!props.activeSiteId) {
    error.value = '请先选择站点'
    return
  }
  error.value = ''
  loading.value = true
  try {
    let url = `api/sites/${props.activeSiteId}/group-quota?group=${encodeURIComponent(groupName.value)}&defaultQuota=${defaultQuota.value}`
    if (refresh) url += '&refresh=true'
    data.value = await api<QuotaResponse>(url)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载额度失败'
  } finally {
    loading.value = false
  }
}

const sortedAccounts = computed(() => {
  if (!data.value) return []
  const list = [...data.value.accounts]
  list.sort((a, b) => {
    let va: number | string, vb: number | string
    switch (sortBy.value) {
      case 'remainingUsd': va = a.remainingUsd; vb = b.remainingUsd; break
      case 'name': va = a.name; vb = b.name; break
      default: va = a.usedPercent; vb = b.usedPercent
    }
    if (typeof va === 'string' && typeof vb === 'string') {
      return sortDesc.value ? vb.localeCompare(va, 'zh-CN') : va.localeCompare(vb, 'zh-CN')
    }
    return sortDesc.value ? (vb as number) - (va as number) : (va as number) - (vb as number)
  })
  return list
})

function toggleSort(key: typeof sortBy.value) {
  if (sortBy.value === key) {
    sortDesc.value = !sortDesc.value
  } else {
    sortBy.value = key
    sortDesc.value = key !== 'name'
  }
}

function formatUSD(v: number) {
  return '$' + v.toFixed(2)
}

function percentClass(pct: number) {
  if (pct >= 90) return 'danger'
  if (pct >= 70) return 'warn'
  return ''
}

function formatResetTime(sec: number) {
  if (sec <= 0) return '—'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (h > 0) return `${h}h${m}m`
  return `${m}m`
}

// --- Chart helpers ---

// Bucket distribution bar chart
const bucketChartWidth = 520
const bucketChartHeight = 200
const bucketChartPad = { top: 20, right: 16, bottom: 36, left: 48 }

const bucketBarMetrics = computed(() => {
  if (!data.value || !data.value.buckets.length) return null
  const buckets = data.value.buckets
  const maxCount = Math.max(...buckets.map(b => b.count), 1)
  const innerW = bucketChartWidth - bucketChartPad.left - bucketChartPad.right
  const innerH = bucketChartHeight - bucketChartPad.top - bucketChartPad.bottom
  const barW = innerW / buckets.length * 0.6
  const gap = innerW / buckets.length * 0.4
  const step = innerW / buckets.length
  return { buckets, maxCount, innerW, innerH, barW, gap, step }
})

function bucketBarX(i: number) {
  const m = bucketBarMetrics.value
  if (!m) return 0
  return bucketChartPad.left + i * m.step + m.gap / 2
}

function bucketBarH(count: number) {
  const m = bucketBarMetrics.value
  if (!m) return 0
  return (count / m.maxCount) * m.innerH
}

function bucketBarY(count: number) {
  const m = bucketBarMetrics.value
  if (!m) return 0
  return bucketChartPad.top + m.innerH - bucketBarH(count)
}

function bucketYTick(v: number) {
  const m = bucketBarMetrics.value
  if (!m) return 0
  return bucketChartPad.top + m.innerH - (v / m.maxCount) * m.innerH
}

// Remaining USD per bucket bar chart
const remainingChartWidth = 520
const remainingChartHeight = 200
const remainingChartPad = { top: 20, right: 16, bottom: 36, left: 56 }

const remainingBarMetrics = computed(() => {
  if (!data.value || !data.value.buckets.length) return null
  const buckets = data.value.buckets
  const maxUSD = Math.max(...buckets.map(b => b.remainingUsd), 0.01)
  const innerW = remainingChartWidth - remainingChartPad.left - remainingChartPad.right
  const innerH = remainingChartHeight - remainingChartPad.top - remainingChartPad.bottom
  const barW = innerW / buckets.length * 0.6
  const gap = innerW / buckets.length * 0.4
  const step = innerW / buckets.length
  return { buckets, maxUSD, innerW, innerH, barW, gap, step }
})

function remainingBarX(i: number) {
  const m = remainingBarMetrics.value
  if (!m) return 0
  return remainingChartPad.left + i * m.step + m.gap / 2
}

function remainingBarH(usd: number) {
  const m = remainingBarMetrics.value
  if (!m) return 0
  return (usd / m.maxUSD) * m.innerH
}

function remainingBarY(usd: number) {
  const m = remainingBarMetrics.value
  if (!m) return 0
  return remainingChartPad.top + m.innerH - remainingBarH(usd)
}

function remainingYTick(v: number) {
  const m = remainingBarMetrics.value
  if (!m) return 0
  return remainingChartPad.top + m.innerH - (v / m.maxUSD) * m.innerH
}

// Estimated total quota distribution
const quotaBuckets = computed(() => {
  if (!data.value || !data.value.accounts.length) return []
  const ranges = [
    { label: '<2', min: 0, max: 2 },
    { label: '2-3', min: 2, max: 3 },
    { label: '3-4', min: 3, max: 4 },
    { label: '4-5', min: 4, max: 5 },
    { label: '5-6', min: 5, max: 6 },
    { label: '6-7', min: 6, max: 7 },
    { label: '7-8', min: 7, max: 8 },
    { label: '8-9', min: 8, max: 9 },
    { label: '9-10', min: 9, max: 10 },
    { label: '≥10', min: 10, max: Infinity },
  ]
  return ranges.map(r => ({
    ...r,
    count: data.value!.accounts.filter(a => a.estimatedTotalQuota >= r.min && a.estimatedTotalQuota < r.max).length,
  }))
})

const quotaChartWidth = 520
const quotaChartHeight = 200
const quotaChartPad = { top: 20, right: 16, bottom: 36, left: 48 }

const quotaBarMetrics = computed(() => {
  const buckets = quotaBuckets.value
  if (!buckets.length) return null
  const maxCount = Math.max(...buckets.map(b => b.count), 1)
  const innerW = quotaChartWidth - quotaChartPad.left - quotaChartPad.right
  const innerH = quotaChartHeight - quotaChartPad.top - quotaChartPad.bottom
  const step = innerW / buckets.length
  const barW = step * 0.6
  const gap = step * 0.4
  return { buckets, maxCount, innerW, innerH, barW, gap, step }
})

function quotaBarX(i: number) {
  const m = quotaBarMetrics.value
  if (!m) return 0
  return quotaChartPad.left + i * m.step + m.gap / 2
}

function quotaBarH(count: number) {
  const m = quotaBarMetrics.value
  if (!m) return 0
  return (count / m.maxCount) * m.innerH
}

function quotaBarY(count: number) {
  const m = quotaBarMetrics.value
  if (!m) return 0
  return quotaChartPad.top + m.innerH - quotaBarH(count)
}

function quotaYTick(v: number) {
  const m = quotaBarMetrics.value
  if (!m) return 0
  return quotaChartPad.top + m.innerH - (v / m.maxCount) * m.innerH
}

function bucketColor(minPct: number) {
  if (minPct >= 91) return '#ef4444'
  if (minPct >= 71) return '#f59e0b'
  if (minPct >= 51) return '#3b82f6'
  if (minPct >= 31) return '#22d3ee'
  return '#22c55e'
}

onMounted(() => { loadQuota() })
</script>

<template>
  <section class="sa-panel">
    <div class="sa-panel-head">
      <div>
        <h2>分组额度</h2>
        <p class="sa-muted">估算分组内账号的额度消耗与剩余情况，24小时缓存。</p>
      </div>
      <div class="sa-actions" style="margin-top:0">
        <label class="sa-muted" style="gap:7px;font-size:14px;display:grid">分组<input v-model="groupName" class="sa-input" style="width:80px;padding:9px 10px" placeholder="free" @keyup.enter="loadQuota()" /></label>
        <label class="sa-muted" style="gap:7px;font-size:14px;display:grid">默认额度<input v-model.number="defaultQuota" type="number" step="0.5" min="0.1" class="sa-input" style="width:80px;padding:9px 10px" @keyup.enter="loadQuota()" /></label>
        <button class="sa-btn sa-btn-secondary" :disabled="loading" @click="loadQuota()">{{ loading ? '加载中...' : '查询' }}</button>
        <button class="sa-btn sa-btn-secondary" :disabled="loading" @click="loadQuota(true)">强制刷新</button>
      </div>
    </div>
    <p v-if="error" class="sa-error">{{ error }}</p>
    <template v-if="data">
      <!-- Summary stats -->
      <div style="display:flex;flex-wrap:wrap;gap:16px;margin:12px 0">
        <div class="quota-stat">
          <span class="sa-muted">分组</span><strong>{{ data.group }}</strong>
        </div>
        <div class="quota-stat">
          <span class="sa-muted">账号数</span><strong>{{ data.totalAccounts }}</strong>
        </div>
        <div class="quota-stat">
          <span class="sa-muted">总额度</span><strong>{{ formatUSD(data.totalQuotaUsd) }}</strong>
        </div>
        <div class="quota-stat">
          <span class="sa-muted">已用</span><strong>{{ formatUSD(data.usedQuotaUsd) }}</strong>
        </div>
        <div class="quota-stat">
          <span class="sa-muted">剩余</span><strong>{{ formatUSD(data.remainingUsd) }}</strong>
        </div>
        <div class="quota-stat">
          <span class="sa-muted">精度</span><strong>{{ data.accurateEstimate ? '基于用量' : '默认估算' }}</strong>
        </div>
        <div v-if="data.cached" class="quota-stat">
          <span class="sa-muted">缓存</span><strong>{{ new Date(data.cachedAt * 1000).toLocaleString() }}</strong>
        </div>
      </div>

      <!-- Tab switcher -->
      <div class="quota-tabs">
        <button class="quota-tab" :class="{ active: activeTab === 'chart' }" @click="activeTab = 'chart'">图表</button>
        <button class="quota-tab" :class="{ active: activeTab === 'table' }" @click="activeTab = 'table'">表格</button>
      </div>

      <!-- Chart tab -->
      <div v-if="activeTab === 'chart'" class="quota-chart-section">
        <!-- Bucket count chart -->
        <div v-if="bucketBarMetrics" class="quota-chart-card">
          <h3>已用占比分布</h3>
          <p class="sa-muted">各已用百分比区间的账号数量</p>
          <svg :viewBox="`0 0 ${bucketChartWidth} ${bucketChartHeight}`" class="quota-chart-svg">
            <g :transform="`translate(0 0)`">
              <!-- Y axis -->
              <line :x1="bucketChartPad.left" :y1="bucketChartPad.top" :x2="bucketChartPad.left" :y2="bucketChartPad.top + bucketBarMetrics.innerH" class="qchart-axis" />
              <!-- X axis -->
              <line :x1="bucketChartPad.left" :y1="bucketChartPad.top + bucketBarMetrics.innerH" :x2="bucketChartWidth - bucketChartPad.right" :y2="bucketChartPad.top + bucketBarMetrics.innerH" class="qchart-axis" />
              <!-- Y ticks -->
              <text :x="bucketChartPad.left - 6" :y="bucketChartPad.top + bucketBarMetrics.innerH + 4" class="qchart-tick" text-anchor="end">0</text>
              <text :x="bucketChartPad.left - 6" :y="bucketYTick(bucketBarMetrics.maxCount) + 4" class="qchart-tick" text-anchor="end">{{ bucketBarMetrics.maxCount }}</text>
              <text v-if="bucketBarMetrics.maxCount > 2" :x="bucketChartPad.left - 6" :y="bucketYTick(bucketBarMetrics.maxCount / 2) + 4" class="qchart-tick" text-anchor="end">{{ Math.round(bucketBarMetrics.maxCount / 2) }}</text>
              <!-- Grid lines -->
              <line :x1="bucketChartPad.left" :y1="bucketYTick(bucketBarMetrics.maxCount)" :x2="bucketChartWidth - bucketChartPad.right" :y2="bucketYTick(bucketBarMetrics.maxCount)" class="qchart-grid" />
              <line v-if="bucketBarMetrics.maxCount > 2" :x1="bucketChartPad.left" :y1="bucketYTick(bucketBarMetrics.maxCount / 2)" :x2="bucketChartWidth - bucketChartPad.right" :y2="bucketYTick(bucketBarMetrics.maxCount / 2)" class="qchart-grid" />
              <!-- Bars -->
              <g v-for="(b, i) in bucketBarMetrics.buckets" :key="b.range">
                <rect :x="bucketBarX(i)" :y="bucketBarY(b.count)" :width="bucketBarMetrics.barW" :height="bucketBarH(b.count)" :fill="bucketColor(b.minPercent)" rx="4" class="qchart-bar" />
                <text v-if="b.count > 0" :x="bucketBarX(i) + bucketBarMetrics.barW / 2" :y="bucketBarY(b.count) - 5" class="qchart-bar-label" text-anchor="middle">{{ b.count }}</text>
                <text :x="bucketBarX(i) + bucketBarMetrics.barW / 2" :y="bucketChartPad.top + bucketBarMetrics.innerH + 16" class="qchart-x-label" text-anchor="middle">{{ b.range }}</text>
              </g>
            </g>
          </svg>
        </div>

        <!-- Remaining USD per bucket chart -->
        <div v-if="remainingBarMetrics" class="quota-chart-card">
          <h3>区间剩余额度</h3>
          <p class="sa-muted">各已用百分比区间的账号剩余额度合计 (USD)</p>
          <svg :viewBox="`0 0 ${remainingChartWidth} ${remainingChartHeight}`" class="quota-chart-svg">
            <g>
              <line :x1="remainingChartPad.left" :y1="remainingChartPad.top" :x2="remainingChartPad.left" :y2="remainingChartPad.top + remainingBarMetrics.innerH" class="qchart-axis" />
              <line :x1="remainingChartPad.left" :y1="remainingChartPad.top + remainingBarMetrics.innerH" :x2="remainingChartWidth - remainingChartPad.right" :y2="remainingChartPad.top + remainingBarMetrics.innerH" class="qchart-axis" />
              <text :x="remainingChartPad.left - 6" :y="remainingChartPad.top + remainingBarMetrics.innerH + 4" class="qchart-tick" text-anchor="end">$0</text>
              <text :x="remainingChartPad.left - 6" :y="remainingYTick(remainingBarMetrics.maxUSD) + 4" class="qchart-tick" text-anchor="end">${{ remainingBarMetrics.maxUSD.toFixed(1) }}</text>
              <line :x1="remainingChartPad.left" :y1="remainingYTick(remainingBarMetrics.maxUSD)" :x2="remainingChartWidth - remainingChartPad.right" :y2="remainingYTick(remainingBarMetrics.maxUSD)" class="qchart-grid" />
              <g v-for="(b, i) in remainingBarMetrics.buckets" :key="'r-' + b.range">
                <rect :x="remainingBarX(i)" :y="remainingBarY(b.remainingUsd)" :width="remainingBarMetrics.barW" :height="remainingBarH(b.remainingUsd)" :fill="bucketColor(b.minPercent)" rx="4" class="qchart-bar" />
                <text v-if="b.remainingUsd > 0" :x="remainingBarX(i) + remainingBarMetrics.barW / 2" :y="remainingBarY(b.remainingUsd) - 5" class="qchart-bar-label" text-anchor="middle">${{ b.remainingUsd.toFixed(2) }}</text>
                <text :x="remainingBarX(i) + remainingBarMetrics.barW / 2" :y="remainingChartPad.top + remainingBarMetrics.innerH + 16" class="qchart-x-label" text-anchor="middle">{{ b.range }}</text>
              </g>
            </g>
          </svg>
        </div>

        <!-- Estimated total quota distribution chart -->
        <div v-if="quotaBarMetrics && quotaBarMetrics.buckets.length" class="quota-chart-card">
          <h3>估算总额度分布</h3>
          <p class="sa-muted">不同估算总额度区间下的账号数量</p>
          <svg :viewBox="`0 0 ${quotaChartWidth} ${quotaChartHeight}`" class="quota-chart-svg">
            <g>
              <line :x1="quotaChartPad.left" :y1="quotaChartPad.top" :x2="quotaChartPad.left" :y2="quotaChartPad.top + quotaBarMetrics.innerH" class="qchart-axis" />
              <line :x1="quotaChartPad.left" :y1="quotaChartPad.top + quotaBarMetrics.innerH" :x2="quotaChartWidth - quotaChartPad.right" :y2="quotaChartPad.top + quotaBarMetrics.innerH" class="qchart-axis" />
              <text :x="quotaChartPad.left - 6" :y="quotaChartPad.top + quotaBarMetrics.innerH + 4" class="qchart-tick" text-anchor="end">0</text>
              <text :x="quotaChartPad.left - 6" :y="quotaYTick(quotaBarMetrics.maxCount) + 4" class="qchart-tick" text-anchor="end">{{ quotaBarMetrics.maxCount }}</text>
              <line :x1="quotaChartPad.left" :y1="quotaYTick(quotaBarMetrics.maxCount)" :x2="quotaChartWidth - quotaChartPad.right" :y2="quotaYTick(quotaBarMetrics.maxCount)" class="qchart-grid" />
              <g v-for="(b, i) in quotaBarMetrics.buckets" :key="'q-' + b.label">
                <rect :x="quotaBarX(i)" :y="quotaBarY(b.count)" :width="quotaBarMetrics.barW" :height="quotaBarH(b.count)" fill="#8b5cf6" rx="4" class="qchart-bar" />
                <text v-if="b.count > 0" :x="quotaBarX(i) + quotaBarMetrics.barW / 2" :y="quotaBarY(b.count) - 5" class="qchart-bar-label" text-anchor="middle">{{ b.count }}</text>
                <text :x="quotaBarX(i) + quotaBarMetrics.barW / 2" :y="quotaChartPad.top + quotaBarMetrics.innerH + 16" class="qchart-x-label" text-anchor="middle">${{ b.label }}</text>
              </g>
            </g>
          </svg>
        </div>
      </div>

      <!-- Table tab -->
      <div v-if="activeTab === 'table'" class="quota-table-section">
        <div class="sa-table-wrap">
          <table class="sa-table">
            <thead>
              <tr>
                <th class="sortable" @click="toggleSort('name')">名称 {{ sortBy === 'name' ? (sortDesc ? '▼' : '▲') : '' }}</th>
                <th class="sortable" @click="toggleSort('usedPercent')">已用% {{ sortBy === 'usedPercent' ? (sortDesc ? '▼' : '▲') : '' }}</th>
                <th>周期用量</th>
                <th>估算总额度</th>
                <th class="sortable" @click="toggleSort('remainingUsd')">剩余 {{ sortBy === 'remainingUsd' ? (sortDesc ? '▼' : '▲') : '' }}</th>
                <th>重置</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="acc in sortedAccounts" :key="acc.id">
                <td>
                  <div class="sa-cell-stack">
                    <strong>{{ acc.name }}</strong>
                    <span class="sa-muted sa-cell-subtitle">ID {{ acc.id }}</span>
                  </div>
                </td>
                <td>
                  <div class="sa-usage-row">
                    <div class="sa-usage-bar-shell">
                      <div class="sa-usage-bar" :class="percentClass(acc.usedPercent)" :style="{ width: `${acc.usedPercent}%` }"></div>
                    </div>
                    <strong class="sa-usage-value">{{ acc.usedPercent.toFixed(1) }}%</strong>
                  </div>
                </td>
                <td>{{ acc.cycleCost > 0 ? formatUSD(acc.cycleCost) : '—' }}</td>
                <td>{{ formatUSD(acc.estimatedTotalQuota) }}</td>
                <td>{{ formatUSD(acc.remainingUsd) }}</td>
                <td><span class="sa-muted">{{ formatResetTime(acc.resetAfterSeconds) }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
    <p v-if="!data && !loading && !error" class="sa-muted">选择分组并查询额度信息。</p>
  </section>
</template>

<style scoped>
.quota-stat { display: grid; gap: 2px; font-size: 13px; }
.quota-stat strong { font-size: 15px; }
.sortable { cursor: pointer; user-select: none; }
.sortable:hover { opacity: 0.8; }

.quota-tabs { display: inline-flex; gap: 2px; padding: 3px; border-radius: 12px; background: rgba(2,6,23,0.38); margin-bottom: 16px; }
.quota-tab { padding: 7px 16px; border-radius: 9px; background: transparent; color: #94a3b8; font-size: 13px; font-weight: 700; border: 0; cursor: pointer; }
.quota-tab.active { color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb); }

.quota-chart-section { display: grid; gap: 18px; }
.quota-chart-card { padding: 16px; border: 1px solid rgba(148,163,184,0.16); border-radius: 16px; background: rgba(2,6,23,0.24); }
.quota-chart-card h3 { margin: 0 0 4px; font-size: 15px; }
.quota-chart-svg { width: 100%; min-width: 400px; overflow: visible; }

.qchart-axis { stroke: rgba(148,163,184,0.38); stroke-width: 1; }
.qchart-grid { stroke: rgba(148,163,184,0.18); stroke-width: 1; stroke-dasharray: 4 5; }
.qchart-tick { fill: #94a3b8; font-size: 11px; }
.qchart-x-label { fill: #cbd5e1; font-size: 11px; }
.qchart-bar { opacity: 0.85; transition: opacity 0.15s; }
.qchart-bar:hover { opacity: 1; }
.qchart-bar-label { fill: #f8fafc; font-size: 11px; font-weight: 700; }

.quota-table-section { margin-top: 4px; }
</style>
