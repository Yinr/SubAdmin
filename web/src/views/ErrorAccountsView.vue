<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api } from '../apiClient'
import { usagePercentValue, usagePercentText } from '../visualMetrics'

const props = defineProps<{ activeSiteId: number | null }>()

type ErrorAccount = {
  id: number
  name: string
  email: string
  platform: string
  type: string
  status: string
  error_message: string
  updated_at: string
  usage_5h: number | null
  usage_7d: number | null
}

const accounts = ref<ErrorAccount[]>([])
const loading = ref(false)
const error = ref('')
const groupName = ref('free')
const copyNotice = ref('')
const filterHandled = ref<'all' | 'unhandled' | 'handled'>('unhandled')

function handledKey(id: number) {
  return `errorAccountHandled_${props.activeSiteId}_${id}`
}

function isHandled(id: number) {
  return localStorage.getItem(handledKey(id)) === '1'
}

function toggleHandled(id: number) {
  if (isHandled(id)) {
    localStorage.removeItem(handledKey(id))
  } else {
    localStorage.setItem(handledKey(id), '1')
  }
  accounts.value = [...accounts.value]
}

async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text)
    copyNotice.value = `已复制${label}`
  } catch {
    copyNotice.value = '复制失败，请手动选择文本复制'
  }
  window.setTimeout(() => { copyNotice.value = '' }, 3000)
}

function copyAllEmails() {
  const emails = filteredAccounts.value.map(a => a.email).filter(Boolean).join('\n')
  if (!emails) { copyNotice.value = '没有可复制的邮箱'; window.setTimeout(() => { copyNotice.value = '' }, 2000); return }
  copyText(emails, '全部邮箱')
}

function copyUnhandledEmails() {
  const emails = filteredAccounts.value.filter(a => !isHandled(a.id)).map(a => a.email).filter(Boolean).join('\n')
  if (!emails) { copyNotice.value = '没有未处理的邮箱'; window.setTimeout(() => { copyNotice.value = '' }, 2000); return }
  copyText(emails, '未处理邮箱')
}

const filteredAccounts = computed(() => {
  return accounts.value.filter(a => {
    if (filterHandled.value === 'handled') return isHandled(a.id)
    if (filterHandled.value === 'unhandled') return !isHandled(a.id)
    return true
  })
})

const unhandledCount = computed(() => accounts.value.filter(a => !isHandled(a.id)).length)
const handledCount = computed(() => accounts.value.filter(a => isHandled(a.id)).length)

async function loadErrorAccounts() {
  if (!props.activeSiteId) {
    error.value = '请先选择站点'
    return
  }
  error.value = ''
  loading.value = true
  try {
    const payload = await api<{ items: ErrorAccount[]; total: number; group: string }>(
      `api/sites/${props.activeSiteId}/error-accounts?group=${encodeURIComponent(groupName.value)}`
    )
    accounts.value = payload.items || []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载错误账号失败'
  } finally {
    loading.value = false
  }
}

function usageBarClass(value: number | null) {
  if (value === null) return 'unknown'
  if (value >= 90) return 'danger'
  if (value >= 70) return 'warn'
  return ''
}

function truncateError(msg: string, maxLen = 120) {
  if (!msg) return ''
  if (msg.length <= maxLen) return msg
  return msg.slice(0, maxLen) + '...'
}

onMounted(() => { loadErrorAccounts() })
</script>

