<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../apiClient'
import {
  applyTemplateToImportForm,
  buildImportExecutionSettings,
  buildImportModelOptions,
  buildImportPreviewSettings,
  createDefaultImportForm,
  customImportModelsFromSelection,
  defaultImportModels,
  importItemList,
  importItemName,
  importItemStatus,
  importItemStatusClass,
  resetImportForm,
  splitImportModelTags,
} from '../importSettings'
import type { ImportPreviewItem } from '../importSettings'

type Site = { id: number; name: string; baseUrl: string }
type ImportPreview = {
  items: ImportPreviewItem[]
  warnings: string[]
  errors: string[]
  summary: Record<string, unknown>
  settings: Record<string, unknown>
}

const props = defineProps<{
  activeSiteId: number | null
  activeSite: Site | null
  groupOptions: { id: string; name: string }[]
  proxyOptions: { id: string; name: string }[]
  groupsLoading: boolean
  proxiesLoading: boolean
  loadGroups: () => void | Promise<void>
  askConfirm: (options: { title: string; message: string; detail?: string; confirmText?: string; cancelText?: string; closeOnBackdrop?: boolean }) => Promise<boolean>
  copyText: (value: unknown, label: string) => void | Promise<void>
}>()

const emit = defineEmits<{ jobCreated: [job: Record<string, unknown>] }>()

const importError = ref('')
const importPreview = ref<ImportPreview | null>(null)
const importTemplates = ref<Record<string, unknown>[]>([])
const importLoading = ref(false)
const importExecuting = ref(false)
const importTemplatesLoading = ref(false)
const importTemplateName = ref('')
const importTemplateDeleteMode = ref(false)
const newImportModelTag = ref('')
const customImportModels = ref<string[]>([])
const importPreviewPage = ref(1)
const importPreviewPageSize = 10
const importForm = reactive(createDefaultImportForm())

const importModelOptions = computed(() => buildImportModelOptions(customImportModels.value))
const selectedImportGroupNames = computed(() => importForm.groups.map((id) => props.groupOptions.find((group) => group.id === id)?.name || `分组 #${id}`))
const selectedImportProxyName = computed(() => props.proxyOptions.find((proxy) => proxy.id === importForm.proxyId)?.name || '')
const pagedImportPreviewItems = computed(() => importPreview.value?.items || [])
const importPreviewTotalPages = computed(() => Math.max(1, Math.ceil(Number(importPreview.value?.summary?.total || 0) / importPreviewPageSize)))

onMounted(loadImportTemplates)

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

