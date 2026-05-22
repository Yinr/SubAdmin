<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

type Site = {
  id: number
  name: string
  baseUrl: string
  adminKeyHint: string
  note: string
  isDefault: boolean
  enabled: boolean
  lastCheckAt?: number
  lastCheckStatus?: string
}

type Account = Record<string, unknown>
type Group = Record<string, unknown>
type GroupState = 'any' | 'include' | 'exclude'
type ViewMode = 'accounts' | 'docs'

const authed = ref(false)
const expiresAt = ref('')
const loginSecret = ref('')
const loginError = ref('')
const sites = ref<Site[]>([])
const siteError = ref('')
const accountError = ref('')
const copyNotice = ref('')
const docsError = ref('')
const aiReference = ref('')
const batchTestError = ref('')
const accounts = ref<Account[]>([])
const groups = ref<Group[]>([])
const batchTestResults = ref<Record<string, unknown>[]>([])
const selectedAccountIds = ref<Set<string>>(new Set())
const activeSiteId = ref<number | null>(null)
const editingSite = ref<Site | null>(null)
const selectedAccount = ref<Account | null>(null)
const showSiteModal = ref(false)
const showAccountModal = ref(false)
const loginLoading = ref(false)
const sitesLoading = ref(false)
const accountsLoading = ref(false)
const groupsLoading = ref(false)
const savingSite = ref(false)
const docsLoading = ref(false)
const batchTesting = ref(false)
const activeView = ref<ViewMode>('accounts')

const siteForm = reactive({
  name: '',
  baseUrl: '',
  adminKey: '',
  note: '',
  isDefault: false,
  enabled: true,
})

const accountFilters = reactive({
  search: '',
  platform: '',
  status: '',
  type: '',
  privacyMode: '',
  sortBy: 'name',
  sortOrder: 'asc',
})

const batchTestForm = reactive({
  modelId: '',
  prompt: '',
  mode: '',
})

const scheduleQuickFilter = ref('all')
const groupToAdd = ref('')
const groupFilterStates = reactive<Record<string, GroupState>>({})

const accountPager = reactive({
  page: 1,
  pageSize: 10,
  total: 0,
  loaded: false,
})

const accountCache = new Map<string, { expiresAt: number; payload: any }>()
let accountRequestSeq = 0

const accountTotalPages = computed(() => (accountPager.total ? Math.ceil(accountPager.total / accountPager.pageSize) : 0))
const hasNextAccountPage = computed(() => {
  if (accountPager.total) return accountPager.page < accountTotalPages.value
  return accounts.value.length >= accountPager.pageSize
})

const groupOptions = computed(() => {
  const values = new Map<string, { id: string; name: string }>()
  groups.value.forEach((group) => {
    const id = String(group.id || '').trim()
    if (!id) return
    values.set(id, { id, name: String(group.name || `分组 #${id}`) })
  })
  accounts.value.forEach((account) => {
    accountGroupEntries(account).forEach((group) => values.set(group.id, group))
  })
  return Array.from(values.values()).sort((a, b) => a.name.localeCompare(b.name, 'zh-CN'))
})

const visibleAccounts = computed(() => {
  return accounts.value.filter((account) => {
    if (scheduleQuickFilter.value !== 'all' && accountScheduleKey(account) !== scheduleQuickFilter.value) return false
    return matchesGroupFilters(account)
  })
})

const groupFilterItems = computed(() => groupOptions.value.filter((group) => groupState(group.id) !== 'any'))

const addableGroupOptions = computed(() => groupOptions.value.filter((group) => groupState(group.id) === 'any'))

const selectedAccountCount = computed(() => selectedAccountIds.value.size)

const allVisibleSelected = computed(() => visibleAccounts.value.length > 0 && visibleAccounts.value.every((account) => selectedAccountIds.value.has(accountID(account))))

const activeSite = computed(() => sites.value.find((site) => site.id === activeSiteId.value) || null)

const dashboardStats = computed(() => {
  const scheduleCounts = accounts.value.reduce<Record<string, number>>((acc, account) => {
    const key = accountScheduleKey(account)
    acc[key] = (acc[key] || 0) + 1
    return acc
  }, {})
  const statusCounts = accounts.value.reduce<Record<string, number>>((acc, account) => {
    const key = String(account.status || 'unknown')
    acc[key] = (acc[key] || 0) + 1
    return acc
  }, {})
  const platformCounts = accounts.value.reduce<Record<string, number>>((acc, account) => {
    const key = String(account.platform || 'unknown')
    acc[key] = (acc[key] || 0) + 1
    return acc
  }, {})
  return {
    sites: sites.value.length,
    enabledSites: sites.value.filter((site) => site.enabled).length,
    loadedAccounts: accounts.value.length,
    visibleAccounts: visibleAccounts.value.length,
    upstreamTotal: accountPager.total,
    activeAccounts: statusCounts.active || 0,
    errorAccounts: statusCounts.error || 0,
    disabledAccounts: statusCounts.disabled || 0,
    rateLimited: scheduleCounts.rate || 0,
    overload: scheduleCounts.overload || 0,
    blocked: (scheduleCounts.temp || 0) + (scheduleCounts.blocked || 0),
    platforms: Object.entries(platformCounts)
      .sort((a, b) => b[1] - a[1])
      .map(([name, count]) => `${name}: ${count}`)
      .join(' / ') || '暂无账号数据',
  }
})

async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    ...options,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || '请求失败')
  return data as T
}

async function textResource(path: string): Promise<string> {
  const res = await fetch(path, { credentials: 'same-origin' })
  if (!res.ok) throw new Error('加载失败')
  return res.text()
}

async function refreshMe() {
  const data = await api<{ authenticated: boolean; expiresAt?: string }>('api/auth/me')
  authed.value = data.authenticated
  expiresAt.value = data.expiresAt || ''
  if (authed.value) await loadSites()
}

async function login() {
  loginError.value = ''
  loginLoading.value = true
  try {
    await api('api/auth/login', { method: 'POST', body: JSON.stringify({ secret: loginSecret.value }) })
    loginSecret.value = ''
    await refreshMe()
  } catch (error) {
    loginError.value = error instanceof Error ? error.message : '登录失败'
  } finally {
    loginLoading.value = false
  }
}

