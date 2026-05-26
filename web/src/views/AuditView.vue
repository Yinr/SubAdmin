<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../apiClient'
import { formatDateTime } from '../formatters'

const auditLogs = ref<Record<string, unknown>[]>([])
const auditLoading = ref(false)
const auditError = ref('')

loadAuditLogs()

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

function auditSummaryText(value: unknown) {
  if (!value || typeof value !== 'object') return '{}'
  return JSON.stringify(value)
}
</script>

<template>
  <section class="panel content-panel">
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
</template>

<style scoped>
.panel { padding: 18px; border: 1px solid rgba(148, 163, 184, 0.18); border-radius: 22px; background: rgba(15, 23, 42, 0.88); box-shadow: 0 24px 80px rgba(0, 0, 0, 0.28); }
.content-panel { min-height: 520px; }
.panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 14px; }
h2 { margin: 0; }
.muted { color: #94a3b8; }
.error { color: #fecaca; }
button { border: 0; border-radius: 12px; padding: 11px 14px; color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb); cursor: pointer; font-weight: 700; }
button:disabled { opacity: 0.5; cursor: not-allowed; }
.secondary { background: rgba(148, 163, 184, 0.14); color: #e2e8f0; }
.table-wrap { overflow-x: auto; margin-top: 12px; }
.result-scroll { max-height: 320px; overflow: auto; border-radius: 12px; border: 1px solid rgba(148, 163, 184, 0.12); }
.jobs-scroll { max-height: 560px; }
.account-table { width: 100%; min-width: 860px; border-collapse: collapse; }
.account-table th, .account-table td { padding: 12px 10px; border-bottom: 1px solid rgba(148, 163, 184, 0.16); text-align: left; vertical-align: top; }
.account-table th { color: #cbd5e1; font-size: 14px; }
.account-table td:last-child { white-space: nowrap; }
@media (max-width: 640px) { .panel { border-radius: 18px; padding: 14px; } .panel-head { align-items: flex-start; flex-direction: column; } .panel-head > button { width: 100%; } .table-wrap { display: none; } }
</style>
