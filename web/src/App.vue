<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import ExpandablePanel from './components/ExpandablePanel.vue'
import ConcurrencyTable from './components/ConcurrencyTable.vue'
import StatsTable from './components/StatsTable.vue'

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
type JobRecord = Record<string, unknown>
type GroupState = 'any' | 'include' | 'exclude'
type ViewMode = 'stats' | 'accounts' | 'import' | 'jobs' | 'audit' | 'sites' | 'docs'
type ApiOptions = RequestInit & { timeoutMs?: number }
type ImportPreviewItem = Record<string, unknown>
type ImportPreview = {
  items: ImportPreviewItem[]
  warnings: string[]
  errors: string[]
  summary: Record<string, unknown>
  settings: Record<string, unknown>
}

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
const batchRefreshError = ref('')
const importError = ref('')
const accounts = ref<Account[]>([])
const groups = ref<Group[]>([])
const proxies = ref<Record<string, unknown>[]>([])
const batchTestResults = ref<Record<string, unknown>[]>([])
const batchRefreshResult = ref<Record<string, unknown> | null>(null)
const importPreview = ref<ImportPreview | null>(null)
const statistics = ref<Record<string, unknown> | null>(null)
const batchTestJob = ref<JobRecord | null>(null)
const recentJobs = ref<JobRecord[]>([])
const auditLogs = ref<Record<string, unknown>[]>([])
const importTemplates = ref<Record<string, unknown>[]>([])
const batchTestScroll = ref<HTMLElement | null>(null)
const batchRefreshScroll = ref<HTMLElement | null>(null)
const selectedAccountIds = ref<Set<string>>(new Set())
const activeSiteId = ref<number | null>(null)
const editingSite = ref<Site | null>(null)
const selectedAccount = ref<Account | null>(null)
const selectedBatchTestResult = ref<Record<string, unknown> | null>(null)
const showSiteModal = ref(false)
const showAccountModal = ref(false)
const showBatchTestModal = ref(false)
const loginLoading = ref(false)
const sitesLoading = ref(false)
const accountsLoading = ref(false)
const groupsLoading = ref(false)
const proxiesLoading = ref(false)
const savingSite = ref(false)
const docsLoading = ref(false)
const statsLoading = ref(false)
const userConcurrencyLoading = ref(false)
const accountConcurrencyLoading = ref(false)
const batchTesting = ref(false)
const batchRefreshing = ref(false)
const batchCollecting = ref(false)
const jobsLoading = ref(false)
const auditLoading = ref(false)
const importLoading = ref(false)
const importExecuting = ref(false)
const importTemplatesLoading = ref(false)
const activeView = ref<ViewMode>('stats')
const statsError = ref('')
const auditError = ref('')
const importTemplateName = ref('')
const importTemplateDeleteMode = ref(false)
const newImportModelTag = ref('')
const customImportModels = ref<string[]>([])
const importPreviewPage = ref(1)
const importPreviewPageSize = 10
const confirmDialog = reactive({
  open: false,
  title: '',
  message: '',
  detail: '',
  confirmText: '确认',
  cancelText: '取消',
  closeOnBackdrop: true,
  resolve: null as null | ((value: boolean) => void),
})
const batchTestTotal = ref(0)
const batchTestDone = ref(0)
const batchRefreshTotal = ref(0)
const batchRefreshDone = ref(0)
const batchCollectPage = ref(0)
const batchCollectTotalPages = ref(0)
const batchCollectFound = ref(0)
const batchResultFilter = ref<'all' | 'failed' | 'success' | 'pending'>('all')
const accountPageJump = ref('')
const accountQueryElapsedSeconds = ref(0)
const accountQuerySlow = ref(false)
const accountQueryNotice = ref('')
const groupPopoverAccount = ref<Account | null>(null)
const chartHoverIndex = ref<number | null>(null)
const groupPopoverPosition = reactive({ top: 0, left: 0 })
const filteredAccountIDsCache = new Map<string, number[]>()
const accountMetaCache = new Map<string, { name: string; note: string }>()
const accountAbortController = ref<AbortController | null>(null)
let accountQueryTimer: number | null = null
let accountQueryCancelled = false
let batchTestPollTimer: number | null = null

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
  upstreamGroup: '',
  sortBy: 'name',
  sortOrder: 'asc',
})

const batchTestForm = reactive({
  modelId: '',
  prompt: '',
  mode: '',
  delayMs: 0,
  jitterMs: 0,
  logResponses: false,
})

const importForm = reactive({
  text: '',
  filename: '',
  groups: [] as string[],
  proxyId: '',
  priority: '',
  concurrency: '',
  namePrefix: '',
  models: ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-image-2'] as string[],
})

const scheduleQuickFilter = ref('all')
const groupToAdd = ref('')
const groupFilterStates = reactive<Record<string, GroupState>>({})

const statsRange = reactive({
  preset: '24h',
  startDate: formatDateInput(addDays(new Date(), -1)),
  endDate: formatDateInput(new Date()),
  granularity: 'hour',
})

const accountPager = reactive({
  page: 1,
  pageSize: 10,
  total: 0,
  loaded: false,
})

const accountCache = new Map<string, { expiresAt: number; payload: any }>()
let accountRequestSeq = 0

const platformOptions = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
]

const accountTypeOptions = [
  { value: 'oauth', label: 'OAuth' },
  { value: 'setup-token', label: 'Setup Token' },
  { value: 'apikey', label: 'API Key' },
  { value: 'bedrock', label: 'AWS Bedrock' },
  { value: 'upstream', label: '对接上游' },
  { value: 'service-account', label: 'Service Account' },
]

const accountStatusOptions = [
  { value: 'active', label: '正常' },
  { value: 'inactive', label: '停用' },
  { value: 'error', label: '错误' },
  { value: 'rate_limited', label: '限流中' },
  { value: 'temp_unschedulable', label: '临时不可调度' },
  { value: 'unschedulable', label: '不可调度' },
]

const privacyModeOptions = [
  { value: '__unset__', label: '未设置' },
  { value: 'training_off', label: '已关闭训练数据共享' },
  { value: 'training_set_cf_blocked', label: '被 Cloudflare 拦截，训练可能仍开启' },
  { value: 'training_set_failed', label: '关闭训练数据共享失败' },
  { value: 'privacy_set', label: '已关闭遥测和营销邮件' },
  { value: 'privacy_set_failed', label: '隐私设置失败' },
]

const accountTotalPages = computed(() => (accountPager.total ? Math.ceil(accountPager.total / accountPager.pageSize) : 0))
const accountPageButtons = computed(() => {
  if (!accountTotalPages.value) return []
  const start = Math.max(1, accountPager.page - 2)
  const end = Math.min(accountTotalPages.value, accountPager.page + 2)
  const pages: number[] = []
  for (let page = start; page <= end; page += 1) pages.push(page)
  return pages
})
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

const upstreamGroupOptions = computed(() => [{ id: 'ungrouped', name: '未分组' }, ...groupOptions.value])

const proxyOptions = computed(() => proxies.value.map((proxy) => ({ id: String(proxy.id || ''), name: String(proxy.name || proxy.id || '未命名代理') })).filter((proxy) => proxy.id))

const importModelOptions = computed(() => {
  const values = new Set(['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-image-2'])
  customImportModels.value.forEach((model) => values.add(model))
  return Array.from(values)
})

const importGroupOptions = computed(() => groupOptions.value)

const selectedImportGroupNames = computed(() => importForm.groups.map((id) => groupOptions.value.find((group) => group.id === id)?.name || `分组 #${id}`))

const selectedImportProxyName = computed(() => proxyOptions.value.find((proxy) => proxy.id === importForm.proxyId)?.name || '')

const pagedImportPreviewItems = computed(() => {
  return importPreview.value?.items || []
})

const importPreviewTotalPages = computed(() => Math.max(1, Math.ceil(Number(importPreview.value?.summary?.total || 0) / importPreviewPageSize)))

const selectedAccountCount = computed(() => selectedAccountIds.value.size)

const failedBatchTestIDs = computed(() => batchTestResults.value.filter((result) => !isBatchResultPending(result) && result.ok !== true).map((result) => Number(result.id)).filter((id) => Number.isFinite(id) && id > 0))

const batchTestFailureGroups = computed(() => {
  const groups = new Map<string, number[]>()
  batchTestResults.value.forEach((result) => {
    if (isBatchResultPending(result) || result.ok === true) return
    const id = Number(result.id)
    if (!Number.isFinite(id) || id <= 0) return
    const hint = String(batchResultHint(result) || '异常')
    groups.set(hint, [...(groups.get(hint) || []), id])
  })
  return Array.from(groups.entries())
    .map(([hint, ids]) => ({ hint, ids, count: ids.length }))
    .sort((a, b) => b.count - a.count || a.hint.localeCompare(b.hint, 'zh-CN'))
})

const filteredIDCacheHit = computed(() => filteredAccountIDsCache.has(currentFilteredIDsCacheKey()))

const batchTestProgress = computed(() => progressPercent(batchTestDone.value, batchTestTotal.value))

const batchRefreshProgress = computed(() => progressPercent(batchRefreshDone.value, batchRefreshTotal.value))

const displayedBatchResults = computed(() => batchTestResults.value.filter((result) => {
  if (batchResultFilter.value === 'failed') return !isBatchResultPending(result) && result.ok !== true
  if (batchResultFilter.value === 'success') return result.ok === true
  if (batchResultFilter.value === 'pending') return isBatchResultPending(result)
  return true
}))

const allVisibleSelected = computed(() => visibleAccounts.value.length > 0 && visibleAccounts.value.every((account) => selectedAccountIds.value.has(accountID(account))))

const activeSite = computed(() => sites.value.find((site) => site.id === activeSiteId.value) || null)
const statisticsSnapshot = computed(() => unwrapAPIData(statistics.value?.snapshot))
const statisticsStats = computed(() => {
  const snapshotStats = statisticsSnapshot.value.stats
  if (snapshotStats && typeof snapshotStats === 'object') return snapshotStats as Record<string, unknown>
  return unwrapAPIData(statistics.value?.stats)
})
const statisticsTrend = computed(() => Array.isArray(statisticsSnapshot.value.trend) ? statisticsSnapshot.value.trend as Record<string, unknown>[] : [])
const statisticsModels = computed(() => Array.isArray(statisticsSnapshot.value.models) ? statisticsSnapshot.value.models as Record<string, unknown>[] : [])
const statisticsRanking = computed(() => {
  const ranking = unwrapAPIData(statistics.value?.ranking)
  return Array.isArray(ranking.ranking) ? ranking.ranking as Record<string, unknown>[] : []
})
const userConcurrency = computed(() => unwrapAPIData(statistics.value?.userConcurrency))
const opsConcurrency = computed(() => unwrapAPIData(statistics.value?.opsConcurrency))
const currentUserConcurrency = computed(() => {
  const users = userConcurrency.value.user
  if (!users || typeof users !== 'object') return []
  return Object.values(users as Record<string, Record<string, unknown>>)
    .filter((user) => Number(user.current_in_use || 0) > 0 || Number(user.waiting_in_queue || 0) > 0)
    .sort((a, b) => Number(b.current_in_use || 0) - Number(a.current_in_use || 0))
})
const currentAccountConcurrency = computed(() => {
  const accounts = opsConcurrency.value.account
  if (!accounts || typeof accounts !== 'object') return []
  return Object.values(accounts as Record<string, Record<string, unknown>>)
    .filter((account) => Number(account.current_in_use || 0) > 0 || Number(account.waiting_in_queue || 0) > 0)
    .sort((a, b) => Number(b.current_in_use || 0) - Number(a.current_in_use || 0))
})
const accountConcurrencySummary = computed(() => currentAccountConcurrency.value.reduce((acc, account) => {
  acc.current += Number(account.current_in_use || 0)
  acc.waiting += Number(account.waiting_in_queue || 0)
  return acc
}, { current: 0, waiting: 0 }))
const userConcurrencySummary = computed(() => currentUserConcurrency.value.reduce((acc, user) => {
  acc.current += Number(user.current_in_use || 0)
  acc.waiting += Number(user.waiting_in_queue || 0)
  return acc
}, { current: 0, waiting: 0 }))
const topModels = computed(() => statisticsModels.value.slice(0, 8))
const recentTrend = computed(() => statisticsTrend.value.slice(-12))
const maxRankingCost = computed(() => Math.max(1, ...statisticsRanking.value.map((user) => Number(user.actual_cost || 0))))
const maxTrendTokens = computed(() => Math.max(1, ...recentTrend.value.map((point) => Number(point.total_tokens || 0))))
const maxTrendRequests = computed(() => Math.max(1, ...recentTrend.value.map((point) => Number(point.requests || 0))))
const chartHoverPoint = computed(() => chartHoverIndex.value === null ? null : recentTrend.value[chartHoverIndex.value] || null)

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

async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const { timeoutMs, signal, ...fetchOptions } = options
  const controller = new AbortController()
  const timeout = typeof timeoutMs === 'number' && timeoutMs > 0 ? window.setTimeout(() => controller.abort(), timeoutMs) : null
  if (signal) {
    if (signal.aborted) controller.abort()
    else signal.addEventListener('abort', () => controller.abort(), { once: true })
  }
  try {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(fetchOptions.headers || {}) },
      ...fetchOptions,
      signal: controller.signal,
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || '请求失败')
    return data as T
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new Error('请求超时，请稍后重试或缩小筛选范围')
    }
    if (error instanceof TypeError) {
      throw new Error('网络连接中断，请刷新后重试')
    }
    throw error
  } finally {
    if (timeout !== null) window.clearTimeout(timeout)
  }
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
  if (authed.value) {
    await loadSites()
    await loadRecentJobs()
  }
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
  activeView.value = 'stats'
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

async function showStats() {
  activeView.value = 'stats'
  await loadStatistics()
}

async function showJobs() {
  activeView.value = 'jobs'
  await loadRecentJobs()
}

async function loadStatistics() {
  statsError.value = ''
  if (!activeSiteId.value) {
    statsError.value = '请先选择站点'
    return
  }
  statsLoading.value = true
  try {
    const params = new URLSearchParams({
      start_date: statsRange.startDate,
      end_date: statsRange.endDate,
      granularity: statsRange.granularity,
    })
    const statsPayload = await api<Record<string, unknown>>(`api/sites/${activeSiteId.value}/statistics?${params.toString()}`)
    const stats = unwrapAPIData(statsPayload.stats)
    statistics.value = {
      ...statsPayload,
      stats: {
        ...stats,
        active_status_accounts: statsPayload.activeStatusAccounts ?? stats.normal_accounts,
      },
    }
  } catch (error) {
    statsError.value = error instanceof Error ? error.message : '加载统计失败'
  } finally {
    statsLoading.value = false
  }
}

async function refreshUserConcurrency() {
  statsError.value = ''
  if (!activeSiteId.value) {
    statsError.value = '请先选择站点'
    return
  }
  userConcurrencyLoading.value = true
  try {
    const payload = await api<Record<string, unknown>>(`api/sites/${activeSiteId.value}/statistics/user-concurrency`)
    statistics.value = { ...(statistics.value || {}), ...payload, userConcurrencyError: undefined }
  } catch (error) {
    statistics.value = { ...(statistics.value || {}), userConcurrencyError: error instanceof Error ? error.message : '刷新用户并发失败' }
  } finally {
    userConcurrencyLoading.value = false
  }
}

async function refreshAccountConcurrency() {
  statsError.value = ''
  if (!activeSiteId.value) {
    statsError.value = '请先选择站点'
    return
  }
  accountConcurrencyLoading.value = true
  try {
    const payload = await api<Record<string, unknown>>(`api/sites/${activeSiteId.value}/statistics/account-concurrency`)
    statistics.value = { ...(statistics.value || {}), ...payload, opsConcurrencyError: undefined }
  } catch (error) {
    statistics.value = { ...(statistics.value || {}), opsConcurrencyError: error instanceof Error ? error.message : '刷新账号并发失败' }
  } finally {
    accountConcurrencyLoading.value = false
  }
}

function addDays(date: Date, days: number) {
  const result = new Date(date)
  result.setDate(result.getDate() + days)
  return result
}