async function logout() {
  await api('api/auth/logout', { method: 'POST', body: '{}' })
  authed.value = false
  sites.value = []
  accounts.value = []
  activeSiteId.value = null
  accountPager.loaded = false
  activeView.value = 'accounts'
}

async function showDocs() {
  activeView.value = 'docs'
  if (aiReference.value || docsLoading.value) return
  docsError.value = ''
  docsLoading.value = true
  try {
    aiReference.value = await textResource('docs/AI_REFERENCE.md')
  } catch (error) {
    docsError.value = error instanceof Error ? error.message : '加载文档失败'
  } finally {
    docsLoading.value = false
  }
}

async function loadSites() {
  siteError.value = ''
  sitesLoading.value = true
  try {
    const data = await api<{ items: Site[] }>('api/sites')
    sites.value = data.items || []
    if (!activeSiteId.value && sites.value.length) {
      activeSiteId.value = (sites.value.find((site) => site.isDefault) || sites.value[0]).id
      await loadGroups()
    }
  } catch (error) {
    siteError.value = error instanceof Error ? error.message : '加载站点失败'
  } finally {
    sitesLoading.value = false
  }
}

function openCreateSite() {
  editingSite.value = null
  Object.assign(siteForm, { name: '', baseUrl: '', adminKey: '', note: '', isDefault: false, enabled: true })
  showSiteModal.value = true
}

function openEditSite(site: Site) {
  editingSite.value = site
  Object.assign(siteForm, {
    name: site.name,
    baseUrl: site.baseUrl,
    adminKey: '',
    note: site.note || '',
    isDefault: site.isDefault,
    enabled: site.enabled,
  })
  showSiteModal.value = true
}

async function saveSite() {
  siteError.value = ''
  savingSite.value = true
  try {
    const payload: Record<string, unknown> = { ...siteForm }
    if (editingSite.value && !String(payload.adminKey || '').trim()) delete payload.adminKey
    if (editingSite.value) {
      await api(`api/sites/${editingSite.value.id}`, { method: 'PATCH', body: JSON.stringify(payload) })
    } else {
      await api('api/sites', { method: 'POST', body: JSON.stringify(payload) })
    }
    showSiteModal.value = false
    await loadSites()
  } catch (error) {
    siteError.value = error instanceof Error ? error.message : '保存站点失败'
  } finally {
    savingSite.value = false
  }
}

async function patchSite(site: Site, payload: Record<string, unknown>) {
  await api(`api/sites/${site.id}`, { method: 'PATCH', body: JSON.stringify(payload) })
  await loadSites()
}

async function deleteSite(site: Site) {
  if (!confirm(`确定删除站点「${site.name}」吗？`)) return
  await api(`api/sites/${site.id}`, { method: 'DELETE' })
  if (activeSiteId.value === site.id) activeSiteId.value = null
  await loadSites()
}

async function testSite(site: Site) {
  const result = await api<{ ok: boolean; statusCode?: number; error?: string }>(`api/sites/${site.id}/test`, { method: 'POST', body: '{}' })
  alert(result.ok ? '连接正常' : `连接失败：${result.statusCode || result.error || '未知错误'}`)
  await loadSites()
}

function selectSite(site: Site) {
  activeSiteId.value = site.id
  groups.value = []
  resetGroupFilters()
  loadGroups()
  loadAccounts()
}

function normalizeGroups(payload: any): Group[] {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload.data)) return payload.data
  if (payload.data && Array.isArray(payload.data.items)) return payload.data.items
  if (Array.isArray(payload.items)) return payload.items
  return []
}

async function loadGroups() {
  if (!activeSiteId.value) return
  groupsLoading.value = true
  try {
    const payload = await api<any>(`api/sites/${activeSiteId.value}/groups`)
    groups.value = normalizeGroups(payload)
  } catch {
    groups.value = []
  } finally {
    groupsLoading.value = false
  }
}

function normalizeAccounts(payload: any): Account[] {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload.data)) return payload.data
  if (payload.data && Array.isArray(payload.data.items)) return payload.data.items
  if (payload.data && Array.isArray(payload.data.accounts)) return payload.data.accounts
  if (Array.isArray(payload.items)) return payload.items
  if (Array.isArray(payload.accounts)) return payload.accounts
  return []
}

function filterQueryValue(key: string, value: string) {
  if (!value.trim()) return ''
  if (key === 'privacyMode') return value.trim()
  return value.trim()
}

const activeAccountFilters = computed(() => {
  const parts = [
    accountFilters.search && `搜索: ${accountFilters.search}`,
    accountFilters.platform && `平台: ${accountFilters.platform}`,
    accountFilters.status && `状态: ${accountFilters.status}`,
    accountFilters.type && `类型: ${accountFilters.type}`,
    accountFilters.privacyMode && `隐私: ${accountFilters.privacyMode}`,
    accountFilters.sortBy !== 'name' && `排序: ${accountFilters.sortBy}`,
    accountFilters.sortOrder !== 'asc' && `方向: ${accountFilters.sortOrder}`,
    scheduleQuickFilter.value !== 'all' && `调度: ${scheduleQuickFilter.value}`,
    ...activeGroupFilters(),
  ].filter(Boolean)
  return parts.join(' · ')
})