async function saveImportTemplate() {
  const name = importTemplateName.value.trim()
  if (!name) {
    importError.value = '请填写模板名称'
    return
  }
  const existing = importTemplates.value.find((template) => String(template.name || '') === name)
  if (existing) {
    const ok = await props.askConfirm({
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
      body: JSON.stringify({ name, siteId: props.activeSiteId, template: importPreviewSettings() }),
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
  const ok = await props.askConfirm({ title: '删除导入模板？', message: `确定删除模板「${template.name || id}」吗？`, confirmText: '删除', closeOnBackdrop: false })
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
  applyTemplateToImportForm(importForm, data)
  customImportModels.value = customImportModelsFromSelection(importForm.models)
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
  splitImportModelTags(newImportModelTag.value).forEach((value) => {
    if (!customImportModels.value.includes(value) && !defaultImportModels.includes(value)) customImportModels.value.push(value)
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
  if (!props.activeSiteId) {
    importError.value = '请先选择站点'
    return
  }
  if (!importForm.text.trim()) {
    importError.value = '请先粘贴账号内容或选择文件'
    return
  }
  importLoading.value = true
  try {
    importPreview.value = await api<ImportPreview>(`api/sites/${props.activeSiteId}/imports/preview`, {
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
  if (!props.activeSiteId || !props.activeSite) {
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
  const site = props.activeSite
  const detail = [
    `站点：${site.name}`,
    `地址：${site.baseUrl}`,
    `导入数量：${total}`,
    `分组：${selectedImportGroupNames.value.join(' / ') || '使用上游默认分组'}`,
    `代理：${selectedImportProxyName.value || '不指定'}`,
    `模型：${importForm.models.join(' / ') || '保持原设置'}`,
    `优先级/并发：${importForm.priority || '默认'} / ${importForm.concurrency || '默认'}`,
  ].join('\n')
  const ok = await props.askConfirm({
    title: '确认导入账号？',
    message: '此操作会写入下列 sub2api 站点，请确认站点信息无误。',
    detail,
    confirmText: '确认导入',
    closeOnBackdrop: false,
  })
  if (!ok) return
  importExecuting.value = true
  try {
    const job = await api<Record<string, unknown>>(`api/sites/${props.activeSiteId}/imports/accounts`, {
      method: 'POST',
      body: JSON.stringify({
        text: importForm.text,
        filename: importForm.filename,
        settings: buildImportExecutionSettings(importForm),
        confirmation: { confirmed: true, siteId: site.id, siteName: site.name, siteBaseUrl: site.baseUrl },
      }),
    })
    emit('jobCreated', job)
  } catch (error) {
    importError.value = error instanceof Error ? error.message : '提交导入任务失败'
  } finally {
    importExecuting.value = false
  }
}

function importPreviewSettings() {
  return buildImportPreviewSettings(importForm, selectedImportGroupNames.value, selectedImportProxyName.value)
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
  resetImportForm(importForm)
  customImportModels.value = []
  importTemplateName.value = ''
  importTemplateDeleteMode.value = false
  importPreviewPage.value = 1
}

function importSummaryValue(key: string) {
  return importPreview.value?.summary?.[key] ?? 0
}
</script>

<template>
  <section class="panel content-panel">
    <div class="panel-head">
      <div>
        <h2>导入预览</h2>
        <p class="muted">先解析预览账号内容，确认站点信息后再提交导入任务；浏览器不会保存 credentials。</p>
      </div>
      <button class="secondary" type="button" @click="clearImportPreview">清空</button>
    </div>
    <p v-if="importError" class="error">{{ importError }}</p>
    <div class="import-workspace">
      <section class="section-card import-input-card">
        <div class="section-title"><div><h3>账号内容</h3><p class="muted">粘贴内容或选择文件，预览阶段不会写入上游。</p></div></div>
        <label>粘贴账号内容<textarea v-model="importForm.text" rows="12" placeholder="支持 JSON、JSON 数组、accounts 包装对象，以及常见 key=value / key: value 行格式。"></textarea></label>
        <label>选择文件<input type="file" accept=".json,.txt,.yaml,.yml,.csv" @change="handleImportFile" /></label>
      </section>
      <section class="section-card import-settings-card">
        <div class="section-title"><div><h3>导入设置</h3><p class="muted">这些设置会应用到预览和最终导入。</p></div></div>
        <div class="import-subsection import-template-section">
          <div class="subsection-head"><div><h3>导入模板</h3><p class="muted">选择已有模板快速套用；模板不保存账号凭据。</p></div></div>
          <div class="active-filter-chips template-picker">
            <button v-for="template in importTemplates" :key="String(template.id)" type="button" class="filter-chip" :class="{ danger: importTemplateDeleteMode }" @click="importTemplateDeleteMode ? deleteImportTemplate(template) : applyImportTemplate(template)">{{ importTemplateDeleteMode ? '删除 ' : '' }}{{ template.name }}</button>
            <span v-if="!importTemplates.length" class="muted">暂无模板。</span>
          </div>
          <details class="advanced-block template-manager">
            <summary>模板管理</summary>
            <div class="filter-grid template-manager-grid">
              <label>模板名称<input v-model="importTemplateName" placeholder="例如 Anthropic OAuth 默认设置" /></label>
              <button type="button" :disabled="!importTemplateName.trim()" @click="saveImportTemplate">保存当前设置</button>
              <button type="button" class="secondary" :disabled="importTemplatesLoading" @click="loadImportTemplates">{{ importTemplatesLoading ? '加载中...' : '刷新模板' }}</button>
              <button type="button" class="secondary" :class="{ danger: importTemplateDeleteMode }" @click="importTemplateDeleteMode = !importTemplateDeleteMode">{{ importTemplateDeleteMode ? '退出删除' : '删除模板' }}</button>
            </div>
          </details>
        </div>
        <div class="filter-grid import-basic-grid">
          <label>代理<select v-model="importForm.proxyId" :disabled="proxiesLoading"><option value="">不指定代理</option><option v-for="proxy in proxyOptions" :key="proxy.id" :value="proxy.id">{{ proxy.name }}</option></select></label>
          <label>命名前缀<input v-model="importForm.namePrefix" placeholder="可选，支持 {date}，如 import-{date}-" /></label>
          <label>优先级<input v-model="importForm.priority" type="number" placeholder="可选" /></label>
          <label>并发<input v-model="importForm.concurrency" type="number" placeholder="可选" /></label>
        </div>
        <div class="import-subsection">
          <div class="subsection-head"><h3>分组</h3><button type="button" class="mini inline-refresh" :disabled="groupsLoading" @click="loadGroups">{{ groupsLoading ? '刷新中...' : '刷新分组' }}</button></div>
          <div class="active-filter-chips tag-picker"><button v-for="group in groupOptions" :key="group.id" type="button" class="filter-chip" :class="{ active: importForm.groups.includes(group.id) }" @click="toggleImportGroup(group.id)">{{ group.name }}</button><span v-if="!groupOptions.length" class="muted">暂无可选分组。</span></div>
          <p class="muted">只允许选择上游已有分组；未选择时使用上游默认分组逻辑。</p>
        </div>
        <div class="import-subsection">
          <div class="subsection-head"><h3>模型</h3></div>
          <div class="active-filter-chips tag-picker"><button v-for="model in importModelOptions" :key="model" type="button" class="filter-chip" :class="{ active: importForm.models.includes(model) }" @click="toggleImportModel(model)">{{ model }}<span v-if="customImportModels.includes(model)" @click.stop="removeImportModelTag(model)">×</span></button></div>
          <div class="group-add-row compact-add-row"><input v-model="newImportModelTag" placeholder="输入新模型，多个可用逗号或空格分隔" @keyup.enter="addImportModelTag" /><button type="button" class="secondary" @click="addImportModelTag">添加模型</button></div>
        </div>
        <div class="form-actions import-actions">
          <button type="button" :disabled="importLoading || !activeSiteId" @click="previewImport(1)">{{ importLoading ? '解析中...' : '生成预览' }}</button>
          <button type="button" class="danger" :disabled="importExecuting || importLoading || !importPreview || Number(importPreview.summary?.invalid || 0) > 0" @click="executeImportAccounts">{{ importExecuting ? '提交中...' : '确认导入' }}</button>
          <span class="muted import-site-note">当前站点：{{ activeSite?.name || '未选择' }}<template v-if="importForm.filename"> · 文件：{{ importForm.filename }}</template></span>
        </div>
      </section>
    </div>
    <section v-if="importPreview" class="batch-results result-panel">
      <div class="panel-head"><div><h2>预览结果</h2><p class="muted">总数 {{ importSummaryValue('total') }} · 识别 {{ importSummaryValue('recognized') }} · 需修正 {{ importSummaryValue('invalid') }} · 疑似重复 {{ importSummaryValue('duplicates') }}</p></div><button class="secondary" type="button" @click="copyText(JSON.stringify(importPreview, null, 2), '导入预览')">复制预览 JSON</button></div>
      <div v-if="importPreview.warnings?.length" class="failure-groups"><span class="muted">警告</span><span v-for="warning in importPreview.warnings" :key="warning" class="tag tag-warning">{{ warning }}</span></div>
      <div class="result-scroll table-wrap"><table class="account-table result-table"><thead><tr><th>#</th><th>状态</th><th>账号</th><th>平台/类型</th><th>应用设置</th><th>凭据字段</th><th>缺失字段</th><th>警告</th></tr></thead><tbody><tr v-for="item in pagedImportPreviewItems" :key="String(item.index)"><td>{{ item.index }}</td><td><span :class="importItemStatusClass(item)">{{ importItemStatus(item) }}</span></td><td><div class="cell-stack"><strong class="account-name">{{ importItemName(item) }}</strong><span v-if="item.group" class="muted account-note">分组：{{ item.group }}</span></div></td><td>{{ item.platform || '未知' }} / {{ item.type || '未知' }}</td><td><div class="cell-stack compact"><span>分组：{{ importItemList(item, 'appliedGroups') }}</span><span>代理：{{ item.appliedProxy || '无' }}</span><span>模型：{{ importItemList(item, 'appliedModels') }}</span><span>优先级/并发：{{ item.appliedPriority || '默认' }} / {{ item.appliedConcurrency || '默认' }}</span></div></td><td>{{ importItemList(item, 'credentialFields') }}</td><td>{{ importItemList(item, 'missingFields') }}</td><td>{{ importItemList(item, 'warnings') }}</td></tr><tr v-if="!importPreview.items.length"><td colspan="8" class="muted">未解析到账号条目。</td></tr></tbody></table></div>
      <div class="pager" v-if="importPreviewTotalPages > 1"><button class="secondary" :disabled="importPreviewPage <= 1 || importLoading" @click="previewImport(importPreviewPage - 1)">上一页</button><span class="muted">第 {{ importPreviewPage }} / {{ importPreviewTotalPages }} 页，默认每页 {{ importPreviewPageSize }} 条</span><button class="secondary" :disabled="importPreviewPage >= importPreviewTotalPages || importLoading" @click="previewImport(importPreviewPage + 1)">下一页</button></div>
    </section>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border: 1px solid rgba(148, 163, 184, 0.18); border-radius: 22px; background: rgba(15, 23, 42, 0.88); box-shadow: 0 24px 80px rgba(0, 0, 0, 0.28); }
.content-panel { min-height: 520px; }
.panel-head, .section-title, .subsection-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 14px; }
.section-title p, .import-subsection p { margin: 4px 0 0; }
h2, h3 { margin: 0; }
h3 { font-size: 15px; }
.muted { color: #94a3b8; }
.error { color: #fecaca; }
label { display: grid; gap: 7px; color: #cbd5e1; font-size: 14px; }
input, select, textarea { width: 100%; box-sizing: border-box; padding: 12px 13px; border: 1px solid rgba(148, 163, 184, 0.28); border-radius: 12px; color: #f8fafc; background: rgba(15, 23, 42, 0.86); outline: none; }
textarea { resize: vertical; min-height: 180px; font: inherit; line-height: 1.5; }
button { border: 0; border-radius: 12px; padding: 11px 14px; color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb); cursor: pointer; font-weight: 700; }
button:disabled { opacity: 0.5; cursor: not-allowed; }
.secondary { background: rgba(148, 163, 184, 0.14); color: #e2e8f0; }
.danger { background: rgba(239, 68, 68, 0.16); color: #fecaca; }
.mini { margin-left: 8px; padding: 5px 8px; border-radius: 9px; background: rgba(148, 163, 184, 0.14); color: #e2e8f0; font-size: 12px; }
.import-workspace { display: grid; grid-template-columns: minmax(320px, 1.08fr) minmax(340px, 0.92fr); gap: 16px; align-items: start; }
.section-card { display: grid; gap: 12px; margin: 0; padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: linear-gradient(135deg, rgba(30, 41, 59, 0.68), rgba(2, 6, 23, 0.28)); }
.import-input-card textarea { min-height: 300px; }
.filter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 14px; align-items: end; }
.import-basic-grid { grid-template-columns: repeat(2, minmax(160px, 1fr)); }
.import-subsection { display: grid; gap: 10px; padding: 12px; border: 1px solid rgba(148, 163, 184, 0.12); border-radius: 14px; background: rgba(2, 6, 23, 0.18); }
.import-template-section { border-color: rgba(125, 211, 252, 0.18); background: linear-gradient(135deg, rgba(12, 74, 110, 0.18), rgba(2, 6, 23, 0.18)); }
.advanced-block { margin-top: 0; padding: 10px; border: 1px solid rgba(148, 163, 184, 0.12); border-radius: 14px; background: rgba(15, 23, 42, 0.34); }
.advanced-block summary { cursor: pointer; color: #ddd6fe; font-weight: 700; }
.template-manager-grid { grid-template-columns: minmax(180px, 1fr) repeat(3, auto); gap: 10px; }
.active-filter-chips { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin: 8px 0 12px; }
.filter-chip { padding: 6px 10px; border-radius: 999px; background: rgba(59, 130, 246, 0.14); color: #dbeafe; font-size: 12px; }
.filter-chip.active { background: linear-gradient(135deg, rgba(124, 58, 237, 0.9), rgba(37, 99, 235, 0.86)); color: #fff; }
.tag-picker .filter-chip { border: 1px solid rgba(147, 197, 253, 0.16); }
.filter-chip span { margin-left: 6px; color: #93c5fd; }
.group-add-row { display: grid; grid-template-columns: minmax(180px, 1fr) auto; gap: 10px; margin-top: 0; align-items: center; }
.form-actions { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.import-site-note { flex: 1 1 220px; }
.batch-results { display: grid; gap: 10px; margin-top: 14px; }
.result-panel { padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(2, 6, 23, 0.24); }
.failure-groups { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.result-scroll { max-height: 320px; overflow: auto; border-radius: 12px; border: 1px solid rgba(148, 163, 184, 0.12); }
.table-wrap { overflow-x: auto; margin-top: 12px; }
.account-table { width: 100%; min-width: 860px; border-collapse: collapse; }
.account-table th, .account-table td { padding: 12px 10px; border-bottom: 1px solid rgba(148, 163, 184, 0.16); text-align: left; vertical-align: top; }
.account-table th { color: #cbd5e1; font-size: 14px; }
.cell-stack { display: grid; gap: 6px; }
.cell-stack.compact { gap: 8px; }
.account-name, .account-note { display: block; max-width: 280px; overflow-wrap: anywhere; line-height: 1.35; }
.tag { display: inline-flex; align-items: center; max-width: 100%; padding: 4px 8px; border-radius: 999px; background: rgba(148, 163, 184, 0.14); color: #e2e8f0; font-size: 12px; line-height: 1.2; white-space: nowrap; }
.tag-success { background: rgba(34, 197, 94, 0.16); color: #bbf7d0; }
.tag-warning { background: rgba(245, 158, 11, 0.16); color: #fde68a; }
.pager { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-top: 12px; }
@media (max-width: 900px) { .import-workspace { grid-template-columns: 1fr; } .import-input-card textarea { min-height: 240px; } }
@media (max-width: 640px) { .panel { border-radius: 18px; padding: 14px; } .panel-head { align-items: flex-start; flex-direction: column; } .panel-head > button { width: 100%; } .filter-grid, .import-basic-grid, .template-manager-grid, .group-add-row { grid-template-columns: 1fr; } .form-actions { display: grid; grid-template-columns: 1fr; align-items: stretch; } .form-actions button { width: 100%; } .table-wrap { display: none; } }
</style>