function formatDateInput(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

function setStatsPreset(preset: string) {
  const now = new Date()
  statsRange.preset = preset
  if (preset === '24h') {
    statsRange.startDate = formatDateInput(addDays(now, -1))
    statsRange.granularity = 'hour'
  } else if (preset === '30d') {
    statsRange.startDate = formatDateInput(addDays(now, -30))
    statsRange.granularity = 'day'
  } else {
    statsRange.startDate = formatDateInput(addDays(now, -7))
    statsRange.granularity = 'day'
  }
  statsRange.endDate = formatDateInput(now)
  loadStatistics()
}

function applyStatsRange() {
  statsRange.preset = 'custom'
  loadStatistics()
}

async function loadSites() {
  siteError.value = ''
  sitesLoading.value = true
  try {
    const data = await api<{ items: Site[] }>('api/sites')
    sites.value = data.items || []
    if (!activeSiteId.value && sites.value.length) {
      const defaultSite = sites.value.find((site) => site.isDefault)
      if (defaultSite) {
        activeSiteId.value = defaultSite.id
        await loadGroups()
        await loadProxies()
        activeView.value = 'stats'
        await loadStatistics()
      } else {
        activeView.value = 'sites'
      }
    } else if (!sites.value.length) {
      activeView.value = 'sites'
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
  const ok = await askConfirm({ title: '删除站点？', message: `确定删除站点「${site.name}」吗？`, confirmText: '删除', closeOnBackdrop: false })
  if (!ok) return
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
  if (accountsLoading.value) cancelAccountQuery()
  activeSiteId.value = site.id
  accounts.value = []
  selectedAccountIds.value = new Set()
  accountPager.loaded = false
  accountPager.total = 0
  accountPager.page = 1
  accountQueryNotice.value = ''
  accountError.value = ''
  accountCache.clear()
  filteredAccountIDsCache.clear()
  groups.value = []
  proxies.value = []
  resetGroupFilters()
  loadGroups()
  loadProxies()
  if (activeView.value === 'stats') loadStatistics()
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

function normalizeProxies(payload: any): Record<string, unknown>[] {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload.data)) return payload.data
  if (payload.data && Array.isArray(payload.data.items)) return payload.data.items
  if (Array.isArray(payload.items)) return payload.items
  return []
}

async function loadProxies() {
  if (!activeSiteId.value) return
  proxiesLoading.value = true
  try {
    const payload = await api<any>(`api/sites/${activeSiteId.value}/proxies`)
    proxies.value = normalizeProxies(payload)
  } catch {
    proxies.value = []
  } finally {
    proxiesLoading.value = false
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

function buildAccountQuery(page: number, pageSize: number) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  Object.entries(accountFilters).forEach(([key, value]) => {
    const trimmed = filterQueryValue(key, value)
    if (!trimmed) return
    if (key === 'privacyMode') {
      params.set('privacy_mode', trimmed)
      return
    }
    if (key === 'upstreamGroup') {
      params.set('group', trimmed)
      return
    }
    params.set(key, trimmed)
  })
  return params
}

function currentFilteredIDsCacheKey() {
  const params = buildAccountQuery(1, 100)
  const localFilters = Object.entries(groupFilterStates)
    .filter(([, state]) => state !== 'any')
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([id, state]) => `${id}:${state}`)
    .join(',')
  return JSON.stringify({ site: activeSiteId.value, upstream: params.toString(), schedule: scheduleQuickFilter.value, groups: localFilters })
}

function payloadTotal(payload: any) {
  return Number(payload.total || payload.data?.total || 0)
}

function optionLabel(options: { value: string; label: string }[], value: unknown) {
  const text = String(value || '')
  if (!text) return '未知'
  return options.find((option) => option.value === text)?.label || text
}

function platformLabel(value: unknown) {
  return optionLabel(platformOptions, value)
}

function accountTypeLabel(value: unknown) {
  return optionLabel(accountTypeOptions, value)
}

function statusLabel(value: unknown) {
  return optionLabel(accountStatusOptions, value)
}

function privacyModeLabel(value: unknown) {
  return optionLabel(privacyModeOptions, value)
}

function unwrapAPIData(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') return {}
  const raw = value as Record<string, unknown>
  if (raw.data && typeof raw.data === 'object') return raw.data as Record<string, unknown>
  return raw
}

function statValue(source: Record<string, unknown>, key: string) {
  const value = source[key]
  if (value === undefined || value === null || value === '') return '暂无'
  if (typeof value === 'number') return value.toLocaleString('zh-CN')
  return String(value)
}

function statisticsActiveAccounts() {
  return statValue({ value: statistics.value?.activeStatusAccounts ?? statisticsStats.value.normal_accounts }, 'value')
}

function compactNumber(value: unknown) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return '暂无'
  const abs = Math.abs(numeric)
  if (abs >= 1_000_000_000) return `${(numeric / 1_000_000_000).toFixed(2)}B`
  if (abs >= 1_000_000) return `${(numeric / 1_000_000).toFixed(2)}M`
  if (abs >= 1_000) return `${(numeric / 1_000).toFixed(2)}K`
  return numeric.toLocaleString('zh-CN')
}

function tokenValue(source: Record<string, unknown>, key: string) {
  return compactNumber(source[key])
}

function statCost(source: Record<string, unknown>, key: string) {
  const value = Number(source[key])
  if (!Number.isFinite(value)) return '暂无'
  return `$${value.toFixed(2)}`
}

function refreshTimeLabel() {
  return formatDateTime(statisticsStats.value.stats_updated_at || statisticsSnapshot.value.generated_at)
}

function userDisplayName(user: Record<string, unknown>) {
  return String(user.username || user.email || user.user_email || `用户 #${user.user_id || '未知'}`)
}

function chartPoints(rows: Record<string, unknown>[], key: string) {
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

function chartPath(rows: Record<string, unknown>[], key: string) {
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

function chartX(index: number, rows = recentTrend.value) {
  if (rows.length <= 1) return 260
  return index / (rows.length - 1) * 520
}

function chartY(row: Record<string, unknown>, key: string) {
  const max = key === 'requests' ? maxTrendRequests.value : maxTrendTokens.value
  return 170 - Number(row[key] || 0) / max * 170
}

function chartAxisLabel(value: number) {
  return compactNumber(value)
}

function handleChartMove(event: MouseEvent) {
  if (!recentTrend.value.length) return
  const rect = (event.currentTarget as SVGElement).getBoundingClientRect()
  const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width))
  chartHoverIndex.value = Math.round(ratio * (recentTrend.value.length - 1))
}

function clearChartHover() {
  chartHoverIndex.value = null
}

function rankingWidth(user: Record<string, unknown>) {
  return usagePercentWidth(Number(user.actual_cost || 0) / maxRankingCost.value * 100)
}

function concurrencyPercent(item: Record<string, unknown>) {
  const current = Number(item.current_in_use || item.current_concurrency || 0)
  const max = Number(item.max_capacity || item.concurrency || 0)
  if (!Number.isFinite(current) || !Number.isFinite(max) || max <= 0) return 0
  return usagePercentWidth(current / max * 100)
}

function accountConcurrencyName(account: Record<string, unknown>) {
  return String(account.account_name || `账号 #${account.account_id || '未知'}`)
}

function accountConcurrencyCapacityTitle(account: Record<string, unknown>) {
  return `并发: ${statValue(account, 'current_in_use')}\n上限: ${statValue(account, 'max_capacity')}\n排队: ${statValue(account, 'waiting_in_queue')}`
}

function userConcurrencyCapacityTitle(user: Record<string, unknown>) {
  return `并发: ${statValue(user, 'current_in_use')}\n上限: ${statValue(user, 'max_capacity')}\n排队: ${statValue(user, 'waiting_in_queue')}`
}

function accountStatusTagClass(key: string) {
  if (key === 'ratelimit_accounts') return 'status-tag status-tag-warn'
  if (key === 'error_accounts') return 'status-tag status-tag-danger'
  if (key === 'overload_accounts') return 'status-tag status-tag-overload'
  return 'status-tag status-tag-ok'
}

const activeAccountFilters = computed(() => {
  const parts = [
    accountFilters.search && `搜索: ${accountFilters.search}`,
    accountFilters.platform && `平台: ${platformLabel(accountFilters.platform)}`,
    accountFilters.status && `状态: ${statusLabel(accountFilters.status)}`,
    accountFilters.type && `类型: ${accountTypeLabel(accountFilters.type)}`,
    accountFilters.privacyMode && `隐私: ${privacyModeLabel(accountFilters.privacyMode)}`,
    accountFilters.upstreamGroup && `上游分组: ${accountFilters.upstreamGroup}`,
    accountFilters.sortBy !== 'name' && `排序: ${accountFilters.sortBy}`,
    accountFilters.sortOrder !== 'asc' && `方向: ${accountFilters.sortOrder}`,
    scheduleQuickFilter.value !== 'all' && `调度: ${scheduleQuickFilter.value}`,
    ...activeGroupFilters(),
  ].filter(Boolean)
  return parts.join(' · ')
})

async function loadAccounts(options: { force?: boolean } = {}) {
  accountError.value = ''
  accountQueryNotice.value = ''
  if (!activeSiteId.value) {
    accountError.value = '请先选择站点'
    return
  }
  const requestSeq = ++accountRequestSeq
  const params = buildAccountQuery(accountPager.page, accountPager.pageSize)
  const cacheKey = `${activeSiteId.value}?${params.toString()}`
  const cached = accountCache.get(cacheKey)
  if (!options.force && cached && cached.expiresAt > Date.now()) {
    accounts.value = normalizeAccounts(cached.payload)
    rememberAccountMeta(accounts.value)
    accountPager.total = payloadTotal(cached.payload)
    accountPager.loaded = true
    accountQueryNotice.value = '已使用 8 秒内缓存结果'
    return
  }
  const startedAt = Date.now()
  stopAccountQueryTimer()
  accountAbortController.value?.abort()
  accountQueryCancelled = false
  accountQueryElapsedSeconds.value = 0
  accountQuerySlow.value = false
  const controller = new AbortController()
  accountAbortController.value = controller
  accountQueryTimer = window.setInterval(() => {
    accountQueryElapsedSeconds.value += 1
    if (accountQueryElapsedSeconds.value >= 5) accountQuerySlow.value = true
  }, 1000)
  accountsLoading.value = true
  try {
    const payload = await api<any>(`api/sites/${activeSiteId.value}/accounts?${params.toString()}`, { signal: controller.signal })
    if (requestSeq !== accountRequestSeq) return
    accountCache.set(cacheKey, { expiresAt: Date.now() + 8000, payload })
    accounts.value = normalizeAccounts(payload)
    rememberAccountMeta(accounts.value)
    accountPager.total = payloadTotal(payload)
    accountPager.loaded = true
    accountQueryNotice.value = `查询完成，用时 ${formatDuration(Date.now() - startedAt)}`
  } catch (error) {
    if (requestSeq !== accountRequestSeq) return
    if (accountQueryCancelled) {
      accountError.value = '已取消本次账号查询'
      return
    }
    accountError.value = error instanceof Error ? error.message : '查询账号失败'
  } finally {
    if (requestSeq === accountRequestSeq) {
      accountsLoading.value = false
      accountAbortController.value = null
      accountQuerySlow.value = false
      stopAccountQueryTimer()
    }
  }
}

async function loadRecentJobs() {
  jobsLoading.value = true
  try {
    const payload = await api<{ items: JobRecord[] }>('api/jobs')
    recentJobs.value = (payload.items || []).slice(0, 10)
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '加载任务失败'
  } finally {
    jobsLoading.value = false
  }
}

async function loadAuditLogs() {
  auditError.value = ''
  auditLoading.value = true
  try {
    const payload = await api<{ items: Record<string, unknown>[] }>('api/audit-logs')
    auditLogs.value = payload.items || []
  } catch (error) {
    auditError.value = error instanceof Error ? error.message : '加载审计日志失败'
  } finally {
    auditLoading.value = false
  }
}

function showAudit() {
  activeView.value = 'audit'
  loadAuditLogs()
}

function showImport() {
  activeView.value = 'import'
  loadImportTemplates()
}

async function loadImportTemplates() {
  importTemplatesLoading.value = true
  try {
    const payload = await api<{ items: Record<string, unknown>[] }>('api/import-templates')
    importTemplates.value = payload.items || []
  } catch (error) {
    importError.value = error instanceof Error ? error.message : '加载导入模板失败'
  } finally {
    importTemplatesLoading.value = false
  }
}

function askConfirm(options: { title: string; message: string; detail?: string; confirmText?: string; cancelText?: string; closeOnBackdrop?: boolean }) {
  confirmDialog.open = true
  confirmDialog.title = options.title
  confirmDialog.message = options.message
  confirmDialog.detail = options.detail || ''
  confirmDialog.confirmText = options.confirmText || '确认'
  confirmDialog.cancelText = options.cancelText || '取消'
  confirmDialog.closeOnBackdrop = options.closeOnBackdrop !== false
  return new Promise<boolean>((resolve) => {
    confirmDialog.resolve = resolve
  })
}

function resolveConfirm(value: boolean) {
  confirmDialog.open = false
  confirmDialog.resolve?.(value)
  confirmDialog.resolve = null
}

function closeConfirmFromBackdrop() {
  if (confirmDialog.closeOnBackdrop) resolveConfirm(false)
}

async function saveImportTemplate() {
  const name = importTemplateName.value.trim()
  if (!name) {
    importError.value = '请填写模板名称'
    return
  }
  const existing = importTemplates.value.find((template) => String(template.name || '') === name)
  if (existing) {
    const ok = await askConfirm({
      title: '覆盖导入模板？',
      message: `已存在同名模板「${name}」，将删除旧模板并保存当前设置。`,
      detail: importTemplateDiff(existing),
      confirmText: '覆盖保存',
      closeOnBackdrop: false,
    })
    if (!ok) return
    await api(`api/import-templates/${existing.id}`, { method: 'DELETE' })
  }
  try {
    await api('api/import-templates', {
      method: 'POST',
      body: JSON.stringify({ name, siteId: activeSiteId.value, template: importPreviewSettings() }),
    })
    importTemplateName.value = ''
    await loadImportTemplates()
  } catch (error) {
    importError.value = error instanceof Error ? error.message : '保存导入模板失败'
  }
}

async function deleteImportTemplate(template: Record<string, unknown>) {
  const id = Number(template.id)
  if (!id) return
  const ok = await askConfirm({ title: '删除导入模板？', message: `确定删除模板「${template.name || id}」吗？`, confirmText: '删除', closeOnBackdrop: false })
  if (!ok) return
  try {
    await api(`api/import-templates/${id}`, { method: 'DELETE' })
    await loadImportTemplates()
  } catch (error) {
    importError.value = error instanceof Error ? error.message : '删除导入模板失败'
  }
}

function applyImportTemplate(template: Record<string, unknown>) {
  const data = (template.template || {}) as Record<string, unknown>
  importTemplateName.value = String(template.name || '')
  ;(['priority', 'concurrency', 'namePrefix'] as const).forEach((key) => {
    importForm[key] = String(data[key] || '')
  })
  importForm.proxyId = String(data.proxyId || data.proxy || '')
  importForm.groups = Array.isArray(data.groupIds) ? data.groupIds.map(String) : Array.isArray(data.groups) ? data.groups.map(String) : []
  importForm.models = Array.isArray(data.models) ? data.models.map(String) : importForm.models
  customImportModels.value = importForm.models.filter((model) => !['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-image-2'].includes(model))
}

function importTemplateDiff(template: Record<string, unknown>) {
  const before = JSON.stringify(template.template || {}, null, 2)
  const after = JSON.stringify(importPreviewSettings(), null, 2)
  return `旧设置:\n${before}\n\n新设置:\n${after}`
}

function toggleImportGroup(group: string) {
  const index = importForm.groups.indexOf(group)
  if (index >= 0) importForm.groups.splice(index, 1)
  else importForm.groups.push(group)
}

function toggleImportModel(model: string) {
  const index = importForm.models.indexOf(model)
  if (index >= 0) importForm.models.splice(index, 1)
  else importForm.models.push(model)
}

function addImportModelTag() {
  const values = newImportModelTag.value.split(/[,\s]+/).map((item) => item.trim()).filter(Boolean)
  values.forEach((value) => {
    if (!customImportModels.value.includes(value) && !['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-image-2'].includes(value)) customImportModels.value.push(value)
    if (!importForm.models.includes(value)) importForm.models.push(value)
  })
  newImportModelTag.value = ''
}

function removeImportModelTag(model: string) {
  customImportModels.value = customImportModels.value.filter((item) => item !== model)
  importForm.models = importForm.models.filter((item) => item !== model)
}

async function previewImport(page = 1) {
  importError.value = ''
  importPreview.value = null
  if (!activeSiteId.value) {
    importError.value = '请先选择站点'
    return
  }
  if (!importForm.text.trim()) {
    importError.value = '请先粘贴账号内容或选择文件'
    return
  }
  importLoading.value = true
  try {
    importPreview.value = await api<ImportPreview>(`api/sites/${activeSiteId.value}/imports/preview`, {
      method: 'POST',
      body: JSON.stringify({
        text: importForm.text,
        filename: importForm.filename,
        settings: importPreviewSettings(),
        limit: importPreviewPageSize,
        offset: (page - 1) * importPreviewPageSize,
      }),
    })
    importPreviewPage.value = page
  } catch (error) {
    importError.value = error instanceof Error ? error.message : '生成导入预览失败'
  } finally {
    importLoading.value = false
  }
}

async function executeImportAccounts() {
  importError.value = ''
  if (!activeSiteId.value || !activeSite.value) {
    importError.value = '请先选择站点'
    return
  }
  if (!importPreview.value) {
    importError.value = '请先生成预览'
    return
  }
  if (Number(importPreview.value.summary?.invalid || 0) > 0) {
    importError.value = '预览存在需修正条目，请修正后再导入'
    return
  }
  const total = Number(importPreview.value.summary?.recognized || importPreview.value.summary?.total || 0)
  if (total <= 0) {
    importError.value = '没有可导入账号'
    return
  }
  const site = activeSite.value
  const detail = [
    `站点：${site.name}`,
    `地址：${site.baseUrl}`,
    `导入数量：${total}`,
    `分组：${selectedImportGroupNames.value.join(' / ') || '使用上游默认分组'}`,
    `代理：${selectedImportProxyName.value || '不指定'}`,
    `模型：${importForm.models.join(' / ') || '保持原设置'}`,
    `优先级/并发：${importForm.priority || '默认'} / ${importForm.concurrency || '默认'}`,
  ].join('\n')
  const ok = await askConfirm({
    title: '确认导入账号？',
    message: '此操作会写入下列 sub2api 站点，请确认站点信息无误。',
    detail,
    confirmText: '确认导入',
    closeOnBackdrop: false,
  })
  if (!ok) return
  importExecuting.value = true
  try {
    const job = await api<JobRecord>(`api/sites/${activeSiteId.value}/imports/accounts`, {
      method: 'POST',
      body: JSON.stringify({
        text: importForm.text,
        filename: importForm.filename,
        settings: importExecutionSettings(),
        confirmation: { confirmed: true, siteId: site.id, siteName: site.name, siteBaseUrl: site.baseUrl },
      }),
    })
    await loadRecentJobs()
    activeView.value = 'jobs'
    await openJobResult(job)
  } catch (error) {
    importError.value = error instanceof Error ? error.message : '提交导入任务失败'
  } finally {
    importExecuting.value = false
  }
}

function importPreviewSettings() {
  const settings: Record<string, unknown> = {}
  ;(['priority', 'concurrency', 'namePrefix'] as const).forEach((key) => {
    const value = String(importForm[key] || '').trim()
    if (value) settings[key] = value
  })
  if (importForm.proxyId) settings.proxyId = Number(importForm.proxyId)
  if (importForm.groups.length) settings.groupIds = importForm.groups.map(Number).filter((id) => Number.isFinite(id) && id > 0)
  if (importForm.groups.length) settings.groups = selectedImportGroupNames.value
  if (selectedImportProxyName.value) settings.proxy = selectedImportProxyName.value
  if (importForm.models.length) settings.models = importForm.models
  return settings
}

function importExecutionSettings() {
  const settings: Record<string, unknown> = {}
  const priorityText = String(importForm.priority || '').trim()
  const concurrencyText = String(importForm.concurrency || '').trim()
  const priority = Number(priorityText)
  const concurrency = Number(concurrencyText)
  if (String(importForm.namePrefix || '').trim()) settings.namePrefix = String(importForm.namePrefix).trim()
  if (priorityText && Number.isFinite(priority)) settings.priority = priority
  if (concurrencyText && Number.isFinite(concurrency)) settings.concurrency = concurrency
  if (importForm.proxyId) settings.proxyId = Number(importForm.proxyId)
  if (importForm.groups.length) settings.groupIds = importForm.groups.map(Number).filter((id) => Number.isFinite(id) && id > 0)
  if (importForm.models.length) settings.models = importForm.models
  return settings
}

async function handleImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importError.value = ''
  if (file.size > 2 * 1024 * 1024) {
    importError.value = '文件不能超过 2 MiB'
    input.value = ''
    return
  }
  importForm.filename = file.name
  importForm.text = await file.text()
}