async function loadAccounts(options: { force?: boolean } = {}) {
  accountError.value = ''
  if (!activeSiteId.value) {
    accountError.value = '请先选择站点'
    return
  }
  const requestSeq = ++accountRequestSeq
  const params = new URLSearchParams({ page: String(accountPager.page), page_size: String(accountPager.pageSize) })
  Object.entries(accountFilters).forEach(([key, value]) => {
    const trimmed = filterQueryValue(key, value)
    if (!trimmed) return
    if (key === 'privacyMode') {
      params.set('privacy_mode', trimmed)
      return
    }
    params.set(key, trimmed)
  })
  const cacheKey = `${activeSiteId.value}?${params.toString()}`
  const cached = accountCache.get(cacheKey)
  if (!options.force && cached && cached.expiresAt > Date.now()) {
    accounts.value = normalizeAccounts(cached.payload)
    accountPager.total = Number(cached.payload.total || cached.payload.data?.total || 0)
    accountPager.loaded = true
    return
  }
  accountsLoading.value = true
  try {
    const payload = await api<any>(`api/sites/${activeSiteId.value}/accounts?${params.toString()}`)
    if (requestSeq !== accountRequestSeq) return
    accountCache.set(cacheKey, { expiresAt: Date.now() + 8000, payload })
    accounts.value = normalizeAccounts(payload)
    accountPager.total = Number(payload.total || payload.data?.total || 0)
    accountPager.loaded = true
  } catch (error) {
    if (requestSeq !== accountRequestSeq) return
    accountError.value = error instanceof Error ? error.message : '查询账号失败'
  } finally {
    if (requestSeq === accountRequestSeq) accountsLoading.value = false
  }
}

function submitAccountFilters() {
  accountPager.page = 1
  loadAccounts()
}

function changeAccountPageSize() {
  accountPager.page = 1
  loadAccounts()
}

function goPrevAccounts() {
  if (accountPager.page <= 1) return
  accountPager.page -= 1
  loadAccounts()
}

function goNextAccounts() {
  if (!hasNextAccountPage.value) return
  accountPager.page += 1
  loadAccounts()
}

function accountName(account: Account) {
  const extra = (account.extra || {}) as Record<string, unknown>
  return String(account.name || account.email || extra.name || extra.email || account.id || '未命名账号')
}

function accountID(account: Account) {
  return String(account.id || '')
}

function toggleAccountSelection(account: Account) {
  const id = accountID(account)
  if (!id) return
  const next = new Set(selectedAccountIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedAccountIds.value = next
}

function toggleAllVisibleAccounts() {
  const next = new Set(selectedAccountIds.value)
  if (allVisibleSelected.value) {
    visibleAccounts.value.forEach((account) => next.delete(accountID(account)))
  } else {
    visibleAccounts.value.forEach((account) => {
      const id = accountID(account)
      if (id) next.add(id)
    })
  }
  selectedAccountIds.value = next
}

function clearAccountSelection() {
  selectedAccountIds.value = new Set()
}

function accountGroups(account: Account) {
  const names = accountGroupEntries(account).map((group) => group.name)
  return names.length ? names.join(' / ') : '未分组'
}

function accountGroupEntries(account: Account) {
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

function accountGroupIDs(account: Account) {
  const ids = accountGroupEntries(account).map((group) => group.id).filter(Boolean)
  return new Set(ids)
}

function groupState(id: string): GroupState {
  return groupFilterStates[id] || 'any'
}

function setGroupState(id: string, state: GroupState) {
  if (state === 'any') {
    delete groupFilterStates[id]
    if (!groupToAdd.value) groupToAdd.value = id
    return
  }
  groupFilterStates[id] = state
}

function addGroupFilter() {
  if (!groupToAdd.value) return
  groupFilterStates[groupToAdd.value] = 'include'
  groupToAdd.value = addableGroupOptions.value.find((group) => group.id !== groupToAdd.value)?.id || ''
}

function matchesGroupFilters(account: Account) {
  const accountGroups = accountGroupIDs(account)
  return Object.entries(groupFilterStates).every(([id, state]) => {
    if (state === 'include') return accountGroups.has(id)
    if (state === 'exclude') return !accountGroups.has(id)
    return true
  })
}

function activeGroupFilters() {
  return groupOptions.value
    .map((group) => {
      const state = groupState(group.id)
      if (state === 'include') return `包含分组: ${group.name}`
      if (state === 'exclude') return `排除分组: ${group.name}`
      return ''
    })
    .filter(Boolean)
}

function resetGroupFilters() {
  Object.keys(groupFilterStates).forEach((key) => delete groupFilterStates[key])
  groupToAdd.value = ''
}

function accountProxy(account: Account) {
  const proxy = account.proxy as Record<string, unknown> | undefined
  if (!proxy) return account.proxy_id ? `代理 #${account.proxy_id}` : '无代理'
  return String(proxy.name || proxy.id || account.proxy_id || '未知代理')
}

function accountSchedule(account: Account) {
  if (account.temp_unschedulable_until) return '临时不可调度'
  if (account.schedulable === false) return '不可调度'
  if (account.overload_until) return '过载冷却'
  if (account.rate_limit_reset_at) return '限流中'
  return '可调度'
}

function accountScheduleKey(account: Account) {
  if (account.temp_unschedulable_until) return 'temp'
  if (account.schedulable === false) return 'blocked'
  if (account.overload_until) return 'overload'
  if (account.rate_limit_reset_at) return 'rate'
  return 'ready'
}

function accountUsage(account: Account) {
  const extra = (account.extra || {}) as Record<string, unknown>
  const primary = extra.codex_primary_used_percent ?? extra.codex_5h_used_percent
  const secondary = extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent
  return [primary !== undefined ? `短窗 ${primary}%` : '', secondary !== undefined ? `长窗 ${secondary}%` : ''].filter(Boolean).join(' / ') || '未知'
}

function displayValue(value: unknown) {
  if (value === undefined || value === null || value === '') return '未知'
  return String(value)
}

function accountErrorText(account: Account) {
  return displayValue(account.error_message || account.error || account.last_error)
}

function accountUsageRows(account: Account) {
  const extra = (account.extra || {}) as Record<string, unknown>
  return [
    ['短窗用量', extra.codex_primary_used_percent ?? extra.codex_5h_used_percent],
    ['长窗用量', extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent],
    ['窗口开始', account.session_window_start],
    ['窗口结束', account.session_window_end],
    ['窗口状态', account.session_window_status],
  ].map(([label, value]) => ({ label: String(label), value: value === undefined || value === null || value === '' ? '未知' : String(value) }))
}

function accountScheduleRows(account: Account) {
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

function accountProxyRows(account: Account) {
  const proxy = account.proxy as Record<string, unknown> | undefined
  return [
    ['代理', accountProxy(account)],
    ['代理 ID', account.proxy_id],
    ['代理名称', proxy?.name],
    ['代理类型', proxy?.type],
  ].map(([label, value]) => ({ label: String(label), value: displayValue(value) }))
}

function formatDateTime(value: unknown) {
  if (!value) return '未知'
  const date = new Date(String(value))
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleString('zh-CN', { hour12: false })
}

function openAccountDetail(account: Account) {
  selectedAccount.value = account
  showAccountModal.value = true
}

function clearAccountFilters() {
  Object.assign(accountFilters, { search: '', platform: '', status: '', type: '', privacyMode: '', sortBy: 'name', sortOrder: 'asc' })
  scheduleQuickFilter.value = 'all'
  resetGroupFilters()
  accountPager.page = 1
  loadAccounts()
}

function redactAccountValue(key: string, value: unknown): unknown {
  if (/credential|token|secret|password|cookie|key|authorization/i.test(key)) return '[已隐藏]'
  if (Array.isArray(value)) return value.map((item) => redactAccountValue(key, item))
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([childKey, childValue]) => [childKey, redactAccountValue(childKey, childValue)]))
  }
  return value
}

