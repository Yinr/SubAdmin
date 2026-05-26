export const defaultImportModels = ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-image-2']

export type ImportFormState = {
  text: string
  filename: string
  groups: string[]
  proxyId: string
  priority: string
  concurrency: string
  namePrefix: string
  models: string[]
}

export function createDefaultImportForm(): ImportFormState {
  return {
    text: '',
    filename: '',
    groups: [],
    proxyId: '',
    priority: '',
    concurrency: '',
    namePrefix: '',
    models: [...defaultImportModels],
  }
}

export function buildImportModelOptions(customModels: string[]) {
  return Array.from(new Set([...defaultImportModels, ...customModels]))
}

export function splitImportModelTags(input: string) {
  return input.split(/[,\s]+/).map((item) => item.trim()).filter(Boolean)
}

export function customImportModelsFromSelection(models: string[]) {
  return models.filter((model) => !defaultImportModels.includes(model))
}

export function applyTemplateToImportForm(form: ImportFormState, data: Record<string, unknown>) {
  form.priority = String(data.priority || '')
  form.concurrency = String(data.concurrency || '')
  form.namePrefix = String(data.namePrefix || '')
  form.proxyId = String(data.proxyId || data.proxy || '')
  form.groups = templateStringList(data.groupIds) || templateStringList(data.groups) || []
  if (Array.isArray(data.models)) form.models = data.models.map(String)
}

function templateStringList(value: unknown) {
  return Array.isArray(value) ? value.map(String) : null
}

function numericIDs(values: string[]) {
  return values.map(Number).filter((id) => Number.isFinite(id) && id > 0)
}

export function buildImportPreviewSettings(form: ImportFormState, groupNames: string[], proxyName: string) {
  const settings: Record<string, unknown> = {}
  ;(['priority', 'concurrency', 'namePrefix'] as const).forEach((key) => {
    const value = String(form[key] || '').trim()
    if (value) settings[key] = value
  })
  if (form.proxyId) settings.proxyId = Number(form.proxyId)
  if (form.groups.length) settings.groupIds = numericIDs(form.groups)
  if (form.groups.length) settings.groups = groupNames
  if (proxyName) settings.proxy = proxyName
  if (form.models.length) settings.models = form.models
  return settings
}

export function buildImportExecutionSettings(form: ImportFormState) {
  const settings: Record<string, unknown> = {}
  const priorityText = String(form.priority || '').trim()
  const concurrencyText = String(form.concurrency || '').trim()
  const priority = Number(priorityText)
  const concurrency = Number(concurrencyText)
  if (String(form.namePrefix || '').trim()) settings.namePrefix = String(form.namePrefix).trim()
  if (priorityText && Number.isFinite(priority)) settings.priority = priority
  if (concurrencyText && Number.isFinite(concurrency)) settings.concurrency = concurrency
  if (form.proxyId) settings.proxyId = Number(form.proxyId)
  if (form.groups.length) settings.groupIds = numericIDs(form.groups)
  if (form.models.length) settings.models = form.models
  return settings
}

export function resetImportForm(form: ImportFormState) {
  Object.assign(form, createDefaultImportForm())
}