function clearImportPreview() {
  importPreview.value = null
  importError.value = ''
  Object.assign(importForm, { text: '', filename: '', groups: [], proxyId: '', priority: '', concurrency: '', namePrefix: '', models: ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-image-2'] })
  customImportModels.value = []
  importTemplateName.value = ''
  importTemplateDeleteMode.value = false
  importPreviewPage.value = 1
}

function stopAccountQueryTimer() {
  if (accountQueryTimer !== null) {
    window.clearInterval(accountQueryTimer)
    accountQueryTimer = null
  }
}

function cancelAccountQuery() {
  if (!accountsLoading.value) return
  accountQueryCancelled = true
  accountAbortController.value?.abort()
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} 秒`
}

function reloadAccountsFromUpstream() {
  accountCache.clear()
  filteredAccountIDsCache.clear()
  loadAccounts({ force: true })
}

function submitAccountFilters() {
  accountPager.page = 1
  reloadAccountsFromUpstream()
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

function goFirstAccounts() {
  if (accountPager.page <= 1) return
  accountPager.page = 1
  loadAccounts()
}

function goLastAccounts() {
  if (!accountTotalPages.value || accountPager.page >= accountTotalPages.value) return
  accountPager.page = accountTotalPages.value
  loadAccounts()
}

function goAccountPage(page: number) {
  if (!Number.isFinite(page)) return
  const target = Math.trunc(page)
  if (target <= 0) return
  if (accountTotalPages.value && target > accountTotalPages.value) return
  if (target === accountPager.page) return
  accountPager.page = target
  loadAccounts()
}

function jumpToAccountPage() {
  const target = Number(String(accountPageJump.value).trim())
  if (!Number.isFinite(target)) return
  accountPageJump.value = ''
  goAccountPage(target)
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

function rememberAccountMeta(items: Account[]) {
  items.forEach((account) => {
    const id = accountID(account)
    if (!id) return
    accountMetaCache.set(id, { name: accountName(account), note: accountNote(account) })
  })
}

function accountMetaForIDs(ids: number[]) {
  const meta: Record<string, { name: string; note: string }> = {}
  ids.forEach((id) => {
    const value = accountMetaCache.get(String(id))
    if (value) meta[String(id)] = value
  })
  return meta
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

async function selectAllFilteredAccounts() {
  batchTestError.value = ''
  if (!activeSiteId.value) {
    batchTestError.value = '请先选择站点'
    return []
  }
  const cacheKey = currentFilteredIDsCacheKey()
  const cachedIDs = filteredAccountIDsCache.get(cacheKey)
  if (cachedIDs) {
    selectedAccountIds.value = new Set(cachedIDs.map((id) => String(id)))
    return cachedIDs
  }
  batchCollecting.value = true
  batchCollectPage.value = 0
  batchCollectTotalPages.value = 0
  batchCollectFound.value = 0
  const ids = new Set<string>()
  const pageSize = 100
  let page = 1
  try {
    while (true) {
      const params = buildAccountQuery(page, pageSize)
      const payload = await api<any>(`api/sites/${activeSiteId.value}/accounts?${params.toString()}`)
      const pageAccounts = normalizeAccounts(payload)
      rememberAccountMeta(pageAccounts)
      pageAccounts.filter(matchesLocalAccountFilters).forEach((account) => {
        const id = accountID(account)
        if (id) ids.add(id)
      })
      const total = payloadTotal(payload)
      batchCollectPage.value = page
      batchCollectFound.value = ids.size
      batchCollectTotalPages.value = total ? Math.ceil(total / pageSize) : 0
      if (!pageAccounts.length) break
      if (total && page * pageSize >= total) break
      if (!total && pageAccounts.length < pageSize) break
      page += 1
    }
    const numericIDs = Array.from(ids).map((id) => Number(id)).filter((id) => Number.isFinite(id) && id > 0)
    filteredAccountIDsCache.set(cacheKey, numericIDs)
    selectedAccountIds.value = new Set(numericIDs.map((id) => String(id)))
    return numericIDs
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '收集筛选账号失败'
    return []
  } finally {
    batchCollecting.value = false
  }
}

function matchesLocalAccountFilters(account: Account) {
  if (scheduleQuickFilter.value !== 'all' && accountScheduleKey(account) !== scheduleQuickFilter.value) return false
  return matchesGroupFilters(account)
}

function clearAccountSelection() {
  selectedAccountIds.value = new Set()
}

function accountGroups(account: Account) {
  const names = accountGroupEntries(account).map((group) => group.name)
  return names.length ? names.join(' / ') : '未分组'
}

function accountNote(account: Account) {
  const extra = (account.extra || {}) as Record<string, unknown>
  const value = account.note ?? account.remark ?? account.description ?? extra.note ?? extra.remark ?? extra.description
  const text = value === undefined || value === null ? '' : String(value).trim()
  return text
}

function accountGroupPreview(account: Account) {
  const groups = accountGroupEntries(account)
  return {
    items: groups.slice(0, 2),
    extra: Math.max(0, groups.length - 2),
  }
}

function groupPopoverKey(account: Account) {
  return accountID(account) || accountName(account)
}

function toggleFloatingGroupPopover(account: Account, event: MouseEvent) {
  const key = groupPopoverKey(account)
  if (groupPopoverAccount.value && groupPopoverKey(groupPopoverAccount.value) === key) {
    closeGroupPopover()
    return
  }
  const target = event.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  groupPopoverPosition.top = rect.bottom + window.scrollY + 8
  groupPopoverPosition.left = Math.max(12, Math.min(rect.left + window.scrollX, window.scrollX + window.innerWidth - 400))
  groupPopoverAccount.value = account
}

function closeGroupPopover() {
  groupPopoverAccount.value = null
}

function addIncludeGroupFilter(id: string) {
  setGroupState(id, 'include')
}

function handleDocumentClick(event: MouseEvent) {
  const target = event.target as HTMLElement | null
  if (!target) return
  if (target.closest('.floating-group-popover') || target.closest('.group-preview-wrap')) return
  closeGroupPopover()
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

function groupStateLabel(state: GroupState) {
  if (state === 'include') return '包含'
  if (state === 'exclude') return '排除'
  return '随意'
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

function accountPlatformLabel(account: Account) {
  const parts = [platformLabel(account.platform), accountTypeLabel(account.type)]
  return parts.filter((part) => part !== '未知').join(' / ') || '未知'
}

function accountStatusLabel(account: Account) {
  return statusLabel(account.status)
}

function accountStatusTone(account: Account) {
  const status = String(account.status || '').toLowerCase()
  if (!status) return 'neutral'
  if (['active', 'enabled', 'ready', 'ok', 'success'].includes(status)) return 'success'
  if (['error', 'failed', 'disabled', 'blocked', 'inactive'].includes(status)) return 'danger'
  if (['pending', 'testing', 'processing'].includes(status)) return 'warning'
  return 'neutral'
}

function accountScheduleTone(account: Account) {
  switch (accountScheduleKey(account)) {
    case 'ready': return 'success'
    case 'rate': return 'warning'
    case 'overload': return 'info'
    case 'temp':
    case 'blocked': return 'danger'
    default: return 'neutral'
  }
}

function accountUsageShort(account: Account) {
  const parts = accountUsageMetrics(account).map((metric) => `${metric.label} ${metric.text}`).filter((item) => !item.endsWith('未知'))
  return parts.length ? parts.join(' / ') : '未知'
}

function accountUsageMetrics(account: Account) {
  const extra = (account.extra || {}) as Record<string, unknown>
  const primary = usagePercentValue(extra.codex_primary_used_percent ?? extra.codex_5h_used_percent)
  const secondary = usagePercentValue(extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent)
  return [
    { label: '5h', value: primary, text: usagePercentText(primary) },
    { label: '7d', value: secondary, text: usagePercentText(secondary) },
  ]
}

function usagePercentValue(value: unknown) {
  if (value === undefined || value === null || value === '') return null
  const numeric = Number(String(value).replace('%', '').trim())
  if (!Number.isFinite(numeric)) return null
  return Math.max(0, Math.min(100, numeric))
}

function usagePercentText(value: number | null) {
  return value === null ? '未知' : `${value.toFixed(Number.isInteger(value) ? 0 : 1)}%`
}

function usagePercentWidth(value: number | null) {
  return value === null ? 0 : value
}

function accountLastUsedLabel(account: Account) {
  return formatDateTime(account.last_used_at)
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
    ['5h 用量', extra.codex_primary_used_percent ?? extra.codex_5h_used_percent],
    ['7d 用量', extra.codex_secondary_used_percent ?? extra.codex_7d_used_percent],
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

function openAccountDetail(account: Account) {
  selectedAccount.value = account
  showAccountModal.value = true
}

function openBatchTestDetail(result: Record<string, unknown>) {
  selectedBatchTestResult.value = result
  showBatchTestModal.value = true
}

function clearAccountFilters() {
  Object.assign(accountFilters, { search: '', platform: '', status: '', type: '', privacyMode: '', upstreamGroup: '', sortBy: 'name', sortOrder: 'asc' })
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
  await testAccountIDs(ids)
}

async function refreshSelectedAccountTokens() {
  batchRefreshError.value = ''
  batchRefreshResult.value = null
  if (!activeSiteId.value) {
    batchRefreshError.value = '请先选择站点'
    return
  }
  const ids = Array.from(selectedAccountIds.value).map((id) => Number(id)).filter((id) => Number.isFinite(id) && id > 0)
  await refreshAccountTokenIDs(ids)
}

async function refreshAllFilteredAccountTokens() {
  batchRefreshError.value = ''
  batchRefreshResult.value = null
  const ids = await selectAllFilteredAccounts()
  await refreshAccountTokenIDs(ids)
}

async function refreshAccountTokenIDs(ids: number[]) {
  if (!ids.length) {
    batchRefreshError.value = '请先选择账号'
    return
  }
  const ok = await askConfirm({ title: '刷新账号令牌？', message: `将刷新 ${ids.length} 个账号的 OAuth 令牌，这会修改上游账号凭证。确认继续？`, confirmText: '确认刷新', closeOnBackdrop: false })
  if (!ok) return
  batchRefreshTotal.value = ids.length
  batchRefreshDone.value = 0
  batchTestResults.value = ids.map((id) => ({ id, pending: true, message: '等待刷新...', ...accountMetaForIDs([id])[String(id)] }))
  batchRefreshing.value = true
  batchTestJob.value = null
  try {
    const job = await api<JobRecord>(`api/sites/${activeSiteId.value}/jobs/batch-token-refresh`, {
      method: 'POST',
      body: JSON.stringify({ ids, accountMeta: accountMetaForIDs(ids) }),
    })
    await pollBatchRefreshJob(Number(job.id))
    await loadAccounts({ force: true })
    await loadRecentJobs()
  } catch (error) {
    batchRefreshError.value = error instanceof Error ? error.message : '批量刷新令牌失败'
  } finally {
    batchRefreshing.value = false
  }
}

async function pollBatchRefreshJob(jobId: number) {
  while (batchRefreshing.value) {
    const job = await api<JobRecord>(`api/jobs/${jobId}`)
    updateBatchRefreshJob(job)
    if (!['queued', 'running'].includes(String(job.status))) return
    await waitForBatchPoll()
  }
}

function updateBatchRefreshJob(job: JobRecord) {
  batchTestJob.value = job
  batchRefreshTotal.value = Number(job.totalCount || batchRefreshTotal.value || 0)
  batchRefreshDone.value = Number(job.doneCount || 0)
  const result = job.result as Record<string, unknown> | undefined
  const items = Array.isArray(result?.items) ? result.items as Record<string, unknown>[] : []
  if (items.length) batchTestResults.value = items
  const response = result?.response as Record<string, unknown> | undefined
  if (response) batchRefreshResult.value = response
  else if (result && Object.keys(result).length) batchRefreshResult.value = result
}

async function testAllFilteredAccounts() {
  batchTestError.value = ''
  batchTestResults.value = []
  const ids = await selectAllFilteredAccounts()
  await testAccountIDs(ids)
}

async function testAccountIDs(ids: number[]) {
  if (!ids.length) {
    batchTestError.value = '请先选择账号'
    return
  }
  batchTestTotal.value = ids.length
  batchTestDone.value = 0
  batchTesting.value = true
  batchTestJob.value = null
  try {
    batchTestResults.value = ids.map((id) => ({ id, pending: true, message: '等待检测...' }))
    const job = await api<JobRecord>(`api/sites/${activeSiteId.value}/jobs/batch-account-test`, {
      method: 'POST',
      body: JSON.stringify({ ids, ...batchTestForm, accountMeta: accountMetaForIDs(ids) }),
    })
    await pollBatchTestJob(Number(job.id))
    await loadRecentJobs()
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '批量检测失败'
  } finally {
    batchTesting.value = false
  }
}

async function pollBatchTestJob(jobId: number) {
  stopBatchTestPollTimer()
  while (batchTesting.value) {
    const job = await api<JobRecord>(`api/jobs/${jobId}`)
    updateBatchTestJob(job)
    if (!['queued', 'running'].includes(String(job.status))) return
    await waitForBatchPoll()
  }
}

async function openJobResult(job: JobRecord) {
  const id = Number(job.id)
  if (!id) return
  try {
    const detail = await api<JobRecord>(`api/jobs/${id}`)
    updateBatchTestJob(detail)
    if (detail.type === 'batch_token_refresh') updateBatchRefreshJob(detail)
    const result = detail.result as Record<string, unknown> | undefined
    const items = Array.isArray(result?.items) ? result.items as Record<string, unknown>[] : []
    batchTestResults.value = items
    batchTestTotal.value = Number(detail.totalCount || items.length || 0)
    batchTestDone.value = Number(detail.doneCount || items.length || 0)
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '加载任务详情失败'
  }
}

async function cancelBatchTestJob() {
  const id = Number(batchTestJob.value?.id)
  if (!id) return
  try {
    const job = await api<JobRecord>(`api/jobs/${id}/cancel`, { method: 'POST', body: '{}' })
    updateBatchTestJob(job)
    if (job.type === 'batch_token_refresh') updateBatchRefreshJob(job)
    await loadRecentJobs()
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '取消任务失败'
  } finally {
    batchTesting.value = false
    batchRefreshing.value = false
    stopBatchTestPollTimer()
  }
}

async function cancelJobFromList(job: JobRecord) {
  const id = Number(job.id)
  if (!id || !isJobCancellable(job)) return
  const ok = await askConfirm({ title: '取消任务？', message: `确定取消任务 #${id} 吗？`, confirmText: '取消任务', closeOnBackdrop: false })
  if (!ok) return
  batchTestError.value = ''
  try {
    const cancelled = await api<JobRecord>(`api/jobs/${id}/cancel`, { method: 'POST', body: '{}' })
    recentJobs.value = recentJobs.value.map((item) => Number(item.id) === id ? cancelled : item)
    if (Number(batchTestJob.value?.id) === id) {
      updateBatchTestJob(cancelled)
      if (cancelled.type === 'batch_token_refresh') updateBatchRefreshJob(cancelled)
      batchTesting.value = false
      batchRefreshing.value = false
      stopBatchTestPollTimer()
    }
    await loadRecentJobs()
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '取消任务失败'
  }
}

async function retryFailedBatchJob() {
  const id = Number(batchTestJob.value?.id)
  if (!id) return
  batchTestError.value = ''
  const isTokenRefresh = batchTestJob.value?.type === 'batch_token_refresh'
  batchTesting.value = !isTokenRefresh
  batchRefreshing.value = isTokenRefresh
  try {
    const job = await api<JobRecord>(`api/jobs/${id}/retry-failed`, { method: 'POST', body: '{}' })
    batchTestResults.value = failedBatchTestIDs.value.map((accountId) => ({ id: accountId, pending: true, message: isTokenRefresh ? '等待刷新...' : '等待检测...', ...accountMetaForIDs([accountId])[String(accountId)] }))
    if (isTokenRefresh) await pollBatchRefreshJob(Number(job.id))
    else await pollBatchTestJob(Number(job.id))
    await loadRecentJobs()
  } catch (error) {
    batchTestError.value = error instanceof Error ? error.message : '重试失败项失败'
  } finally {
    batchTesting.value = false
    batchRefreshing.value = false
  }
}

async function retryFailedJobFromList(job: JobRecord) {
  await openJobResult(job)
  await retryFailedBatchJob()
}

function updateBatchTestJob(job: JobRecord) {
  batchTestJob.value = job
  batchTestTotal.value = Number(job.totalCount || batchTestTotal.value || 0)
  batchTestDone.value = Number(job.doneCount || 0)
  const result = job.result as Record<string, unknown> | undefined
  const items = Array.isArray(result?.items) ? result.items as Record<string, unknown>[] : []
  if (items.length) {
    const itemMap = new Map(items.map((item) => [String(item.id), item]))
    batchTestResults.value = batchTestResults.value.map((item) => itemMap.get(String(item.id)) || item)
  }
}

function jobProgress(job: JobRecord) {
  return progressPercent(Number(job.doneCount || 0), Number(job.totalCount || 0))
}

function jobStatusLabel(status: unknown) {
  const value = String(status || '')
  if (value === 'queued') return '排队中'
  if (value === 'running') return '运行中'
  if (value === 'succeeded') return '成功'
  if (value === 'failed') return '失败'
  if (value === 'cancelled') return '已取消'
  return value || '未知'
}

function isJobCancellable(job: JobRecord) {
  return ['queued', 'running'].includes(String(job.status || ''))
}

function jobTypeLabel(type: unknown) {
  if (type === 'batch_account_test') return '批量检测'
  if (type === 'batch_token_refresh') return '刷新令牌'
  if (type === 'import_accounts') return '导入账号'
  return String(type || '未知')
}

function taskResultTitle() {
  if (batchTestJob.value?.type === 'batch_token_refresh') return '刷新令牌结果'
  if (batchTestJob.value?.type === 'import_accounts') return '导入账号结果'
  return '批量检测结果'
}

function batchResultFilterLabel() {
  if (batchResultFilter.value === 'failed') return '失败'
  if (batchResultFilter.value === 'success') return '成功'
  if (batchResultFilter.value === 'pending') return '进行中'
  return '全部'
}

function batchResultAction(result: Record<string, unknown>) {
  if (batchTestJob.value?.type === 'batch_token_refresh') return '令牌刷新'
  if (batchTestJob.value?.type === 'import_accounts') return result.accountId ? `创建 #${result.accountId}` : '账号导入'
  return result.model || '未知'
}

function selectedTaskResultJSON() {
  if (!batchTestJob.value) return ''
  return JSON.stringify(batchTestJob.value.result || {}, null, 2)
}

function importSummaryValue(key: string) {
  return importPreview.value?.summary?.[key] ?? 0
}

function importItemStatus(item: ImportPreviewItem) {
  return item.recognized ? '可预览' : '需修正'
}

function importItemStatusClass(item: ImportPreviewItem) {
  return item.recognized ? 'tag tag-success' : 'tag tag-warning'
}

function importItemList(item: ImportPreviewItem, key: string) {
  const value = item[key]
  return Array.isArray(value) && value.length ? value.join(' / ') : '无'
}

function importItemName(item: ImportPreviewItem) {
  return String(item.name || '未命名')
}

function auditSummaryText(value: unknown) {
  if (!value || typeof value !== 'object') return '{}'
  return JSON.stringify(value)
}

function waitForBatchPoll() {
  return new Promise((resolve) => {
    batchTestPollTimer = window.setTimeout(resolve, 1200)
  })
}

function stopBatchTestPollTimer() {
  if (batchTestPollTimer) window.clearTimeout(batchTestPollTimer)
  batchTestPollTimer = null
}

function isBatchResultPending(result: Record<string, unknown>) {
  return result.pending === true || result.message === '检测中...'
}

function batchResultLabel(result: Record<string, unknown>) {
  if (isBatchResultPending(result)) return batchTestJob.value?.type === 'batch_token_refresh' ? '刷新中' : '测试中'
  return result.ok ? '成功' : '失败'
}

function batchResultTagClass(result: Record<string, unknown>) {
  if (isBatchResultPending(result)) return 'tag tag-warning'
  return result.ok ? 'tag tag-success' : 'tag tag-danger'
}

function batchResultHint(result: Record<string, unknown>) {
  if (isBatchResultPending(result)) return batchTestJob.value?.type === 'batch_token_refresh' ? '刷新中' : '测试中'
  if (result.ok === true && !result.hint && !result.resetAt) return '正常'
  if (result.ok === false && !result.hint && !result.resetAt) return '异常'
  return result.hint || result.resetAt || '异常'
}

function batchResultSummary(result: Record<string, unknown>) {
  const text = String(result.message || result.error || result.hint || '')
  if (!text) return '无响应内容'
  return text.length > 36 ? `${text.slice(0, 36)}...` : text
}

function resultAccountName(result: Record<string, unknown>) {
  return String(result.name || accountMetaCache.get(String(result.id || ''))?.name || '未知账号')
}

function resultAccountNote(result: Record<string, unknown>) {
  return String(result.note || accountMetaCache.get(String(result.id || ''))?.note || '')
}

function batchResultDetailJSON(result: Record<string, unknown>) {
  return JSON.stringify(result, null, 2)
}

function progressPercent(done: number, total: number) {
  if (!total) return 0
  return Math.min(100, Math.round((done / total) * 100))
}

function scrollToBottom(el: HTMLElement | null) {
  if (!el) return
  el.scrollTop = el.scrollHeight
}

async function retryFailedBatchTests() {
  if (!failedBatchTestIDs.value.length) return
  if (batchTestJob.value?.id) {
    await retryFailedBatchJob()
    return
  }
  selectedAccountIds.value = new Set(failedBatchTestIDs.value.map((id) => String(id)))
  await testSelectedAccounts()
}

function copyFailedBatchTestIDs() {
  copyText(failedBatchTestIDs.value.join('\n'), '失败账号 ID')
}

function copyBatchTestGroupIDs(group: { hint: string; ids: number[] }) {
  copyText(group.ids.join('\n'), `${group.hint}账号 ID`)
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

watch(() => batchTestResults.value.length, async () => {
  await nextTick()
  scrollToBottom(batchTestScroll.value)
})

watch(batchRefreshResult, async () => {
  await nextTick()
  scrollToBottom(batchRefreshScroll.value)
})

onMounted(refreshMe)
onMounted(() => {
  document.addEventListener('click', handleDocumentClick, true)
})
onUnmounted(() => {
  stopAccountQueryTimer()
  stopBatchTestPollTimer()
  accountAbortController.value?.abort()
  document.removeEventListener('click', handleDocumentClick, true)
})
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
          <button :class="activeView === 'stats' ? '' : 'secondary'" @click="showStats">统计</button>
          <button :class="activeView === 'accounts' ? '' : 'secondary'" @click="activeView = 'accounts'">账号</button>
          <button :class="activeView === 'import' ? '' : 'secondary'" @click="showImport">导入</button>
          <button :class="activeView === 'jobs' ? '' : 'secondary'" @click="showJobs">任务</button>
          <button :class="activeView === 'audit' ? '' : 'secondary'" @click="showAudit">审计</button>
          <button :class="activeView === 'sites' ? '' : 'secondary'" @click="activeView = 'sites'">站点</button>
          <button :class="activeView === 'docs' ? '' : 'secondary'" @click="showDocs">文档</button>
          <button class="secondary" @click="logout">退出登录</button>
        </div>
      </header>

      <section v-if="activeView === 'stats'" class="panel stats-panel">
        <div class="panel-head">
          <div>
            <h2>用量仪表盘</h2>
            <p class="muted">当前站点：{{ activeSite?.name || '未选择' }} · 站点 {{ dashboardStats.enabledSites }} / {{ dashboardStats.sites }} 启用</p>
          </div>
          <div class="refresh-meta">
            <span class="muted">刷新时间：{{ refreshTimeLabel() }}</span>
            <button class="secondary" :disabled="statsLoading || !activeSiteId" @click="loadStatistics">{{ statsLoading ? '加载中...' : '刷新统计' }}</button>
          </div>
        </div>
        <div class="stats-controls">
          <button type="button" class="secondary" :class="{ active: statsRange.preset === '24h' }" @click="setStatsPreset('24h')">近 24 小时</button>
          <button type="button" class="secondary" :class="{ active: statsRange.preset === '7d' }" @click="setStatsPreset('7d')">近 7 天</button>
          <button type="button" class="secondary" :class="{ active: statsRange.preset === '30d' }" @click="setStatsPreset('30d')">近 30 天</button>
          <label>开始<input v-model="statsRange.startDate" type="date" /></label>
          <label>结束<input v-model="statsRange.endDate" type="date" /></label>
          <label>粒度
            <select v-model="statsRange.granularity">
              <option value="hour">小时</option>
              <option value="day">天</option>
            </select>
          </label>
          <button type="button" @click="applyStatsRange">应用范围</button>
        </div>
        <p v-if="statsError" class="error">{{ statsError }}</p>
        <div class="stats-grid">
          <article class="stat-panel-card">
            <span>用户</span>
            <strong>{{ statValue(statisticsStats, 'active_users') }} / {{ statValue(statisticsStats, 'total_users') }}</strong>
            <p class="muted stat-lines"><span>今日活跃 / 全部</span><span title="1 小时内活跃用户数">当前活跃 {{ statValue(statisticsStats, 'hourly_active_users') }}</span></p>
          </article>
          <article class="stat-panel-card">
            <span>账号状态</span>
            <strong>{{ statisticsActiveAccounts() }} / {{ statValue(statisticsStats, 'total_accounts') }}</strong>
            <div class="status-chip-row">
              <span class="status-tag status-tag-warn">限流 {{ statValue(statisticsStats, 'ratelimit_accounts') }}</span>
              <span class="status-tag status-tag-danger">错误 {{ statValue(statisticsStats, 'error_accounts') }}</span>
              <span class="status-tag status-tag-overload">过载 {{ statValue(statisticsStats, 'overload_accounts') }}</span>
            </div>
          </article>
          <article class="stat-panel-card">
            <span>今日请求</span>
            <strong>{{ statValue(statisticsStats, 'today_requests') }}</strong>
            <p class="muted">今日消耗 {{ statCost(statisticsStats, 'today_actual_cost') }}</p>
          </article>
          <article class="stat-panel-card">
            <span>今日 Tokens</span>
            <strong>{{ tokenValue(statisticsStats, 'today_tokens') }}</strong>
            <p class="muted stat-lines"><span>费用 {{ statCost(statisticsStats, 'today_actual_cost') }}</span><span>成本 {{ statCost(statisticsStats, 'today_account_cost') }}</span></p>
          </article>
          <article class="stat-panel-card">
            <span>累计请求</span>
            <strong>{{ statValue(statisticsStats, 'total_requests') }}</strong>
            <p class="muted stat-lines"><span>费用 {{ statCost(statisticsStats, 'total_actual_cost') }}</span><span>成本 {{ statCost(statisticsStats, 'total_account_cost') }}</span></p>
          </article>
          <article class="stat-panel-card">
            <span>吞吐</span>
            <strong>{{ statValue(statisticsStats, 'rpm') }} RPM / {{ statValue(statisticsStats, 'tpm') }} TPM</strong>
            <p class="muted">最近 5 分钟聚合统计。</p>
          </article>
        </div>
        <div class="stats-sections">
          <ExpandablePanel title="请求与 Token 趋势" panel-class="stats-section-trend">
            <template #meta><span class="muted">{{ statisticsSnapshot.start_date || statsRange.startDate }} 至 {{ statisticsSnapshot.end_date || statsRange.endDate }}</span></template>
            <div v-if="recentTrend.length" class="trend-panel">
              <div class="trend-chart-card">
                <div class="trend-chart-frame">
                  <svg viewBox="0 0 600 220" role="img" aria-label="请求与 Token 趋势" @mousemove="handleChartMove" @mouseleave="clearChartHover">
                    <g transform="translate(56 18)">
                      <line x1="0" y1="0" x2="0" y2="170" class="chart-axis" />
                      <line x1="0" y1="170" x2="520" y2="170" class="chart-axis" />
                      <line x1="0" y1="85" x2="520" y2="85" class="chart-grid" />
                      <text x="-8" y="4" class="chart-tick" text-anchor="end">{{ chartAxisLabel(maxTrendTokens) }}</text>
                      <text x="-8" y="89" class="chart-tick" text-anchor="end">{{ chartAxisLabel(maxTrendTokens / 2) }}</text>
                      <text x="-8" y="174" class="chart-tick" text-anchor="end">0</text>
                      <path :d="chartPath(recentTrend, 'total_tokens')" class="chart-line tokens" />
                      <path :d="chartPath(recentTrend, 'requests')" class="chart-line requests" />
                      <g v-if="chartHoverPoint && chartHoverIndex !== null" class="chart-hover">
                        <line :x1="chartX(chartHoverIndex)" y1="0" :x2="chartX(chartHoverIndex)" y2="170" class="chart-hover-line" />
                        <circle :cx="chartX(chartHoverIndex)" :cy="chartY(chartHoverPoint, 'total_tokens')" r="4" class="chart-dot tokens" />
                        <circle :cx="chartX(chartHoverIndex)" :cy="chartY(chartHoverPoint, 'requests')" r="4" class="chart-dot requests" />
                      </g>
                    </g>
                    <foreignObject v-if="chartHoverPoint" x="350" y="18" width="230" height="90">
                      <div class="chart-tooltip">
                        <strong>{{ chartHoverPoint.date }}</strong>
                        <span>Tokens：{{ tokenValue(chartHoverPoint, 'total_tokens') }}</span>
                        <span>请求：{{ statValue(chartHoverPoint, 'requests') }}</span>
                      </div>
                    </foreignObject>
                  </svg>
                </div>
                <div class="chart-legend-row">
                  <div class="chart-legend"><span class="legend-token">Tokens</span><span class="legend-request">请求</span></div>
                  <div class="chart-labels"><span>{{ recentTrend[0]?.date }}</span><span>{{ recentTrend[recentTrend.length - 1]?.date }}</span></div>
                </div>
              </div>
              <div class="trend-summary-grid">
                <article class="trend-summary-card"><span>总 Tokens</span><strong>{{ tokenValue(statisticsStats, 'total_tokens') }}</strong></article>
                <article class="trend-summary-card"><span>总请求</span><strong>{{ statValue(statisticsStats, 'total_requests') }}</strong></article>
              </div>
            </div>
            <p v-else class="muted">暂无趋势数据。</p>
          </ExpandablePanel>
          <ExpandablePanel title="当前用户并发" panel-class="stats-section-user-concurrency">
            <template #meta>
              <span class="muted concurrency-meta" title="来自 /api/v1/admin/ops/user-concurrency。">
                <span>{{ userConcurrencySummary.current }} 使用中 · {{ userConcurrencySummary.waiting }} 排队</span>
                <button type="button" class="mini inline-refresh" :disabled="userConcurrencyLoading || !activeSiteId" @click.stop="refreshUserConcurrency">{{ userConcurrencyLoading ? '刷新中...' : '刷新' }}</button>
              </span>
            </template>
            <p v-if="userConcurrency.enabled === false" class="muted">sub2api Ops 实时监控未开启，无法获取用户并发。</p>
            <p v-else-if="statistics?.userConcurrencyError" class="error">{{ statistics.userConcurrencyError }}</p>
            <ConcurrencyTable
              v-else
              name-label="用户"
              value-label="容量"
              empty-text="暂无用户并发数据。"
              :rows="currentUserConcurrency"
              :key-of="(row) => String(row.user_id ?? userDisplayName(row))"
              :name-of="userDisplayName"
              :value-of="(row) => `${statValue(row, 'current_in_use')} / ${statValue(row, 'max_capacity')}`"
              :percent-of="concurrencyPercent"
              :title-of="userConcurrencyCapacityTitle"
            />
          </ExpandablePanel>
          <ExpandablePanel title="当前账号并发" panel-class="stats-section-account-concurrency">
            <template #meta>
              <span class="muted concurrency-meta" title="来自 /api/v1/admin/ops/concurrency。">
                <span>{{ accountConcurrencySummary.current }} 使用中 · {{ accountConcurrencySummary.waiting }} 排队</span>
                <button type="button" class="mini inline-refresh" :disabled="accountConcurrencyLoading || !activeSiteId" @click.stop="refreshAccountConcurrency">{{ accountConcurrencyLoading ? '刷新中...' : '刷新' }}</button>
              </span>
            </template>
            <p v-if="opsConcurrency.enabled === false" class="muted">sub2api Ops 实时监控未开启，无法获取账号并发。</p>
            <p v-else-if="statistics?.opsConcurrencyError" class="error">{{ statistics.opsConcurrencyError }}</p>
            <ConcurrencyTable
              v-else
              name-label="账号"
              value-label="容量"
              empty-text="暂无账号并发数据。"
              :rows="currentAccountConcurrency"
              :key-of="(row) => String(row.account_id ?? accountConcurrencyName(row))"
              :name-of="accountConcurrencyName"
              :value-of="(row) => `${statValue(row, 'current_in_use')} / ${statValue(row, 'max_capacity')}`"
              :percent-of="concurrencyPercent"
              :title-of="accountConcurrencyCapacityTitle"
            />
          </ExpandablePanel>
          <ExpandablePanel title="用户消费排行" panel-class="stats-section-ranking">
            <template #meta><span class="muted">{{ statisticsSnapshot.start_date || statsRange.startDate }} 至 {{ statisticsSnapshot.end_date || statsRange.endDate }}</span></template>
            <StatsTable class="fit-table">
              <table class="mini-table ranking-table">
                <colgroup>
                  <col class="name-col" />
                  <col class="usage-col" />
                </colgroup>
                <thead>
                  <tr><th>用户</th><th>用量</th></tr>
                </thead>
                <tbody>
                  <tr v-for="user in statisticsRanking" :key="String(user.user_id)">
                    <td class="ranking-name">{{ userDisplayName(user) }}</td>
                    <td class="ranking-usage">
                      <div class="usage-bar-shell"><div class="usage-bar" :style="{ width: `${rankingWidth(user)}%` }"></div></div>
                      <span>{{ statCost(user, 'actual_cost') }} <b>·</b> {{ tokenValue(user, 'tokens') }}</span>
                    </td>
                  </tr>
                  <tr v-if="!statisticsRanking.length"><td colspan="2" class="muted">暂无用户排行数据。</td></tr>
                </tbody>
              </table>
            </StatsTable>
          </ExpandablePanel>
          <ExpandablePanel title="模型分布" panel-class="stats-section-models">
            <template #meta><span class="muted">{{ statisticsSnapshot.start_date || statsRange.startDate }} 至 {{ statisticsSnapshot.end_date || statsRange.endDate }}</span></template>
            <StatsTable>
              <table class="mini-table">
                <colgroup>
                  <col class="name-col" />
                  <col class="num-col" />
                  <col class="num-col" />
                  <col class="money-col" />
                  <col class="money-col" />
                </colgroup>
                <thead><tr><th>模型</th><th class="num-cell">请求</th><th class="num-cell">Tokens</th><th class="num-cell">实际扣费</th><th class="num-cell">成本</th></tr></thead>
                <tbody>
                  <tr v-for="model in topModels" :key="String(model.model)">
                    <td class="name-cell">{{ model.model || '未知模型' }}</td>
                    <td class="num-cell">{{ statValue(model, 'requests') }}</td>
                    <td class="num-cell">{{ tokenValue(model, 'total_tokens') }}</td>
                    <td class="num-cell">{{ statCost(model, 'actual_cost') }}</td>
                    <td class="num-cell">{{ statCost(model, 'account_cost') }}</td>
                  </tr>
                  <tr v-if="!topModels.length"><td colspan="5" class="muted">暂无模型统计。</td></tr>
                </tbody>
              </table>
            </StatsTable>
          </ExpandablePanel>
        </div>
      </section>

      <section v-else-if="activeView === 'import'" class="panel content-panel">
        <div class="panel-head">
          <div>
            <h2>导入预览</h2>
            <p class="muted">先解析预览账号内容，确认站点信息后再提交导入任务；浏览器不会保存 credentials。</p>
          </div>
          <button class="secondary" type="button" @click="clearImportPreview">清空</button>
        </div>
        <p v-if="importError" class="error">{{ importError }}</p>
        <div class="form-block import-form">
          <label>粘贴账号内容<textarea v-model="importForm.text" rows="10" placeholder="支持 JSON、JSON 数组、accounts 包装对象，以及常见 key=value / key: value 行格式。"></textarea></label>
          <div class="filter-grid">
            <label>选择文件<input type="file" accept=".json,.txt,.yaml,.yml,.csv" @change="handleImportFile" /></label>
            <label>代理
              <select v-model="importForm.proxyId" :disabled="proxiesLoading">
                <option value="">不指定代理</option>
                <option v-for="proxy in proxyOptions" :key="proxy.id" :value="proxy.id">{{ proxy.name }}</option>
              </select>
            </label>
            <label>优先级<input v-model="importForm.priority" type="number" placeholder="可选" /></label>
            <label>并发<input v-model="importForm.concurrency" type="number" placeholder="可选" /></label>
            <label>命名前缀<input v-model="importForm.namePrefix" placeholder="可选，支持 {date}，如 import-{date}-" /></label>
          </div>
          <div class="section-card compact-section import-setting-card">
            <h3>分组 <button type="button" class="mini inline-refresh" :disabled="groupsLoading" @click="loadGroups">{{ groupsLoading ? '刷新中...' : '刷新分组' }}</button></h3>
            <div class="active-filter-chips tag-picker">
              <button v-for="group in importGroupOptions" :key="group.id" type="button" class="filter-chip" :class="{ active: importForm.groups.includes(group.id) }" @click="toggleImportGroup(group.id)">{{ group.name }}</button>
              <span v-if="!importGroupOptions.length" class="muted">暂无可选分组。</span>
            </div>
            <p class="muted">只允许选择上游已有分组；未选择时使用上游默认分组逻辑。</p>
          </div>
          <div class="section-card compact-section import-setting-card">
            <h3>模型</h3>
            <div class="active-filter-chips tag-picker">
              <button v-for="model in importModelOptions" :key="model" type="button" class="filter-chip" :class="{ active: importForm.models.includes(model) }" @click="toggleImportModel(model)">{{ model }}<span v-if="customImportModels.includes(model)" @click.stop="removeImportModelTag(model)">×</span></button>
            </div>
            <div class="group-add-row">
              <input v-model="newImportModelTag" placeholder="输入新模型，多个可用逗号或空格分隔" @keyup.enter="addImportModelTag" />
              <button type="button" class="secondary" @click="addImportModelTag">添加模型</button>
            </div>
          </div>
          <div class="form-actions">
            <button type="button" :disabled="importLoading || !activeSiteId" @click="previewImport(1)">{{ importLoading ? '解析中...' : '生成预览' }}</button>
            <button type="button" class="danger" :disabled="importExecuting || importLoading || !importPreview || Number(importPreview.summary?.invalid || 0) > 0" @click="executeImportAccounts">{{ importExecuting ? '提交中...' : '确认导入' }}</button>
            <span class="muted">当前站点：{{ activeSite?.name || '未选择' }}<template v-if="importForm.filename"> · 文件：{{ importForm.filename }}</template></span>
          </div>
        </div>
        <section class="result-panel import-template-panel">
          <div class="panel-head">
            <div>
              <h2>导入模板</h2>
              <p class="muted">保存和套用预览默认设置；模板不会保存账号凭据。</p>
            </div>
            <div class="actions"><button class="secondary" :disabled="importTemplatesLoading" @click="loadImportTemplates">{{ importTemplatesLoading ? '加载中...' : '刷新模板' }}</button><button class="secondary" :class="{ danger: importTemplateDeleteMode }" @click="importTemplateDeleteMode = !importTemplateDeleteMode">{{ importTemplateDeleteMode ? '退出删除' : '删除模板' }}</button></div>
          </div>
          <div class="filter-grid">
            <label>模板名称<input v-model="importTemplateName" placeholder="例如 Anthropic OAuth 默认设置" /></label>
            <button type="button" :disabled="!importTemplateName.trim()" @click="saveImportTemplate">保存当前设置为模板</button>
          </div>
          <div class="active-filter-chips">
            <button v-for="template in importTemplates" :key="String(template.id)" type="button" class="filter-chip" :class="{ danger: importTemplateDeleteMode }" @click="importTemplateDeleteMode ? deleteImportTemplate(template) : applyImportTemplate(template)">{{ importTemplateDeleteMode ? '删除 ' : '' }}{{ template.name }}</button>
            <span v-if="!importTemplates.length" class="muted">暂无模板。</span>
          </div>
        </section>
        <section v-if="importPreview" class="batch-results result-panel">
          <div class="panel-head">
            <div>
              <h2>预览结果</h2>
              <p class="muted">总数 {{ importSummaryValue('total') }} · 识别 {{ importSummaryValue('recognized') }} · 需修正 {{ importSummaryValue('invalid') }} · 疑似重复 {{ importSummaryValue('duplicates') }}</p>
            </div>
            <button class="secondary" type="button" @click="copyText(JSON.stringify(importPreview, null, 2), '导入预览')">复制预览 JSON</button>
          </div>
          <div v-if="importPreview.warnings?.length" class="failure-groups">
            <span class="muted">警告</span>
            <span v-for="warning in importPreview.warnings" :key="warning" class="tag tag-warning">{{ warning }}</span>
          </div>
          <div class="result-scroll table-wrap">
            <table class="account-table result-table">
              <thead><tr><th>#</th><th>状态</th><th>账号</th><th>平台/类型</th><th>应用设置</th><th>凭据字段</th><th>缺失字段</th><th>警告</th></tr></thead>
              <tbody>
              <tr v-for="item in pagedImportPreviewItems" :key="String(item.index)">
                  <td>{{ item.index }}</td>
                  <td><span :class="importItemStatusClass(item)">{{ importItemStatus(item) }}</span></td>
                  <td><div class="cell-stack"><strong class="account-name">{{ importItemName(item) }}</strong><span v-if="item.group" class="muted account-note">分组：{{ item.group }}</span></div></td>
                  <td>{{ item.platform || '未知' }} / {{ item.type || '未知' }}</td>
                  <td><div class="cell-stack compact"><span>分组：{{ importItemList(item, 'appliedGroups') }}</span><span>代理：{{ item.appliedProxy || '无' }}</span><span>模型：{{ importItemList(item, 'appliedModels') }}</span><span>优先级/并发：{{ item.appliedPriority || '默认' }} / {{ item.appliedConcurrency || '默认' }}</span></div></td>
                  <td>{{ importItemList(item, 'credentialFields') }}</td>
                  <td>{{ importItemList(item, 'missingFields') }}</td>
                  <td>{{ importItemList(item, 'warnings') }}</td>
                </tr>
                <tr v-if="!importPreview.items.length"><td colspan="8" class="muted">未解析到账号条目。</td></tr>
              </tbody>
            </table>
          </div>
          <div class="pager" v-if="importPreviewTotalPages > 1">
            <button class="secondary" :disabled="importPreviewPage <= 1 || importLoading" @click="previewImport(importPreviewPage - 1)">上一页</button>
            <span class="muted">第 {{ importPreviewPage }} / {{ importPreviewTotalPages }} 页，默认每页 {{ importPreviewPageSize }} 条</span>
            <button class="secondary" :disabled="importPreviewPage >= importPreviewTotalPages || importLoading" @click="previewImport(importPreviewPage + 1)">下一页</button>
          </div>
        </section>
      </section>

      <section v-else-if="activeView === 'jobs'" class="panel content-panel">
        <div class="panel-head">
          <div>
            <h2>任务</h2>
            <p class="muted">统一查看批量任务历史、进度、结果和失败重试。当前已接入批量账号检测和令牌刷新。</p>
          </div>
          <button class="secondary" :disabled="jobsLoading" @click="loadRecentJobs">{{ jobsLoading ? '加载中...' : '刷新任务' }}</button>
        </div>
        <p v-if="batchTestError" class="error">{{ batchTestError }}</p>
        <section class="batch-results result-panel">
          <div class="panel-head">
            <div>
              <h2>最近任务</h2>
                <p class="muted">展示最近 10 个任务；点击查看结果可恢复批量检测或令牌刷新明细。</p>
            </div>
          </div>
          <div class="result-scroll table-wrap jobs-scroll">
            <table class="account-table result-table">
              <thead><tr><th>任务</th><th>类型</th><th>状态</th><th>进度</th><th>结果</th><th>创建时间</th><th>操作</th></tr></thead>
              <tbody>
                <tr v-for="job in recentJobs" :key="String(job.id)">
                  <td>#{{ job.id }}</td>
                  <td>{{ jobTypeLabel(job.type) }}</td>
                  <td>{{ jobStatusLabel(job.status) }}</td>
                  <td>
                    <div class="usage-row compact-progress">
                      <div class="usage-bar-shell"><div class="usage-bar" :style="{ width: `${jobProgress(job)}%` }"></div></div>
                      <strong class="usage-value">{{ job.doneCount || 0 }} / {{ job.totalCount || 0 }}</strong>
                    </div>
                  </td>
                  <td>{{ job.successCount || 0 }} 成功 / {{ job.failedCount || 0 }} 失败</td>
                  <td>{{ formatDateTime(job.createdAt) }}</td>
                  <td class="row-actions">
                    <button class="secondary mini" @click="openJobResult(job)">查看结果</button>
                    <button class="secondary mini" :disabled="job.type === 'import_accounts' || Number(job.failedCount || 0) <= 0 || batchTesting || batchRefreshing" @click="retryFailedJobFromList(job)">重试失败</button>
                    <button class="secondary mini" :disabled="!isJobCancellable(job)" @click="cancelJobFromList(job)">取消任务</button>
                  </td>
                </tr>
                <tr v-if="!recentJobs.length"><td colspan="7" class="muted">暂无任务记录。</td></tr>
              </tbody>
            </table>
          </div>
        </section>
        <section v-if="batchTestResults.length" class="batch-results result-panel">
          <div class="panel-head">
            <div>
              <h2>{{ taskResultTitle() }}</h2>
              <p class="muted"><span v-if="batchTestJob">任务 #{{ batchTestJob.id }} · {{ jobStatusLabel(batchTestJob.status) }} · </span>{{ batchTestDone }} / {{ batchTestTotal || batchTestResults.length }} 完成</p>
            </div>
            <div class="actions compact-actions">
              <button class="secondary" @click="copyText(JSON.stringify(batchTestResults, null, 2), '检测结果')">复制结果</button>
              <button class="secondary" :disabled="!failedBatchTestIDs.length || batchTesting" @click="copyFailedBatchTestIDs">复制失败账号 ID</button>
              <button class="secondary" :disabled="!failedBatchTestIDs.length || batchTesting" @click="retryFailedBatchTests">只重试失败项</button>
            </div>
          </div>
          <div class="active-filter-chips">
            <span class="muted">结果筛选：{{ batchResultFilterLabel() }}</span>
            <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'all' }" @click="batchResultFilter = 'all'">全部 {{ batchTestResults.length }}</button>
            <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'failed' }" @click="batchResultFilter = 'failed'">失败 {{ failedBatchTestIDs.length }}</button>
            <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'success' }" @click="batchResultFilter = 'success'">成功 {{ batchTestResults.filter((item) => item.ok === true).length }}</button>
            <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'pending' }" @click="batchResultFilter = 'pending'">进行中 {{ batchTestResults.filter(isBatchResultPending).length }}</button>
          </div>
          <div class="progress-track"><div class="progress-bar" :style="{ width: `${batchTestProgress}%` }"></div></div>
          <div ref="batchTestScroll" class="result-scroll table-wrap">
            <table class="account-table result-table">
              <thead><tr><th>账号</th><th>结果</th><th>操作</th><th>HTTP</th><th>耗时</th><th>提示</th><th>详情</th></tr></thead>
              <tbody>
                <tr v-for="result in displayedBatchResults" :key="String(result.id)">
                  <td><div class="cell-stack"><span class="muted cell-subtitle">{{ result.id }}</span><strong class="account-name">{{ resultAccountName(result) }}</strong><span v-if="resultAccountNote(result)" class="muted account-note">{{ resultAccountNote(result) }}</span></div></td>
                  <td><span :class="batchResultTagClass(result)">{{ batchResultLabel(result) }}</span></td>
                  <td>{{ batchResultAction(result) }}</td>
                  <td>{{ result.statusCode || '未知' }}</td>
                  <td>{{ result.durationMs ?? '未知' }} ms</td>
                  <td>{{ batchResultHint(result) }}</td>
                  <td><button class="secondary mini" type="button" :title="batchResultSummary(result)" @click="openBatchTestDetail(result)">查看详情</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
        <section v-else-if="batchTestJob" class="batch-results result-panel">
          <div class="panel-head">
            <div>
              <h2>任务结果</h2>
              <p class="muted">任务 #{{ batchTestJob.id }} · {{ jobTypeLabel(batchTestJob.type) }} · {{ jobStatusLabel(batchTestJob.status) }} · {{ batchTestJob.doneCount || 0 }} / {{ batchTestJob.totalCount || 0 }} 完成</p>
            </div>
            <button class="secondary" @click="copyText(selectedTaskResultJSON(), '任务结果')">复制结果</button>
          </div>
          <pre class="inline-json result-json">{{ selectedTaskResultJSON() }}</pre>
        </section>
      </section>

      <section v-else-if="activeView === 'audit'" class="panel content-panel">
        <div class="panel-head">
          <div>
            <h2>审计日志</h2>
            <p class="muted">记录站点写操作、任务动作和导入预览摘要；敏感字段会脱敏。</p>
          </div>
          <button class="secondary" :disabled="auditLoading" @click="loadAuditLogs">{{ auditLoading ? '加载中...' : '刷新审计' }}</button>
        </div>
        <p v-if="auditError" class="error">{{ auditError }}</p>
        <div class="result-scroll table-wrap jobs-scroll">
          <table class="account-table result-table">
            <thead><tr><th>ID</th><th>动作</th><th>站点</th><th>目标</th><th>请求摘要</th><th>结果摘要</th><th>时间</th></tr></thead>
            <tbody>
              <tr v-for="log in auditLogs" :key="String(log.id)">
                <td>#{{ log.id }}</td>
                <td>{{ log.action }}</td>
                <td>{{ log.siteId || '全局' }}</td>
                <td>{{ log.targetType }} · {{ log.targetCount || 0 }}</td>
                <td><code>{{ auditSummaryText(log.requestSummary) }}</code></td>
                <td><code>{{ auditSummaryText(log.resultSummary) }}</code></td>
                <td>{{ formatDateTime(log.createdAt) }}</td>
              </tr>
              <tr v-if="!auditLogs.length"><td colspan="7" class="muted">暂无审计日志。</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-else-if="activeView === 'docs'" class="panel docs-panel">
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

      <div v-if="activeView === 'accounts' || activeView === 'sites'" class="layout-grid" :class="{ 'single-column': activeView === 'accounts' }">
        <aside v-if="activeView === 'sites'" class="panel">
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
                <button class="secondary" @click="selectSite(site)">选择站点</button>
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

        <section v-if="activeView === 'accounts'" class="panel content-panel">
          <div class="panel-head">
            <h2>上游账号</h2>
            <button class="secondary" :disabled="accountsLoading" @click="reloadAccountsFromUpstream">刷新</button>
          </div>
          <section class="section-card">
            <div class="section-title">
              <div>
                <h3>查询筛选</h3>
                <p class="muted">基础条件会发送到 sub2api 账号列表接口。</p>
              </div>
            </div>
            <form class="filter-grid" @submit.prevent="submitAccountFilters">
              <label>搜索<input v-model="accountFilters.search" placeholder="名称、备注或标识" /></label>
              <label>平台
                <select v-model="accountFilters.platform">
                  <option value="">全部平台</option>
                  <option v-for="option in platformOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </label>
              <label>状态
                <select v-model="accountFilters.status">
                  <option value="">全部状态</option>
                  <option v-for="option in accountStatusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </label>
              <label>类型
                <select v-model="accountFilters.type">
                  <option value="">全部类型</option>
                  <option v-for="option in accountTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </label>
              <label>上游分组
                <select v-model="accountFilters.upstreamGroup">
                  <option value="">全部</option>
                  <option v-for="group in upstreamGroupOptions" :key="group.id" :value="group.id">{{ group.name }}</option>
                </select>
              </label>
              <label>每页数量
                <select v-model.number="accountPager.pageSize" @change="changeAccountPageSize">
                  <option :value="10">10</option>
                  <option :value="20">20</option>
                  <option :value="50">50</option>
                </select>
              </label>
              <div class="form-actions">
                <button type="submit" :disabled="accountsLoading">{{ accountsLoading ? '查询中...' : '查询' }}</button>
                <button type="button" class="secondary" :disabled="accountsLoading" @click="clearAccountFilters">清空筛选</button>
              </div>
            </form>
            <details class="advanced-block">
              <summary>高级查询与本地筛选</summary>
              <div class="filter-grid advanced-grid">
                <label>隐私模式
                  <select v-model="accountFilters.privacyMode">
                    <option value="">全部 Privacy 状态</option>
                    <option v-for="option in privacyModeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
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
              </div>
              <div class="group-filter-head">
                <strong>分组三态筛选</strong>
                <span class="muted">只过滤当前已加载结果；未加入筛选列表的分组默认随意。</span>
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
            </details>
          </section>
          <p v-if="accountError" class="error">{{ accountError }}</p>
          <p v-if="activeAccountFilters" class="muted filter-summary">
            当前筛选：{{ activeAccountFilters }}
            <button type="button" class="mini" @click="copyText(activeAccountFilters, '筛选条件')">复制</button>
            <span v-if="filteredIDCacheHit" class="pill">已缓存筛选 ID</span>
          </p>
          <div v-if="groupFilterItems.length" class="active-filter-chips">
            <span class="muted">分组筛选</span>
            <button v-for="group in groupFilterItems" :key="group.id" type="button" class="filter-chip" @click="setGroupState(group.id, 'any')">
              {{ groupStateLabel(groupState(group.id)) }}：{{ group.name }} <span>×</span>
            </button>
          </div>
          <p v-if="accountQueryNotice" class="muted query-notice">{{ accountQueryNotice }}</p>
          <details class="section-card batch-section batch-details">
            <summary class="section-title batch-summary">
              <div>
                <h3>批量操作</h3>
                <p class="muted">已选择 {{ selectedAccountCount }} 个账号；当前筛选全部会先汇总账号 ID 并缓存。</p>
              </div>
              <span class="summary-toggle"></span>
            </summary>
            <div class="batch-body">
              <button class="secondary" :disabled="selectedAccountCount === 0" @click="clearAccountSelection">一键取消选择</button>
              <div class="primary-actions">
                <button :disabled="batchTesting || selectedAccountCount === 0" @click="testSelectedAccounts">{{ batchTesting ? '检测中...' : '检测选中账号' }}</button>
                <button class="secondary" :disabled="batchTesting || batchCollecting || !activeSiteId" @click="testAllFilteredAccounts">{{ batchCollecting ? '收集中...' : '检测当前筛选全部' }}</button>
              </div>
              <details class="advanced-block">
                <summary>检测参数</summary>
                <div class="filter-grid advanced-grid">
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
                  <label>基础间隔(ms)<input v-model.number="batchTestForm.delayMs" type="number" min="0" step="100" /></label>
                  <label>随机抖动(ms)<input v-model.number="batchTestForm.jitterMs" type="number" min="0" step="100" /></label>
                  <label class="inline-check"><input v-model="batchTestForm.logResponses" type="checkbox" /> 保存响应日志到日志目录</label>
                </div>
              </details>
              <details class="advanced-block danger-block">
                <summary>刷新令牌</summary>
                <p class="muted">会修改 sub2api 上游账号 credentials，建议先按测试分组筛选并确认范围。</p>
                <div class="primary-actions">
                  <button class="secondary" :disabled="batchRefreshing || selectedAccountCount === 0" @click="refreshSelectedAccountTokens">{{ batchRefreshing ? '刷新中...' : '刷新选中令牌' }}</button>
                  <button class="secondary" :disabled="batchRefreshing || batchCollecting || !activeSiteId" @click="refreshAllFilteredAccountTokens">{{ batchCollecting ? '收集中...' : '刷新当前筛选全部令牌' }}</button>
                </div>
              </details>
            </div>
          </details>
          <p v-if="batchCollecting" class="muted">正在按当前筛选条件分页收集账号 ID...</p>
          <p v-if="batchCollecting" class="muted">已扫描第 {{ batchCollectPage }} 页<span v-if="batchCollectTotalPages"> / 约 {{ batchCollectTotalPages }} 页</span>，已收集 {{ batchCollectFound }} 个账号 ID。</p>
          <p v-if="batchTesting" class="muted">正在执行批量检测任务；每个账号完成后会更新一行结果。大批量会自动提高最小间隔。</p>
          <p v-if="batchTestJob" class="muted">任务 #{{ batchTestJob.id }} · {{ jobStatusLabel(batchTestJob.status) }} · {{ batchTestDone }} / {{ batchTestTotal }} 完成 <button type="button" class="mini" @click="showJobs">查看任务</button></p>
          <p v-if="batchRefreshing" class="muted">正在刷新账号 OAuth 令牌；sub2api 批量刷新接口会修改上游账号凭证。</p>
          <p v-if="batchTestError" class="error">{{ batchTestError }}</p>
          <p v-if="batchRefreshError" class="error">{{ batchRefreshError }}</p>
          <p v-if="copyNotice" class="muted">{{ copyNotice }}</p>
          <section v-if="batchTestResults.length" class="batch-results result-panel">
            <div class="panel-head">
              <div>
                <h2>{{ taskResultTitle() }}</h2>
                <p class="muted">{{ batchTestDone }} / {{ batchTestTotal || batchTestResults.length }} 完成</p>
              </div>
              <div class="actions compact-actions">
                <button class="secondary" @click="copyText(JSON.stringify(batchTestResults, null, 2), '检测结果')">复制结果</button>
              <button class="secondary" :disabled="(!batchTesting && !batchRefreshing) || !batchTestJob" @click="cancelBatchTestJob">取消任务</button>
                <button class="secondary" :disabled="!failedBatchTestIDs.length || batchTesting" @click="copyFailedBatchTestIDs">复制失败账号 ID</button>
                <button class="secondary" :disabled="!failedBatchTestIDs.length || batchTesting" @click="retryFailedBatchTests">只重试失败项</button>
              </div>
            </div>
            <div class="active-filter-chips">
              <span class="muted">结果筛选：{{ batchResultFilterLabel() }}</span>
              <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'all' }" @click="batchResultFilter = 'all'">全部 {{ batchTestResults.length }}</button>
              <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'failed' }" @click="batchResultFilter = 'failed'">失败 {{ failedBatchTestIDs.length }}</button>
              <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'success' }" @click="batchResultFilter = 'success'">成功 {{ batchTestResults.filter((item) => item.ok === true).length }}</button>
              <button type="button" class="filter-chip" :class="{ active: batchResultFilter === 'pending' }" @click="batchResultFilter = 'pending'">进行中 {{ batchTestResults.filter(isBatchResultPending).length }}</button>
            </div>
            <div class="progress-track"><div class="progress-bar" :style="{ width: `${batchTestProgress}%` }"></div></div>
            <div v-if="batchTestFailureGroups.length" class="failure-groups">
              <span class="muted">失败分类</span>
              <button v-for="group in batchTestFailureGroups" :key="group.hint" class="mini" type="button" @click="copyBatchTestGroupIDs(group)">{{ group.hint }} {{ group.count }} 个</button>
            </div>
            <div ref="batchTestScroll" class="result-scroll table-wrap">
              <table class="account-table result-table">
                <thead><tr><th>账号</th><th>结果</th><th>操作</th><th>HTTP</th><th>耗时</th><th>提示</th><th>详情</th></tr></thead>
                <tbody>
                  <tr v-for="result in displayedBatchResults" :key="String(result.id)">
                    <td><div class="cell-stack"><span class="muted cell-subtitle">{{ result.id }}</span><strong class="account-name">{{ resultAccountName(result) }}</strong><span v-if="resultAccountNote(result)" class="muted account-note">{{ resultAccountNote(result) }}</span></div></td>
                    <td><span :class="batchResultTagClass(result)">{{ batchResultLabel(result) }}</span></td>
                    <td>{{ batchResultAction(result) }}</td>
                    <td>{{ result.statusCode || '未知' }}</td>
                    <td>{{ result.durationMs ?? '未知' }} ms</td>
                    <td>{{ batchResultHint(result) }}</td>
                    <td>
                      <button class="secondary mini" type="button" :title="batchResultSummary(result)" @click="openBatchTestDetail(result)">查看详情</button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
          <section v-if="batchRefreshing || batchRefreshResult" class="batch-results result-panel">
            <div class="panel-head">
              <div>
                <h2>批量刷新令牌结果</h2>
                <p class="muted">{{ batchRefreshDone }} / {{ batchRefreshTotal }} 完成</p>
              </div>
              <div class="actions compact-actions">
                <button v-if="batchRefreshResult" class="secondary" @click="copyText(JSON.stringify(batchRefreshResult, null, 2), '刷新令牌结果')">复制结果</button>
              </div>
            </div>
            <div class="progress-track"><div class="progress-bar" :class="{ indeterminate: batchRefreshing && batchRefreshDone === 0 }" :style="{ width: `${batchRefreshProgress}%` }"></div></div>
            <div ref="batchRefreshScroll" class="result-scroll refresh-result-scroll">
              <div v-if="batchRefreshResult" class="result-grid">
                <div><span class="muted">总数</span><strong>{{ batchRefreshResult.total ?? '未知' }}</strong></div>
                <div><span class="muted">成功</span><strong>{{ batchRefreshResult.success ?? '未知' }}</strong></div>
                <div><span class="muted">失败</span><strong>{{ batchRefreshResult.failed ?? '未知' }}</strong></div>
              </div>
              <p v-else class="muted">正在等待 sub2api 批量刷新返回结果...</p>
              <pre v-if="batchRefreshResult" class="inline-json result-json">{{ JSON.stringify(batchRefreshResult.errors || batchRefreshResult.warnings || batchRefreshResult, null, 2) }}</pre>
            </div>
          </section>
          <div class="table-shell">
            <div v-if="accountsLoading" class="table-overlay" aria-live="polite">
              <div class="overlay-card">
                <div class="overlay-spinner"></div>
                <strong>正在加载账号列表</strong>
                <span v-if="!accountQuerySlow">请稍候，当前页内容即将更新。</span>
                <span v-else>sub2api 仍在处理，已等待 {{ accountQueryElapsedSeconds }} 秒。</span>
                <button type="button" class="secondary" @click="cancelAccountQuery">取消本次查询</button>
              </div>
            </div>
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
                  <td>
                    <div class="cell-stack">
                      <span class="muted cell-subtitle">{{ account.id || '未知 ID' }}</span>
                      <strong class="account-name">{{ accountName(account) }}</strong>
                      <span v-if="accountNote(account)" class="muted account-note">{{ accountNote(account) }}</span>
                    </div>
                  </td>
                  <td>
                    <div class="cell-stack compact">
                      <div class="chip-row">
                        <span class="tag">{{ platformLabel(account.platform) }}</span>
                        <span class="tag muted-tag">{{ accountTypeLabel(account.type) }}</span>
                      </div>
                      <span class="tag" :class="`tag-${accountStatusTone(account)}`">{{ accountStatusLabel(account) }}</span>
                    </div>
                  </td>
                  <td>
                    <div class="cell-stack compact">
                      <div class="chip-row wrap group-preview-wrap">
                        <template v-if="accountGroupPreview(account).items.length">
                          <button v-for="group in accountGroupPreview(account).items" :key="group.id" type="button" class="chip chip-button" @click="addIncludeGroupFilter(group.id)">{{ group.name }}</button>
                          <button v-if="accountGroupPreview(account).extra" type="button" class="chip muted-chip chip-more chip-button" @click.stop="toggleFloatingGroupPopover(account, $event)">+{{ accountGroupPreview(account).extra }}</button>
                        </template>
                        <span v-else class="muted">未分组</span>
                      </div>
                    </div>
                  </td>
                  <td>
                    <div class="cell-stack compact">
                      <span class="tag">{{ accountProxy(account) }}</span>
                      <span class="tag" :class="`tag-${accountScheduleTone(account)}`">{{ accountSchedule(account) }}</span>
                    </div>
                  </td>
                  <td>
                    <div class="cell-stack compact">
                      <div v-for="metric in accountUsageMetrics(account)" :key="metric.label" class="usage-row">
                        <span class="usage-meta">{{ metric.label }}</span>
                        <div class="usage-bar-shell">
                          <div class="usage-bar" :class="{ unknown: metric.value === null }" :style="{ width: `${usagePercentWidth(metric.value)}%` }"></div>
                        </div>
                        <strong class="usage-value">{{ metric.text }}</strong>
                      </div>
                      <span class="muted cell-subtitle">最近使用：{{ accountLastUsedLabel(account) }}</span>
                    </div>
                  </td>
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
            <div class="mobile-account-list">
              <article v-for="account in visibleAccounts" :key="`mobile-${String(account.id || accountName(account))}`" class="mobile-account-card">
                <div class="mobile-account-head">
                  <input type="checkbox" :checked="selectedAccountIds.has(accountID(account))" @change="toggleAccountSelection(account)" />
                  <div class="mobile-account-title">
                    <span class="muted cell-subtitle">{{ account.id || '未知 ID' }}</span>
                    <strong>{{ accountName(account) }}</strong>
                    <span v-if="accountNote(account)" class="muted account-note">{{ accountNote(account) }}</span>
                  </div>
                </div>
                <div class="mobile-chip-grid">
                  <span class="tag">{{ platformLabel(account.platform) }}</span>
                  <span class="tag muted-tag">{{ accountTypeLabel(account.type) }}</span>
                  <span class="tag" :class="`tag-${accountStatusTone(account)}`">{{ accountStatusLabel(account) }}</span>
                  <span class="tag" :class="`tag-${accountScheduleTone(account)}`">{{ accountSchedule(account) }}</span>
                </div>
                <div class="mobile-meta-block">
                  <span class="muted">分组</span>
                  <div class="chip-row wrap">
                    <template v-if="accountGroupPreview(account).items.length">
                      <button v-for="group in accountGroupPreview(account).items" :key="group.id" type="button" class="chip chip-button" @click="addIncludeGroupFilter(group.id)">{{ group.name }}</button>
                      <button v-if="accountGroupPreview(account).extra" type="button" class="chip muted-chip chip-button" @click.stop="toggleFloatingGroupPopover(account, $event)">+{{ accountGroupPreview(account).extra }}</button>
                    </template>
                    <span v-else class="muted">未分组</span>
                  </div>
                </div>
                <div class="mobile-meta-grid">
                  <div><span class="muted">代理</span><strong>{{ accountProxy(account) }}</strong></div>
                  <div><span class="muted">最近使用</span><strong>{{ accountLastUsedLabel(account) }}</strong></div>
                </div>
                <div class="mobile-usage-list">
                  <div v-for="metric in accountUsageMetrics(account)" :key="metric.label" class="usage-row">
                    <span class="usage-meta">{{ metric.label }}</span>
                    <div class="usage-bar-shell">
                      <div class="usage-bar" :class="{ unknown: metric.value === null }" :style="{ width: `${usagePercentWidth(metric.value)}%` }"></div>
                    </div>
                    <strong class="usage-value">{{ metric.text }}</strong>
                  </div>
                </div>
                <div class="mobile-account-actions">
                  <button class="secondary" @click="openAccountDetail(account)">详情</button>
                  <button class="secondary" @click="copyText(account.id, '账号 ID')">复制 ID</button>
                  <button class="secondary" @click="copyText(accountName(account), '账号名称')">复制名称</button>
                </div>
              </article>
              <p v-if="!visibleAccounts.length" class="muted mobile-empty">{{ accountPager.loaded ? '没有匹配的账号。' : '请选择站点并查询账号。' }}</p>
            </div>
          </div>
          <p v-if="accountPager.loaded && visibleAccounts.length !== accounts.length" class="muted">当前页本地筛选命中 {{ visibleAccounts.length }} / {{ accounts.length }} 条。</p>
          <div class="pager">
            <button class="secondary" :disabled="accountPager.page <= 1 || accountsLoading" @click="goFirstAccounts">首页</button>
            <button class="secondary" :disabled="accountPager.page <= 1 || accountsLoading" @click="goPrevAccounts">上一页</button>
            <button v-for="page in accountPageButtons" :key="page" class="secondary" :class="{ active: page === accountPager.page }" :disabled="page === accountPager.page || accountsLoading" @click="goAccountPage(page)">{{ page }}</button>
            <span class="muted">第 {{ accountPager.page }} 页<span v-if="accountTotalPages"> / 共 {{ accountTotalPages }} 页</span></span>
            <button class="secondary" :disabled="!hasNextAccountPage || accountsLoading" @click="goNextAccounts">下一页</button>
            <button class="secondary" :disabled="!hasNextAccountPage || accountsLoading" @click="goLastAccounts">末页</button>
            <label class="jump-label muted">跳页<input v-model="accountPageJump" type="number" min="1" :max="accountTotalPages || undefined" @keyup.enter="jumpToAccountPage" /></label>
            <button class="secondary" :disabled="!accountTotalPages || accountsLoading" @click="jumpToAccountPage">跳转</button>
            <span v-if="accountPager.total" class="muted">共 {{ accountPager.total }} 条</span>
          </div>
        </section>
      </div>
    </section>

    <div v-if="groupPopoverAccount" class="group-popover floating-group-popover" :style="{ top: `${groupPopoverPosition.top}px`, left: `${groupPopoverPosition.left}px` }">
      <div class="popover-head">
        <span class="muted popover-label">完整分组</span>
        <button type="button" class="mini" @click.stop="closeGroupPopover">关闭</button>
      </div>
      <button v-for="group in accountGroupEntries(groupPopoverAccount)" :key="`${accountID(groupPopoverAccount)}-${group.id}`" type="button" class="chip chip-button popover-chip" @click.stop="addIncludeGroupFilter(group.id)">{{ group.name }}</button>
    </div>

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

    <div v-if="showBatchTestModal && selectedBatchTestResult" class="modal-mask">
      <section class="modal-card detail-card">
        <h2>检测详情</h2>
        <p class="muted">账号 ID：{{ selectedBatchTestResult.id || '未知' }}</p>
        <div class="detail-sections">
          <section class="detail-section">
            <h3>结果</h3>
            <div class="detail-grid">
              <span>状态</span><strong><span :class="batchResultTagClass(selectedBatchTestResult)">{{ batchResultLabel(selectedBatchTestResult) }}</span></strong>
              <span>提示</span><strong>{{ batchResultHint(selectedBatchTestResult) }}</strong>
              <span>模型</span><strong>{{ selectedBatchTestResult.model || '未知' }}</strong>
              <span>HTTP</span><strong>{{ selectedBatchTestResult.statusCode || '未知' }}</strong>
              <span>耗时</span><strong>{{ selectedBatchTestResult.durationMs ?? '未知' }} ms</strong>
              <span>重置时间</span><strong>{{ selectedBatchTestResult.resetAt || '未知' }}</strong>
            </div>
          </section>
          <section class="detail-section">
            <h3>摘要</h3>
            <p class="result-message">{{ selectedBatchTestResult.message || selectedBatchTestResult.error || '无响应内容' }}</p>
          </section>
        </div>
        <pre class="detail-json">{{ batchResultDetailJSON(selectedBatchTestResult) }}</pre>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="copyText(batchResultDetailJSON(selectedBatchTestResult), '检测详情')">复制详情</button>
          <button type="button" class="secondary" @click="showBatchTestModal = false">关闭</button>
        </div>
      </section>
    </div>

    <div v-if="confirmDialog.open" class="modal-mask" @click.self="closeConfirmFromBackdrop">
      <section class="modal-card confirm-card">
        <h2>{{ confirmDialog.title }}</h2>
        <p>{{ confirmDialog.message }}</p>
        <pre v-if="confirmDialog.detail" class="detail-json">{{ confirmDialog.detail }}</pre>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="resolveConfirm(false)">{{ confirmDialog.cancelText }}</button>
          <button type="button" :class="confirmDialog.confirmText.includes('删除') ? 'danger' : ''" @click="resolveConfirm(true)">{{ confirmDialog.confirmText }}</button>
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
.layout-grid.single-column { grid-template-columns: 1fr; }
.panel { padding: 18px; }
.content-panel { min-height: 520px; }
.stats-panel { display: grid; gap: 14px; }
.stats-controls { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, auto)); gap: 10px; align-items: end; padding: 12px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: 16px; background: rgba(2, 6, 23, 0.22); }
.stats-controls label { min-width: 150px; }
.concurrency-meta { display: flex !important; flex-wrap: wrap; gap: 8px; align-items: center; }
.inline-refresh { margin-left: 0; }
.stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; }
.stat-panel-card { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(2, 6, 23, 0.24); }
.stat-panel-card.wide-card { grid-column: span 2; }
.stat-panel-card span { color: #94a3b8; font-size: 13px; }
.stat-panel-card strong { display: block; margin-top: 8px; color: #fff; font-size: 22px; }
.stat-panel-card p { margin: 8px 0 0; }
.stat-lines { display: grid; gap: 3px; }
.stats-sections { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 14px; }
.trend-panel { display: grid; gap: 12px; }
.trend-chart-card { display: grid; gap: 10px; }
.trend-chart-frame { overflow: auto; border: 1px solid rgba(148, 163, 184, 0.12); border-radius: 14px; background: rgba(2, 6, 23, 0.2); }
.trend-chart-frame svg { width: 100%; min-width: 600px; min-height: 240px; padding: 4px 0; overflow: visible; }
.chart-axis { stroke: rgba(148, 163, 184, 0.38); stroke-width: 1; }
.chart-grid { stroke: rgba(148, 163, 184, 0.18); stroke-width: 1; stroke-dasharray: 4 5; }
.chart-line { fill: none; stroke-width: 3; stroke-linecap: round; stroke-linejoin: round; }
.chart-line.tokens { stroke: #8b5cf6; }
.chart-line.requests { stroke: #22d3ee; }
.chart-tick { fill: #94a3b8; font-size: 11px; }
.chart-hover-line { stroke: rgba(226, 232, 240, 0.35); stroke-width: 1; stroke-dasharray: 4 4; }
.chart-dot.tokens { fill: #8b5cf6; stroke: #0f172a; stroke-width: 2; }
.chart-dot.requests { fill: #22d3ee; stroke: #0f172a; stroke-width: 2; }
.chart-tooltip { display: grid; gap: 4px; box-sizing: border-box; padding: 9px 10px; border: 1px solid rgba(148, 163, 184, 0.24); border-radius: 12px; background: rgba(2, 6, 23, 0.88); color: #e2e8f0; font-size: 12px; }
.chart-tooltip strong { color: #fff; }
.chart-legend-row { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 12px; }
.chart-legend, .chart-labels { display: flex; flex-wrap: wrap; gap: 12px; justify-content: space-between; color: #94a3b8; font-size: 12px; }
.trend-summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 10px; }
.trend-summary-card { padding: 12px 14px; border: 1px solid rgba(148, 163, 184, 0.12); border-radius: 14px; background: rgba(2, 6, 23, 0.2); }
.trend-summary-card span { display: block; color: #94a3b8; font-size: 12px; }
.trend-summary-card strong { display: block; margin-top: 6px; color: #fff; font-size: 20px; }
.legend-token::before, .legend-request::before { content: ''; display: inline-block; width: 10px; height: 10px; margin-right: 6px; border-radius: 99px; }
.legend-token::before { background: #8b5cf6; }
.legend-request::before { background: #22d3ee; }
.full-scroll { overflow: auto; max-width: 100%; }
.full-scroll .mini-table { min-width: 620px; table-layout: fixed; }
.fit-table .mini-table { min-width: 320px; }
.name-col { width: 28%; }
.num-col { width: 14%; }
.money-col { width: 15%; }
.name-cell, .ranking-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.num-cell { text-align: right; white-space: nowrap; }
.ranking-table .name-col { width: 34%; }
.ranking-table .usage-col { width: auto; }
.ranking-usage { display: grid; gap: 6px; }
.ranking-usage span { color: #94a3b8; font-size: 12px; }
.ranking-usage b { color: #c4b5fd; font-size: 15px; line-height: 0; }
.full-scroll::-webkit-scrollbar, .trend-chart-frame::-webkit-scrollbar { height: 10px; width: 10px; }
.full-scroll::-webkit-scrollbar-track, .trend-chart-frame::-webkit-scrollbar-track { background: rgba(15, 23, 42, 0.45); border-radius: 999px; }
.full-scroll::-webkit-scrollbar-thumb, .trend-chart-frame::-webkit-scrollbar-thumb { background: rgba(148, 163, 184, 0.34); border: 2px solid transparent; background-clip: padding-box; border-radius: 999px; }
.full-scroll::-webkit-scrollbar-thumb:hover, .trend-chart-frame::-webkit-scrollbar-thumb:hover { background: rgba(148, 163, 184, 0.5); border: 2px solid transparent; background-clip: padding-box; }
.status-chip-row { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
.status-tag { display: inline-flex; align-items: center; gap: 6px; padding: 5px 10px; border-radius: 999px; font-size: 12px; font-weight: 700; }
.status-tag-ok { background: rgba(34, 197, 94, 0.16); color: #86efac; }
.status-tag-warn { background: rgba(234, 179, 8, 0.18); color: #fde68a; }
.status-tag-danger { background: rgba(239, 68, 68, 0.18); color: #fecaca; }
.status-tag-overload { background: rgba(249, 115, 22, 0.18); color: #fdba74; }
.stats-table-wrap { overflow: auto; border-radius: 12px; border: 1px solid rgba(148, 163, 184, 0.12); }
.mini-table { width: 100%; border-collapse: collapse; min-width: 560px; }
.mini-table th, .mini-table td { padding: 10px 12px; border-bottom: 1px solid rgba(148, 163, 184, 0.1); text-align: left; font-size: 13px; }
.mini-table th { color: #c4b5fd; background: rgba(2, 6, 23, 0.28); }
.mini-table td { color: #e2e8f0; }
.docs-panel { display: grid; gap: 14px; }
.docs-links { display: flex; flex-wrap: wrap; gap: 10px; }
.docs-reader { display: grid; gap: 12px; margin-top: 8px; }
.docs-reader pre { margin: 0; max-height: 620px; overflow: auto; padding: 16px; border-radius: 14px; background: rgba(2, 6, 23, 0.55); color: #dbeafe; line-height: 1.55; white-space: pre-wrap; }
.panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 14px; }
.refresh-meta { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; justify-content: flex-end; }
.eyebrow { margin: 0 0 8px; color: #c4b5fd; letter-spacing: 0.16em; text-transform: uppercase; font-size: 12px; }
h1, h2 { margin: 0; }
h3 { margin: 0 0 10px; font-size: 15px; }
.muted { color: #94a3b8; }
.error { color: #fecaca; }
.form-block, .filter-grid, .modal-card { display: grid; gap: 14px; }
.filter-grid { grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); align-items: end; }
.section-card { display: grid; gap: 12px; margin: 14px 0; padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(15, 23, 42, 0.46); }
.section-title { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; justify-content: space-between; }
.section-title p { margin: 6px 0 0; }
.form-actions, .primary-actions { display: flex; flex-wrap: wrap; gap: 10px; align-items: end; }
.advanced-block { margin-top: 4px; padding: 12px; border: 1px solid rgba(148, 163, 184, 0.12); border-radius: 14px; background: rgba(2, 6, 23, 0.2); }
.advanced-block summary { cursor: pointer; color: #ddd6fe; font-weight: 700; }
.advanced-block > *:not(summary) { margin-top: 12px; }
.advanced-grid { margin-top: 12px; }
.danger-block { border-color: rgba(248, 113, 113, 0.2); }
.batch-section { background: linear-gradient(135deg, rgba(30, 41, 59, 0.66), rgba(15, 23, 42, 0.5)); }
.batch-details { display: block; }
.batch-summary { cursor: pointer; list-style: none; margin: 0; }
.batch-summary::-webkit-details-marker { display: none; }
.batch-body { display: grid; gap: 12px; margin-top: 12px; }
.summary-toggle::after { content: '展开'; color: #c4b5fd; font-size: 13px; font-weight: 700; }
.batch-details[open] .summary-toggle::after { content: '收起'; }
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
input, select, textarea {
  width: 100%; box-sizing: border-box; padding: 12px 13px; border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 12px; color: #f8fafc; background: rgba(15, 23, 42, 0.86); outline: none;
}
textarea { resize: vertical; min-height: 180px; font: inherit; line-height: 1.5; }
input[type="checkbox"] { width: auto; }
.inline-check { display: flex; grid-auto-flow: column; grid-template-columns: auto 1fr; align-items: center; }
.chip-check { width: auto; display: inline-flex; padding: 8px 10px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: 999px; background: rgba(15, 23, 42, 0.42); }
button {
  border: 0; border-radius: 12px; padding: 11px 14px; color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb);
  cursor: pointer; font-weight: 700;
}
button:disabled { opacity: 0.5; cursor: not-allowed; }
.button-link { display: inline-block; border-radius: 12px; padding: 10px 12px; color: #fff; font-weight: 700; text-decoration: none; }
.secondary { background: rgba(148, 163, 184, 0.14); color: #e2e8f0; }
.secondary.active { background: linear-gradient(135deg, #7c3aed, #2563eb); color: #fff; }
.mini { margin-left: 8px; padding: 5px 8px; border-radius: 9px; background: rgba(148, 163, 184, 0.14); color: #e2e8f0; font-size: 12px; }
.danger { background: rgba(239, 68, 68, 0.16); color: #fecaca; }
.site-list, .account-list { display: grid; gap: 12px; }
.filter-summary { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.active-filter-chips { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin: 8px 0 12px; }
.filter-chip { padding: 6px 10px; border-radius: 999px; background: rgba(59, 130, 246, 0.14); color: #dbeafe; font-size: 12px; }
.filter-chip.active { background: linear-gradient(135deg, rgba(124, 58, 237, 0.9), rgba(37, 99, 235, 0.86)); color: #fff; }
.tag-picker .filter-chip { border: 1px solid rgba(147, 197, 253, 0.16); }
.import-setting-card { background: linear-gradient(135deg, rgba(30, 41, 59, 0.68), rgba(2, 6, 23, 0.28)); }
.confirm-card { width: min(560px, calc(100vw - 32px)); }
.filter-chip span { margin-left: 6px; color: #93c5fd; }
.query-notice { margin: 6px 0 12px; }
.batch-toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 12px 0; padding: 12px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: 14px; background: rgba(2, 6, 23, 0.22); }
.batch-results { display: grid; gap: 10px; margin-top: 14px; }
.result-panel { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(2, 6, 23, 0.24); }
.failure-groups { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.result-scroll { max-height: 320px; overflow: auto; border-radius: 12px; border: 1px solid rgba(148, 163, 184, 0.12); }
.refresh-result-scroll { padding: 12px; background: rgba(15, 23, 42, 0.28); }
.result-table { min-width: 860px; }
.progress-track { height: 10px; overflow: hidden; border-radius: 999px; background: rgba(148, 163, 184, 0.18); }
.progress-bar { height: 100%; border-radius: inherit; background: linear-gradient(90deg, #22c55e, #38bdf8, #8b5cf6); transition: width 0.2s ease; }
.progress-bar.indeterminate { width: 42% !important; animation: progress-slide 1.2s ease-in-out infinite; }
.result-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 10px; }
.result-grid div { display: grid; gap: 4px; padding: 12px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: 12px; background: rgba(15, 23, 42, 0.45); }
.result-json { max-width: none; margin-top: 10px; }
.row-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.table-shell { position: relative; }
.table-overlay {
  position: absolute; inset: 0; z-index: 4; display: grid; place-items: center; min-height: 180px;
  background: rgba(2, 6, 23, 0.38); backdrop-filter: blur(2px);
}
.overlay-card {
  display: grid; justify-items: center; gap: 8px; padding: 18px 20px; border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 16px; background: rgba(15, 23, 42, 0.94); box-shadow: 0 20px 60px rgba(0, 0, 0, 0.36);
}
.overlay-card span { color: #94a3b8; font-size: 13px; }
.overlay-spinner { width: 22px; height: 22px; border-radius: 999px; border: 2px solid rgba(148, 163, 184, 0.24); border-top-color: #8b5cf6; animation: spin 0.9s linear infinite; }
.table-wrap { overflow-x: auto; margin-top: 12px; }
.mobile-account-list { display: none; }
.account-table { width: 100%; min-width: 720px; border-collapse: collapse; }
.account-table th,
.account-table td { padding: 12px 10px; border-bottom: 1px solid rgba(148, 163, 184, 0.16); text-align: left; vertical-align: top; }
.account-table th { color: #cbd5e1; font-size: 14px; }
.account-table td:last-child { white-space: nowrap; }
.account-table td:nth-child(2),
.account-table td:nth-child(3),
.account-table td:nth-child(4),
.account-table td:nth-child(5),
.account-table td:nth-child(6) { min-width: 150px; }
.account-table td:nth-child(2) { max-width: 300px; }
.account-name { display: block; max-width: 280px; overflow-wrap: anywhere; line-height: 1.35; }
.account-note { display: block; max-width: 280px; overflow-wrap: anywhere; line-height: 1.4; }
.cell-stack { display: grid; gap: 6px; }
.cell-stack.compact { gap: 8px; }
.cell-subtitle { font-size: 12px; }
.chip-row { display: flex; flex-wrap: nowrap; gap: 6px; align-items: center; overflow: hidden; }
.chip-row.wrap { flex-wrap: wrap; }
.tag, .chip {
  display: inline-flex; align-items: center; max-width: 100%; padding: 4px 8px; border-radius: 999px;
  background: rgba(148, 163, 184, 0.14); color: #e2e8f0; font-size: 12px; line-height: 1.2; white-space: nowrap;
}
.chip { background: rgba(59, 130, 246, 0.14); color: #dbeafe; }
.chip-button { border: 0; cursor: pointer; }
.tag-success { background: rgba(34, 197, 94, 0.16); color: #bbf7d0; }
.tag-warning { background: rgba(245, 158, 11, 0.16); color: #fde68a; }
.tag-danger { background: rgba(239, 68, 68, 0.16); color: #fecaca; }
.tag-info { background: rgba(14, 165, 233, 0.16); color: #bae6fd; }
.tag-neutral { background: rgba(148, 163, 184, 0.14); color: #e2e8f0; }
.muted-tag, .muted-chip { opacity: 0.78; }
.group-preview-wrap { position: relative; overflow: visible; }
.group-popover {
  min-width: 220px; max-width: 380px; display: flex; flex-wrap: wrap; gap: 8px; padding: 12px;
  border: 1px solid rgba(148, 163, 184, 0.18); border-radius: 14px; background: rgba(15, 23, 42, 0.98);
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.34);
}
.floating-group-popover {
  position: absolute; z-index: 80; max-height: 360px; overflow: auto;
}
.popover-head { width: 100%; display: flex; gap: 10px; align-items: center; justify-content: space-between; }
.popover-label { width: 100%; font-size: 12px; }
.popover-head .popover-label { width: auto; }
.popover-chip { white-space: normal; }
.usage-row { display: grid; grid-template-columns: 36px minmax(90px, 1fr) 54px; gap: 8px; align-items: center; }
.usage-meta { color: #cbd5e1; font-size: 12px; }
.usage-bar-shell { height: 8px; overflow: hidden; border-radius: 999px; background: rgba(148, 163, 184, 0.18); }
.usage-bar { height: 100%; border-radius: inherit; background: linear-gradient(90deg, #22c55e, #38bdf8, #8b5cf6); }
.usage-bar.unknown { width: 0 !important; background: rgba(148, 163, 184, 0.42); }
.usage-value { color: #f8fafc; font-size: 12px; }
.pager { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-top: 12px; }
.pager .active { background: linear-gradient(135deg, #8b5cf6, #2563eb); }
.jump-label { display: inline-flex; flex-direction: row; gap: 8px; align-items: center; }
.jump-label input { width: 90px; padding: 9px 10px; }
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
.result-message { margin: 0; color: #dbeafe; overflow-wrap: anywhere; line-height: 1.6; }
.detail-json { overflow: auto; max-height: 360px; padding: 14px; border-radius: 14px; background: rgba(2, 6, 23, 0.55); color: #dbeafe; font-size: 12px; line-height: 1.55; }
.inline-json { max-width: 520px; max-height: 160px; overflow: auto; margin: 0; white-space: pre-wrap; color: #dbeafe; font-size: 12px; }
.modal-actions { justify-content: flex-end; }
@media (max-width: 900px) {
  .layout-grid { grid-template-columns: 1fr; }
  .topbar { align-items: flex-start; flex-direction: column; }
  .topbar-actions { width: 100%; justify-content: flex-start; }
  .overview-card.wide, .stat-panel-card.wide-card { grid-column: auto; }
  .stats-sections { grid-template-columns: 1fr; }
  .account-table { min-width: 960px; }
}
@media (max-width: 640px) {
  .app-shell { padding: 12px; }
  .login-card { margin: 6vh auto; padding: 20px; }
  .topbar, .panel, .modal-card { border-radius: 18px; padding: 14px; }
  .topbar-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .topbar-actions button { padding: 10px 8px; }
  .overview-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .overview-card { padding: 12px; }
  .overview-card strong { font-size: 18px; }
  .overview-card p { font-size: 12px; }
  .filter-grid, .advanced-grid { grid-template-columns: 1fr; }
  .section-card, .local-filter, .batch-toolbar { padding: 12px; border-radius: 14px; }
  .panel-head { align-items: flex-start; flex-direction: column; }
  .panel-head > button, .panel-head .actions { width: 100%; }
  .actions, .form-actions, .primary-actions, .modal-actions { display: grid; grid-template-columns: 1fr; align-items: stretch; }
  .actions button, .form-actions button, .primary-actions button, .modal-actions button { width: 100%; }
  .site-title { align-items: flex-start; flex-direction: column; }
  .group-add-row { grid-template-columns: 1fr; }
  .group-options { grid-template-columns: 1fr; }
  .group-option { align-items: flex-start; flex-direction: column; }
  .segmented { width: 100%; display: grid; grid-template-columns: repeat(3, 1fr); }
  .table-wrap { margin-left: -2px; margin-right: -2px; }
  .table-wrap { display: none; }
  .mobile-account-list { display: grid; gap: 12px; margin-top: 12px; }
  .mobile-account-card { display: grid; gap: 12px; padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(15, 23, 42, 0.58); }
  .mobile-account-head { display: grid; grid-template-columns: auto 1fr; gap: 10px; align-items: start; }
  .mobile-account-title { min-width: 0; display: grid; gap: 4px; }
  .mobile-account-title strong { overflow-wrap: anywhere; line-height: 1.35; }
  .mobile-chip-grid { display: flex; flex-wrap: wrap; gap: 6px; }
  .mobile-meta-block { display: grid; gap: 6px; }
  .mobile-meta-grid { display: grid; grid-template-columns: 1fr; gap: 8px; }
  .mobile-meta-grid div { display: grid; gap: 4px; padding: 10px; border-radius: 12px; background: rgba(2, 6, 23, 0.22); }
  .mobile-meta-grid strong { overflow-wrap: anywhere; font-size: 13px; }
  .mobile-usage-list { display: grid; gap: 8px; padding: 10px; border-radius: 12px; background: rgba(2, 6, 23, 0.22); }
  .mobile-account-actions { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .mobile-account-actions button { padding: 9px 8px; font-size: 12px; }
  .mobile-empty { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 14px; background: rgba(15, 23, 42, 0.4); }
  .pager { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .pager > * { width: 100%; justify-content: center; }
  .jump-label { grid-column: span 2; justify-content: space-between; }
  .jump-label input { width: 130px; }
  .docs-links { display: grid; grid-template-columns: 1fr; }
  .docs-reader pre { max-height: 520px; font-size: 12px; }
  .modal-mask { align-items: start; padding: 12px; overflow: auto; }
  .detail-card { max-height: none; }
  .detail-grid { grid-template-columns: 86px 1fr; gap: 8px 10px; }
  .floating-group-popover { left: 12px !important; right: 12px; max-width: none; min-width: 0; }
}
@keyframes progress-slide {
  0% { transform: translateX(-110%); }
  100% { transform: translateX(260%); }
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