<template>
  <section class="sa-panel">
    <div class="sa-panel-head">
      <div>
        <h2>错误账号</h2>
        <p class="sa-muted">列出指定分组中 status=error 的账号，可在本地标记已处理状态。</p>
      </div>
      <div class="sa-actions" style="margin-top:0">
        <label class="sa-muted" style="gap:7px;font-size:14px;display:grid">分组<input v-model="groupName" class="sa-input" style="width:80px;padding:9px 10px" placeholder="free" @keyup.enter="loadErrorAccounts" /></label>
        <button class="sa-btn sa-btn-secondary" :disabled="loading" @click="loadErrorAccounts">{{ loading ? '加载中...' : '刷新' }}</button>
        <button class="sa-btn sa-btn-secondary" @click="copyAllEmails">复制全部邮箱</button>
        <button class="sa-btn sa-btn-secondary" @click="copyUnhandledEmails">复制未处理邮箱</button>
      </div>
    </div>
    <p v-if="error" class="sa-error">{{ error }}</p>
    <p v-if="copyNotice" class="sa-muted">{{ copyNotice }}</p>
    <div style="display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:8px 0 12px">
      <span class="sa-muted">{{ accounts.length }} 个 · {{ unhandledCount }} 未处理 · {{ handledCount }} 已处理</span>
      <button class="sa-filter-chip" :class="{ active: filterHandled === 'all' }" @click="filterHandled = 'all'">全部 {{ accounts.length }}</button>
      <button class="sa-filter-chip" :class="{ active: filterHandled === 'unhandled' }" @click="filterHandled = 'unhandled'">未处理 {{ unhandledCount }}</button>
      <button class="sa-filter-chip" :class="{ active: filterHandled === 'handled' }" @click="filterHandled = 'handled'">已处理 {{ handledCount }}</button>
    </div>
    <div class="sa-table-wrap">
      <table class="sa-table">
        <thead>
          <tr>
            <th>邮箱</th>
            <th>平台/类型</th>
            <th>错误信息</th>
            <th>标记时间</th>
            <th>额度 5h / 7d</th>
            <th>已处理</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="account in filteredAccounts" :key="account.id">
            <td>
              <div class="sa-cell-stack">
                <strong class="sa-account-name">{{ account.email || account.name }}</strong>
                <span class="sa-muted sa-cell-subtitle">ID {{ account.id }}</span>
              </div>
            </td>
            <td>
              <div style="display:flex;flex-wrap:nowrap;gap:6px;align-items:center">
                <span class="sa-tag">{{ account.platform }}</span>
                <span class="sa-tag sa-tag-muted">{{ account.type }}</span>
              </div>
            </td>
            <td><span class="sa-error" style="font-size:12px;line-height:1.4;word-break:break-all">{{ truncateError(account.error_message) }}</span></td>
            <td><span class="sa-muted">{{ account.updated_at?.slice(0, 19) || '未知' }}</span></td>
            <td>
              <div class="sa-cell-stack compact">
                <div v-for="metric in [{ label: '5h', value: usagePercentValue(account.usage_5h) }, { label: '7d', value: usagePercentValue(account.usage_7d) }]" :key="metric.label" class="sa-usage-row">
                  <span class="sa-usage-meta">{{ metric.label }}</span>
                  <div class="sa-usage-bar-shell">
                    <div class="sa-usage-bar" :class="usageBarClass(metric.value)" :style="{ width: `${metric.value ?? 0}%` }"></div>
                  </div>
                  <strong class="sa-usage-value">{{ usagePercentText(metric.value) }}</strong>
                </div>
              </div>
            </td>
            <td><input type="checkbox" class="sa-checkbox" :checked="isHandled(account.id)" @change="toggleHandled(account.id)" /></td>
            <td class="sa-row-actions">
              <button class="sa-btn sa-btn-mini" @click="copyText(account.email || account.name, '邮箱')">复制邮箱</button>
            </td>
          </tr>
          <tr v-if="!filteredAccounts.length">
            <td colspan="7" class="sa-muted">{{ loading ? '加载中...' : (accounts.length ? '当前筛选无匹配账号。' : '请选择站点并查询。') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="mobile-error-list">
      <article v-for="account in filteredAccounts" :key="`mobile-${account.id}`" class="mobile-error-card">
        <div class="mobile-error-head">
          <input type="checkbox" class="sa-checkbox" :checked="isHandled(account.id)" @change="toggleHandled(account.id)" />
          <div>
            <strong style="overflow-wrap:anywhere">{{ account.email || account.name }}</strong>
            <span class="sa-muted">ID {{ account.id }}</span>
          </div>
        </div>
        <div style="display:flex;flex-wrap:wrap;gap:6px">
          <span class="sa-tag">{{ account.platform }}</span>
          <span class="sa-tag sa-tag-muted">{{ account.type }}</span>
        </div>
        <div class="sa-mobile-usage-list">
          <div v-for="metric in [{ label: '5h', value: usagePercentValue(account.usage_5h) }, { label: '7d', value: usagePercentValue(account.usage_7d) }]" :key="metric.label" class="sa-usage-row">
            <span class="sa-usage-meta">{{ metric.label }}</span>
            <div class="sa-usage-bar-shell">
              <div class="sa-usage-bar" :class="usageBarClass(metric.value)" :style="{ width: `${metric.value ?? 0}%` }"></div>
            </div>
            <strong class="sa-usage-value">{{ usagePercentText(metric.value) }}</strong>
          </div>
        </div>
        <div class="mobile-error-detail">
          <span class="sa-muted">错误</span><span class="sa-error" style="font-size:12px;word-break:break-all">{{ truncateError(account.error_message, 200) }}</span>
          <span class="sa-muted">时间</span><span>{{ account.updated_at?.slice(0, 19) || '未知' }}</span>
        </div>
        <div class="mobile-error-actions">
          <button class="sa-btn sa-btn-secondary" @click="copyText(account.email || account.name, '邮箱')">复制邮箱</button>
        </div>
      </article>
      <p v-if="!filteredAccounts.length" class="sa-muted mobile-empty">{{ loading ? '加载中...' : (accounts.length ? '当前筛选无匹配。' : '请选择站点并查询。') }}</p>
    </div>
  </section>
</template>

<style scoped>
.mobile-error-list { display: none; }
.mobile-error-card { display: grid; gap: 12px; padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(15, 23, 42, 0.58); }
.mobile-error-head { display: grid; grid-template-columns: auto 1fr; gap: 10px; align-items: start; }
.mobile-error-detail { display: grid; grid-template-columns: 60px 1fr; gap: 6px 8px; font-size: 13px; }
.mobile-error-actions { display: grid; grid-template-columns: 1fr; gap: 8px; }
.sa-mobile-usage-list { display: grid; gap: 8px; padding: 10px; border-radius: 12px; background: rgba(2, 6, 23, 0.22); }
.mobile-empty { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 14px; background: rgba(15, 23, 42, 0.4); }

@media (max-width: 640px) {
  .sa-table-wrap { display: none; }
  .mobile-error-list { display: grid; gap: 12px; }
}
</style>
