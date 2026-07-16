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
      <div v-if="data.buckets.length" style="display:flex;flex-wrap:wrap;gap:10px;margin:8px 0 16px">
        <div v-for="b in data.buckets" :key="b.range" class="quota-bucket" :class="{ 'bucket-warn': b.minPercent >= 71, 'bucket-danger': b.minPercent >= 91 }">
          <strong>{{ b.range }}</strong>
          <span>{{ b.count }} 个</span>
          <span class="sa-muted">{{ formatUSD(b.remainingUsd) }}</span>
        </div>
      </div>
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
    </template>
    <p v-if="!data && !loading && !error" class="sa-muted">选择分组并查询额度信息。</p>
  </section>
</template>

<style scoped>
.quota-stat { display: grid; gap: 2px; font-size: 13px; }
.quota-stat strong { font-size: 15px; }
.quota-bucket { display: grid; gap: 2px; padding: 8px 12px; border: 1px solid rgba(148,163,184,0.16); border-radius: 10px; font-size: 13px; background: rgba(15,23,42,0.4); }
.quota-bucket.bucket-warn { border-color: rgba(250,204,21,0.3); }
.quota-bucket.bucket-danger { border-color: rgba(239,68,68,0.3); background: rgba(239,68,68,0.08); }
.sortable { cursor: pointer; user-select: none; }
.sortable:hover { opacity: 0.8; }
</style>