function accountDetailJSON(account: Account) {
  return JSON.stringify(redactAccountValue('account', account), null, 2)
}

async function testSelectedAccounts() {
  batchTestError.value = ''
  batchTestResults.value = []
  if (!activeSiteId.value) {
    batchTestError.value = '请先选择站点'
    return
  }
  const ids = Array.from(selectedAccountIds.value).map((id) => Number(id)).filter((id) => Number.isFinite(id) && id > 0)
  if (!ids.length) {
    batchTestError.value = '请先选择账号'
    return
  }
  if (ids.length > 20) {
    batchTestError.value = '一次最多检测 20 个账号'
    return
  }
  batchTesting.value = true
  try {
    for (const id of ids) {
      batchTestResults.value = [...batchTestResults.value, { id, ok: false, message: '检测中...' }]
      const payload = await api<{ items: Record<string, unknown>[] }>(`api/sites/${activeSiteId.value}/accounts`, {
        method: 'POST',
        body: JSON.stringify({ ids: [id], ...batchTestForm }),
      })
      const result = payload.items?.[0] || { id, ok: false, error: '无检测结果' }
      batchTestResults.value = batchTestResults.value.map((item) => String(item.id) === String(id) ? result : item)
    }
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '批量检测失败'
  } finally {
    batchTesting.value = false
  }
}

async function copyText(value: unknown, label: string) {
  const text = String(value || '')
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    copyNotice.value = `已复制${label}`
  } catch {
    copyNotice.value = '复制失败，请手动选择文本复制'
  }
  window.setTimeout(() => {
    copyNotice.value = ''
  }, 1800)
}

onMounted(refreshMe)
</script>

