<script setup lang="ts">
import { ref, computed } from 'vue'
import { api } from '../apiClient'

const props = defineProps<{ activeSiteId: number | null }>()
const emit = defineEmits<{ (e: 'open-detail', id: number): void }>()

type SearchResult = {
  keyword: string
  accounts: { id: number; name: string; email?: string; platform?: string; type?: string; status?: string; [k: string]: unknown }[]
  total: number
  returned: number
  truncated: boolean
  error?: string
  statusCode?: number
}

const inputText = ref('')
const skipComments = ref(true)
const loading = ref(false)
const error = ref('')
const results = ref<SearchResult[]>([])
const expandedKeyword = ref<string | null>(null)

async function search() {
  if (!props.activeSiteId) {
    error.value = '请先选择站点'
    return
  }
  const names = inputText.value.split('\n').map(l => l.trim()).filter(Boolean)
  if (!names.length) {
    error.value = '请输入至少一个关键词'
    return
  }
  error.value = ''
  loading.value = true
  results.value = []
  try {
    const payload = await api<{ items: SearchResult[] }>(`api/sites/${props.activeSiteId}/accounts/search-by-names`, {
      method: 'POST',
      body: JSON.stringify({ names, skipComments: skipComments.value }),
    })
    results.value = payload.items || []
    if (results.value.length > 0 && expandedKeyword.value === null) {
      expandedKeyword.value = results.value[0].keyword
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '搜索失败'
  } finally {
    loading.value = false
  }
}

const totalAccounts = computed(() => results.value.reduce((s, r) => s + r.accounts.length, 0))
const totalKeywords = computed(() => results.value.length)
const errorKeywords = computed(() => results.value.filter(r => r.error).length)

function toggleExpand(keyword: string) {
  expandedKeyword.value = expandedKeyword.value === keyword ? null : keyword
}

function statusClass(status?: string) {
  if (!status) return ''
  if (status === 'active') return 'status-tag status-tag-ok'
  if (status === 'error') return 'status-tag status-tag-danger'
  if (status === 'rate_limited') return 'status-tag status-tag-warn'
  return 'status-tag'
}
</script>

<template>
  <section class="sa-panel">
    <div class="sa-panel-head">
      <div>
        <h2>账号搜索</h2>
        <p class="sa-muted">按关键词批量搜索账号，每行一个关键词，# 开头的行为注释。</p>
      </div>
    </div>
    <div style="display:flex;flex-wrap:wrap;gap:12px;align-items:start;margin-bottom:12px">
      <div style="flex:1;min-width:240px">
        <textarea v-model="inputText" class="sa-input" rows="6" placeholder="每行一个关键词&#10;# 注释行&#10;user@example.com&#10;account-name" style="width:100%;resize:vertical;font-family:monospace;font-size:13px" @keydown.ctrl.enter="search" />
        <label style="display:flex;align-items:center;gap:6px;margin-top:6px;font-size:13px">
          <input type="checkbox" v-model="skipComments" /> 跳过 # 注释行
        </label>
      </div>
      <div style="display:flex;flex-direction:column;gap:8px">
        <button class="sa-btn" :disabled="loading || !activeSiteId" @click="search">{{ loading ? '搜索中...' : '搜索' }}</button>
        <span class="sa-muted" style="font-size:12px">Ctrl+Enter 快捷搜索</span>
      </div>
    </div>
    <p v-if="error" class="sa-error">{{ error }}</p>
    <div v-if="results.length" style="margin-bottom:12px;display:flex;flex-wrap:wrap;gap:8px;align-items:center">
      <span class="sa-muted">{{ totalKeywords }} 个关键词 · {{ totalAccounts }} 个账号 · {{ errorKeywords }} 个失败</span>
    </div>
    <div v-for="group in results" :key="group.keyword" class="search-group">
      <div class="search-group-head" @click="toggleExpand(group.keyword)">
        <span class="search-toggle">{{ expandedKeyword === group.keyword ? '▼' : '▶' }}</span>
        <strong>{{ group.keyword }}</strong>
        <span class="sa-muted" style="font-size:12px">
          <template v-if="group.error">
            <span class="sa-error">失败: {{ group.error }}</span>
          </template>
          <template v-else>
            {{ group.accounts.length }} 个账号<template v-if="group.truncated"> (共 {{ group.total }}，仅显示前 {{ group.returned }})</template>
          </template>
        </span>
      </div>
      <div v-if="expandedKeyword === group.keyword && !group.error" class="search-group-body">
        <table class="sa-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>名称/邮箱</th>
              <th>平台/类型</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="acc in group.accounts" :key="acc.id">
              <td>{{ acc.id }}</td>
              <td>
                <div class="sa-cell-stack">
                  <strong>{{ acc.name || '—' }}</strong>
                  <span class="sa-muted sa-cell-subtitle">{{ acc.email || '' }}</span>
                </div>
              </td>
              <td>
                <div style="display:flex;gap:6px;align-items:center">
                  <span class="sa-tag">{{ acc.platform || '?' }}</span>
                  <span class="sa-tag sa-tag-muted">{{ acc.type || '?' }}</span>
                </div>
              </td>
              <td><span :class="statusClass(acc.status)">{{ acc.status || '—' }}</span></td>
              <td><button class="sa-btn sa-btn-mini" @click="emit('open-detail', acc.id)">详情</button></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <p v-if="!results.length && !loading && !error" class="sa-muted">输入关键词并点击搜索。</p>
  </section>
</template>

<style scoped>
.search-group { border: 1px solid rgba(148,163,184,0.16); border-radius: 12px; margin-bottom: 10px; overflow: hidden; }
.search-group-head { display: flex; gap: 10px; align-items: center; padding: 10px 14px; cursor: pointer; background: rgba(15,23,42,0.4); }
.search-group-head:hover { background: rgba(15,23,42,0.6); }
.search-toggle { font-size: 11px; color: #94a3b8; }
.search-group-body { padding: 0 14px 14px; }
</style>