<template>
  <main class="app-shell">
    <section v-if="!authed" class="login-card">
      <p class="eyebrow">SubAdmin</p>
      <h1>管理控制台</h1>
      <p class="muted">使用管理密钥登录。sub2api 管理员 Key 只保存在服务端。</p>
      <form @submit.prevent="login" class="form-block">
        <label>管理密钥<input v-model="loginSecret" type="password" autocomplete="current-password" required /></label>
        <button type="submit" :disabled="loginLoading">{{ loginLoading ? '登录中...' : '登录' }}</button>
        <p v-if="loginError" class="error">{{ loginError }}</p>
      </form>
    </section>

    <section v-else class="dashboard">
      <header class="topbar">
        <div>
          <p class="eyebrow">SubAdmin</p>
          <h1>管理控制台</h1>
          <p class="muted">会话过期时间：{{ expiresAt || '未知' }}</p>
        </div>
        <div class="topbar-actions">
          <button :class="activeView === 'accounts' ? '' : 'secondary'" @click="activeView = 'accounts'">账号</button>
          <button :class="activeView === 'docs' ? '' : 'secondary'" @click="showDocs">文档</button>
          <button class="secondary" @click="logout">退出登录</button>
        </div>
      </header>

      <section v-if="activeView === 'accounts'" class="overview-grid">
        <article class="overview-card">
          <span>站点</span>
          <strong>{{ dashboardStats.enabledSites }} / {{ dashboardStats.sites }}</strong>
          <p class="muted">启用 / 全部</p>
        </article>
        <article class="overview-card">
          <span>当前站点</span>
          <strong>{{ activeSite?.name || '未选择' }}</strong>
          <p class="muted">{{ activeSite?.enabled ? '启用' : '未启用或未知' }}</p>
        </article>
        <article class="overview-card">
          <span>账号</span>
          <strong>{{ dashboardStats.visibleAccounts }} / {{ dashboardStats.loadedAccounts }}</strong>
          <p class="muted">当前页命中 / 已加载<span v-if="dashboardStats.upstreamTotal"> · 上游 {{ dashboardStats.upstreamTotal }}</span></p>
        </article>
        <article class="overview-card">
          <span>状态</span>
          <strong>{{ dashboardStats.activeAccounts }} active</strong>
          <p class="muted">error {{ dashboardStats.errorAccounts }} · disabled {{ dashboardStats.disabledAccounts }}</p>
        </article>
        <article class="overview-card">
          <span>调度风险</span>
          <strong>{{ dashboardStats.rateLimited + dashboardStats.overload + dashboardStats.blocked }}</strong>
          <p class="muted">限流 {{ dashboardStats.rateLimited }} · 过载 {{ dashboardStats.overload }} · 不可调度 {{ dashboardStats.blocked }}</p>
        </article>
        <article class="overview-card wide">
          <span>平台分布</span>
          <strong>{{ dashboardStats.platforms }}</strong>
          <p class="muted">基于当前已加载账号结果统计</p>
        </article>
        <article class="overview-card wide">
          <span>文档</span>
          <strong>受保护 API 文档</strong>
          <p class="muted">登录后可访问 Swagger UI、OpenAPI YAML 和 AI Reference。文档不会自动注入 sub2api 管理员 Key。</p>
          <div class="actions compact-actions">
            <a class="button-link secondary" href="docs/" target="_blank" rel="noreferrer">Swagger UI</a>
            <a class="button-link secondary" href="docs/openapi.yaml" target="_blank" rel="noreferrer">OpenAPI</a>
            <a class="button-link secondary" href="docs/AI_REFERENCE.md" target="_blank" rel="noreferrer">AI Reference</a>
          </div>
        </article>
      </section>

      <section v-else class="panel docs-panel">
        <div class="panel-head">
          <div>
            <h2>API 文档</h2>
            <p class="muted">这些文档通过 SubAdmin 登录态保护。不会自动注入 sub2api 管理员 Key。</p>
          </div>
          <button class="secondary" :disabled="docsLoading" @click="showDocs">{{ docsLoading ? '加载中...' : '刷新 AI Reference' }}</button>
        </div>
        <div class="docs-links">
          <a class="button-link secondary" href="docs/" target="_blank" rel="noreferrer">打开 Swagger UI</a>
          <a class="button-link secondary" href="docs/openapi.yaml" target="_blank" rel="noreferrer">查看 OpenAPI YAML</a>
          <a class="button-link secondary" href="docs/AI_REFERENCE.md" target="_blank" rel="noreferrer">打开原始 AI Reference</a>
        </div>
        <p class="muted">Swagger UI 的 Try it out 仍需你手动填写上游管理员 Key；SubAdmin 不会把已保存站点 Key 注入浏览器。</p>
        <p v-if="docsError" class="error">{{ docsError }}</p>
        <p v-if="docsLoading" class="muted">正在加载 AI Reference...</p>
        <section class="docs-reader" v-if="aiReference">
          <div class="panel-head">
            <h2>AI Reference</h2>
            <button class="secondary" @click="copyText(aiReference, 'AI Reference')">复制全文</button>
          </div>
          <pre>{{ aiReference }}</pre>
        </section>
      </section>

      <div v-if="activeView === 'accounts'" class="layout-grid">
        <aside class="panel">
          <div class="panel-head">
            <h2>站点</h2>
            <button @click="openCreateSite">新增</button>
          </div>
          <p v-if="siteError" class="error">{{ siteError }}</p>
          <p v-if="sitesLoading" class="muted">正在加载站点...</p>
          <div class="site-list">
            <article v-for="site in sites" :key="site.id" class="site-card" :class="{ active: site.id === activeSiteId }">
              <div class="site-title">
                <strong>{{ site.name }}</strong>
                <span class="pill" v-if="site.isDefault">默认</span>
              </div>
              <p class="muted">{{ site.baseUrl }}</p>
              <p class="muted">{{ site.enabled ? '启用' : '停用' }} · {{ site.adminKeyHint }}</p>
              <div class="actions">
                <button class="secondary" @click="selectSite(site)">查看账号</button>
                <button class="secondary" @click="openEditSite(site)">编辑</button>
                <button class="secondary" :disabled="site.isDefault" @click="patchSite(site, { isDefault: true })">设默认</button>
                <button class="secondary" @click="patchSite(site, { enabled: !site.enabled })">{{ site.enabled ? '停用' : '启用' }}</button>
                <button class="secondary" @click="testSite(site)">测试</button>
                <button class="danger" @click="deleteSite(site)">删除</button>
              </div>
            </article>
            <p v-if="!sites.length" class="muted">还没有配置站点。</p>
          </div>
        </aside>

        <section class="panel content-panel">
          <div class="panel-head">
            <h2>上游账号</h2>
            <button class="secondary" :disabled="accountsLoading" @click="loadAccounts({ force: true })">刷新</button>
          </div>
          <div class="filter-note">
            <strong>上游查询筛选</strong>
            <span>以下条件会发送到 sub2api 账号列表接口，会影响上游返回的数据范围。</span>
          </div>
          <form class="filter-grid" @submit.prevent="submitAccountFilters">
            <label>搜索<input v-model="accountFilters.search" placeholder="名称、备注或标识" /></label>
            <label>平台
              <select v-model="accountFilters.platform">
                <option value="">全部</option>
                <option value="anthropic">Anthropic</option>
                <option value="openai">OpenAI</option>
                <option value="gemini">Gemini</option>
                <option value="antigravity">Antigravity</option>
              </select>
            </label>
            <label>状态
              <select v-model="accountFilters.status">
                <option value="">全部</option>
                <option value="active">active</option>
                <option value="disabled">disabled</option>
                <option value="error">error</option>
                <option value="unused">unused</option>
                <option value="used">used</option>
                <option value="expired">expired</option>
              </select>
            </label>
            <label>类型
              <select v-model="accountFilters.type">
                <option value="">全部</option>
                <option value="oauth">OAuth</option>
                <option value="setup-token">Setup Token</option>
                <option value="apikey">API Key</option>
                <option value="upstream">Upstream</option>
                <option value="bedrock">Bedrock</option>
                <option value="service-account">Service Account</option>
              </select>
            </label>
            <label>隐私模式
              <select v-model="accountFilters.privacyMode">
                <option value="">全部</option>
                <option value="training_off">OpenAI: training_off</option>
                <option value="training_set_failed">OpenAI: training_set_failed</option>
                <option value="training_set_cf_blocked">OpenAI: training_set_cf_blocked</option>
                <option value="privacy_set">Antigravity: privacy_set</option>
                <option value="privacy_set_failed">Antigravity: privacy_set_failed</option>
                <option value="__unset__">未设置</option>
              </select>
            </label>
            <label>排序字段
              <select v-model="accountFilters.sortBy">
                <option value="name">名称</option>
                <option value="created_at">创建时间</option>
                <option value="updated_at">更新时间</option>
                <option value="last_used_at">最近使用</option>
                <option value="priority">优先级</option>
                <option value="rate_limited_at">限流时间</option>
              </select>
            </label>
            <label>排序方向
              <select v-model="accountFilters.sortOrder">
                <option value="asc">升序</option>
                <option value="desc">降序</option>
              </select>
            </label>
            <label>每页数量
              <select v-model.number="accountPager.pageSize" @change="changeAccountPageSize">
                <option :value="10">10</option>
                <option :value="20">20</option>
                <option :value="50">50</option>
              </select>
            </label>
            <button type="submit" :disabled="accountsLoading">{{ accountsLoading ? '查询中...' : '查询' }}</button>
            <button type="button" class="secondary" :disabled="accountsLoading" @click="clearAccountFilters">清空筛选</button>
          </form>
          <section class="local-filter">
            <div class="filter-note compact">
              <strong>当前页本地筛选</strong>
              <span>以下条件只过滤当前已加载结果，不会重新请求上游，也不代表全库命中数量。</span>
            </div>
            <label>调度状态
              <select v-model="scheduleQuickFilter">
                <option value="all">全部</option>
                <option value="ready">可调度</option>
                <option value="rate">限流中</option>
                <option value="overload">过载冷却</option>
                <option value="temp">临时不可调度</option>
                <option value="blocked">不可调度</option>
              </select>
            </label>
            <div class="group-filter-head">
              <strong>分组三态筛选</strong>
              <span class="muted">未加入筛选列表的分组默认随意</span>
            </div>
            <p v-if="groupsLoading" class="muted">正在加载分组选项...</p>
            <div class="group-add-row">
              <select v-model="groupToAdd" :disabled="!addableGroupOptions.length">
                <option value="">选择分组加入筛选</option>
                <option v-for="group in addableGroupOptions" :key="group.id" :value="group.id">{{ group.name }}</option>
              </select>
              <button type="button" class="secondary" :disabled="!groupToAdd" @click="addGroupFilter">加入</button>
            </div>
            <div class="group-options" v-if="groupFilterItems.length">
              <div v-for="group in groupFilterItems" :key="group.id" class="group-option">
                <span>{{ group.name }}</span>
                <div class="segmented">
                  <button type="button" :class="{ active: groupState(group.id) === 'include' }" @click="setGroupState(group.id, 'include')">选中</button>
                  <button type="button" :class="{ active: groupState(group.id) === 'exclude' }" @click="setGroupState(group.id, 'exclude')">不选中</button>
                  <button type="button" @click="setGroupState(group.id, 'any')">移除</button>
                </div>
              </div>
            </div>
            <p v-else-if="!groupsLoading" class="muted">暂无分组筛选；需要限制时先选择分组并加入。</p>
            <p v-if="!groupOptions.length && !groupsLoading" class="muted">暂无分组选项；查询账号后也会从当前列表补充分组。</p>
          </section>
          <p v-if="accountError" class="error">{{ accountError }}</p>
          <p v-if="activeAccountFilters" class="muted filter-summary">
            当前筛选：{{ activeAccountFilters }}
            <button type="button" class="mini" @click="copyText(activeAccountFilters, '筛选条件')">复制</button>
          </p>
          <div class="batch-toolbar">
            <span class="muted">已选择 {{ selectedAccountCount }} 个账号，OpenAI 留空默认模型为 gpt-5.4；检测会逐个账号执行并追加结果。</span>
            <label>测试模型<input v-model="batchTestForm.modelId" placeholder="留空使用 sub2api 默认测试模型" /></label>
            <label>提示词<input v-model="batchTestForm.prompt" placeholder="留空使用上游默认测试提示" /></label>
            <label>模式
              <select v-model="batchTestForm.mode">
                <option value="">默认</option>
                <option value="chat">chat</option>
                <option value="responses">responses</option>
                <option value="image">image</option>
              </select>
            </label>
            <button class="secondary" :disabled="batchTesting || selectedAccountCount === 0" @click="testSelectedAccounts">{{ batchTesting ? '检测中...' : '检测选中账号' }}</button>
            <button class="secondary" :disabled="selectedAccountCount === 0" @click="clearAccountSelection">清空选择</button>
          </div>
          <p v-if="batchTesting" class="muted">正在检测 {{ selectedAccountCount }} 个账号；每个账号完成后会更新一行结果。</p>
          <p v-if="batchTestError" class="error">{{ batchTestError }}</p>
          <p v-if="copyNotice" class="muted">{{ copyNotice }}</p>
          <p v-if="accountsLoading" class="muted">正在加载账号列表...</p>
          <div class="table-wrap">
            <table class="account-table">
              <thead>
                <tr>
                  <th><input type="checkbox" :checked="allVisibleSelected" @change="toggleAllVisibleAccounts" /></th>
                  <th>名称</th>
                  <th>平台/状态</th>
                  <th>分组</th>
                  <th>代理/调度</th>
                  <th>用量/最近使用</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="account in visibleAccounts" :key="String(account.id || accountName(account))">
                  <td><input type="checkbox" :checked="selectedAccountIds.has(accountID(account))" @change="toggleAccountSelection(account)" /></td>
                  <td>{{ accountName(account) }}</td>
                  <td>{{ [account.platform, account.status].filter(Boolean).join(' / ') || '未知' }}</td>
                  <td>{{ accountGroups(account) }}</td>
                  <td>{{ accountProxy(account) }} / {{ accountSchedule(account) }}</td>
                  <td>{{ accountUsage(account) }}<br><span class="muted">{{ formatDateTime(account.last_used_at) }}</span></td>
                  <td class="row-actions">
                    <button class="secondary" @click="openAccountDetail(account)">详情</button>
                    <button class="secondary" @click="copyText(account.id, '账号 ID')">复制 ID</button>
                    <button class="secondary" @click="copyText(accountName(account), '账号名称')">复制名称</button>
                  </td>
                </tr>
                <tr v-if="!visibleAccounts.length">
                  <td colspan="7" class="muted">{{ accountPager.loaded ? '没有匹配的账号。' : '请选择站点并查询账号。' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <section v-if="batchTestResults.length" class="batch-results">
            <div class="panel-head">
              <h2>批量检测结果</h2>
              <button class="secondary" @click="copyText(JSON.stringify(batchTestResults, null, 2), '检测结果')">复制结果</button>
            </div>
            <div class="table-wrap">
              <table class="account-table">
                <thead><tr><th>账号 ID</th><th>结果</th><th>模型</th><th>HTTP</th><th>耗时</th><th>摘要</th></tr></thead>
                <tbody>
                  <tr v-for="result in batchTestResults" :key="String(result.id)">
                    <td>{{ result.id }}</td>
                    <td>{{ result.ok ? '成功' : '失败' }}</td>
                    <td>{{ result.model || '未知' }}</td>
                    <td>{{ result.statusCode || '未知' }}</td>
                    <td>{{ result.durationMs ?? '未知' }} ms</td>
                    <td><pre class="inline-json">{{ result.message || result.error || '无响应内容' }}</pre></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
          <p v-if="accountPager.loaded && visibleAccounts.length !== accounts.length" class="muted">当前页本地筛选命中 {{ visibleAccounts.length }} / {{ accounts.length }} 条。</p>
          <div class="pager">
            <button class="secondary" :disabled="accountPager.page <= 1 || accountsLoading" @click="goPrevAccounts">上一页</button>
            <span class="muted">第 {{ accountPager.page }} 页<span v-if="accountTotalPages"> / 共 {{ accountTotalPages }} 页</span></span>
            <button class="secondary" :disabled="!hasNextAccountPage || accountsLoading" @click="goNextAccounts">下一页</button>
            <span v-if="accountPager.total" class="muted">共 {{ accountPager.total }} 条</span>
          </div>
        </section>
      </div>
    </section>

    <div v-if="showSiteModal" class="modal-mask">
      <form class="modal-card" @submit.prevent="saveSite">
        <h2>{{ editingSite ? '编辑站点' : '新增站点' }}</h2>
        <label>名称<input v-model="siteForm.name" required /></label>
        <label>基础地址<input v-model="siteForm.baseUrl" required /></label>
        <label>管理员 Key<input v-model="siteForm.adminKey" type="password" :required="!editingSite" autocomplete="off" /></label>
        <label>备注<input v-model="siteForm.note" /></label>
        <div class="check-row">
          <label><input v-model="siteForm.isDefault" type="checkbox" /> 默认站点</label>
          <label><input v-model="siteForm.enabled" type="checkbox" /> 启用</label>
        </div>
        <p v-if="siteError" class="error">{{ siteError }}</p>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="showSiteModal = false">取消</button>
          <button type="submit" :disabled="savingSite">{{ savingSite ? '保存中...' : '保存' }}</button>
        </div>
      </form>
    </div>

    <div v-if="showAccountModal && selectedAccount" class="modal-mask">
      <section class="modal-card detail-card">
        <h2>账号详情</h2>
        <p class="muted">来自当前列表结果，敏感字段已隐藏。</p>
        <div class="detail-sections">
          <section class="detail-section">
            <h3>基础信息</h3>
            <div class="detail-grid">
              <span>ID</span><strong>{{ selectedAccount.id || '未知' }}</strong>
              <span>名称</span><strong>{{ accountName(selectedAccount) }}</strong>
              <span>平台</span><strong>{{ selectedAccount.platform || '未知' }}</strong>
              <span>类型</span><strong>{{ selectedAccount.type || '未知' }}</strong>
              <span>状态</span><strong>{{ selectedAccount.status || '未知' }}</strong>
              <span>优先级</span><strong>{{ selectedAccount.priority ?? '未知' }}</strong>
              <span>并发</span><strong>{{ selectedAccount.concurrency ?? '未知' }}</strong>
              <span>最近使用</span><strong>{{ formatDateTime(selectedAccount.last_used_at) }}</strong>
            </div>
          </section>
          <section class="detail-section">
            <h3>分组与代理</h3>
            <div class="detail-grid">
              <span>分组</span><strong>{{ accountGroups(selectedAccount) }}</strong>
              <template v-for="row in accountProxyRows(selectedAccount)" :key="row.label">
                <span>{{ row.label }}</span><strong>{{ row.value }}</strong>
              </template>
            </div>
          </section>
          <section class="detail-section">
            <h3>调度状态</h3>
            <div class="detail-grid">
              <template v-for="row in accountScheduleRows(selectedAccount)" :key="row.label">
                <span>{{ row.label }}</span><strong>{{ row.value }}</strong>
              </template>
            </div>
          </section>
          <section class="detail-section">
            <h3>用量与错误</h3>
            <div class="detail-grid">
              <template v-for="row in accountUsageRows(selectedAccount)" :key="row.label">
                <span>{{ row.label }}</span><strong>{{ row.value }}</strong>
              </template>
              <span>错误信息</span><strong>{{ accountErrorText(selectedAccount) }}</strong>
            </div>
          </section>
        </div>
        <pre class="detail-json">{{ accountDetailJSON(selectedAccount) }}</pre>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="copyText(selectedAccount.id, '账号 ID')">复制 ID</button>
          <button type="button" class="secondary" @click="copyText(accountName(selectedAccount), '账号名称')">复制名称</button>
          <button type="button" class="secondary" @click="showAccountModal = false">关闭</button>
        </div>
      </section>
    </div>
  </main>
</template>

<style scoped>
:global(body) {
  margin: 0;
  min-height: 100vh;
  color: #e5e7eb;
  background: radial-gradient(circle at top left, rgba(124, 58, 237, 0.22), transparent 28%), #0b1020;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
.app-shell { min-height: 100vh; padding: 24px; }
.login-card, .panel, .topbar, .modal-card {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 22px;
  background: rgba(15, 23, 42, 0.88);
  box-shadow: 0 24px 80px rgba(0, 0, 0, 0.28);
}
.login-card { width: min(520px, 100%); margin: 12vh auto; padding: 28px; }
.dashboard { display: grid; gap: 18px; }
.topbar { display: flex; justify-content: space-between; gap: 18px; align-items: center; padding: 22px; }
.topbar-actions { display: flex; flex-wrap: wrap; gap: 10px; justify-content: flex-end; }
.overview-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 12px; }
.overview-card { padding: 16px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 18px; background: rgba(15, 23, 42, 0.7); }
.overview-card.wide { grid-column: span 2; }
.overview-card span { color: #94a3b8; font-size: 13px; }
.overview-card strong { display: block; margin-top: 8px; color: #fff; font-size: 22px; line-height: 1.2; }
.overview-card p { margin: 8px 0 0; font-size: 13px; }
.layout-grid { display: grid; grid-template-columns: minmax(320px, 420px) 1fr; gap: 18px; align-items: start; }
.panel { padding: 18px; }
.content-panel { min-height: 520px; }
.docs-panel { display: grid; gap: 14px; }
.docs-links { display: flex; flex-wrap: wrap; gap: 10px; }
.docs-reader { display: grid; gap: 12px; margin-top: 8px; }
.docs-reader pre { margin: 0; max-height: 620px; overflow: auto; padding: 16px; border-radius: 14px; background: rgba(2, 6, 23, 0.55); color: #dbeafe; line-height: 1.55; white-space: pre-wrap; }
.panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 14px; }
.eyebrow { margin: 0 0 8px; color: #c4b5fd; letter-spacing: 0.16em; text-transform: uppercase; font-size: 12px; }
h1, h2 { margin: 0; }
h3 { margin: 0 0 10px; font-size: 15px; }
.muted { color: #94a3b8; }
.error { color: #fecaca; }
.form-block, .filter-grid, .modal-card { display: grid; gap: 14px; }
.filter-grid { grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); align-items: end; }
.filter-note { display: flex; flex-wrap: wrap; gap: 8px; align-items: baseline; margin: 8px 0 12px; padding: 10px 12px; border-radius: 14px; background: rgba(37, 99, 235, 0.1); color: #cbd5e1; }
.filter-note.compact { margin-top: 0; }
.filter-note span { color: #94a3b8; font-size: 13px; }
.local-filter { display: grid; gap: 12px; margin: 14px 0; padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(15, 23, 42, 0.46); }
.group-filter-head { display: flex; flex-wrap: wrap; gap: 10px; align-items: baseline; justify-content: space-between; }
.group-add-row { display: grid; grid-template-columns: minmax(180px, 1fr) auto; gap: 10px; margin-top: 12px; align-items: center; }
.group-options { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 10px; margin-top: 12px; }
.group-option { display: flex; gap: 10px; align-items: center; justify-content: space-between; padding: 10px; border: 1px solid rgba(148, 163, 184, 0.13); border-radius: 12px; background: rgba(2, 6, 23, 0.22); }
.segmented { display: inline-flex; gap: 2px; padding: 3px; border-radius: 12px; background: rgba(2, 6, 23, 0.38); }
.segmented button { padding: 7px 9px; border-radius: 9px; background: transparent; color: #94a3b8; font-size: 12px; }
.segmented button.active { color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb); }
label { display: grid; gap: 7px; color: #cbd5e1; font-size: 14px; }
input, select {
  width: 100%; box-sizing: border-box; padding: 12px 13px; border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 12px; color: #f8fafc; background: rgba(15, 23, 42, 0.86); outline: none;
}
input[type="checkbox"] { width: auto; }
button {
  border: 0; border-radius: 12px; padding: 11px 14px; color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb);
  cursor: pointer; font-weight: 700;
}
button:disabled { opacity: 0.5; cursor: not-allowed; }
.button-link { display: inline-block; border-radius: 12px; padding: 10px 12px; color: #fff; font-weight: 700; text-decoration: none; }
.secondary { background: rgba(148, 163, 184, 0.14); color: #e2e8f0; }
.mini { margin-left: 8px; padding: 5px 8px; border-radius: 9px; background: rgba(148, 163, 184, 0.14); color: #e2e8f0; font-size: 12px; }
.danger { background: rgba(239, 68, 68, 0.16); color: #fecaca; }
.site-list, .account-list { display: grid; gap: 12px; }
.filter-summary { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.batch-toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 12px 0; padding: 12px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: 14px; background: rgba(2, 6, 23, 0.22); }
.batch-results { display: grid; gap: 10px; margin-top: 14px; }
.row-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.table-wrap { overflow-x: auto; margin-top: 12px; }
.account-table { width: 100%; min-width: 720px; border-collapse: collapse; }
.account-table th,
.account-table td { padding: 12px 10px; border-bottom: 1px solid rgba(148, 163, 184, 0.16); text-align: left; vertical-align: top; }
.account-table th { color: #cbd5e1; font-size: 14px; }
.account-table td:last-child { white-space: nowrap; }
.pager { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-top: 12px; }
.site-card, .account-card { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(15, 23, 42, 0.58); }
.site-card.active { border-color: rgba(196, 181, 253, 0.72); }
.site-title { display: flex; justify-content: space-between; gap: 8px; align-items: center; }
.pill { padding: 4px 8px; border-radius: 999px; background: rgba(196, 181, 253, 0.12); color: #ddd6fe; font-size: 12px; }
.actions, .modal-actions, .check-row { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-top: 12px; }
.compact-actions { margin-top: 10px; }
.modal-mask { position: fixed; inset: 0; display: grid; place-items: center; padding: 20px; background: rgba(2, 6, 23, 0.72); }
.modal-card { width: min(560px, 100%); padding: 22px; }
.detail-card { width: min(820px, 100%); max-height: calc(100vh - 56px); overflow: auto; }
.detail-sections { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; margin: 16px 0; }
.detail-section { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: 16px; background: rgba(2, 6, 23, 0.25); }
.detail-grid { display: grid; grid-template-columns: 110px 1fr; gap: 10px 14px; margin: 16px 0; }
.detail-section .detail-grid { margin: 0; }
.detail-grid span { color: #94a3b8; }
.detail-grid strong { overflow-wrap: anywhere; }
.detail-json { overflow: auto; max-height: 360px; padding: 14px; border-radius: 14px; background: rgba(2, 6, 23, 0.55); color: #dbeafe; font-size: 12px; line-height: 1.55; }
.inline-json { max-width: 520px; max-height: 160px; overflow: auto; margin: 0; white-space: pre-wrap; color: #dbeafe; font-size: 12px; }
.modal-actions { justify-content: flex-end; }
@media (max-width: 900px) {
  .layout-grid { grid-template-columns: 1fr; }
  .topbar { align-items: flex-start; flex-direction: column; }
  .overview-card.wide { grid-column: auto; }
}
</style>
