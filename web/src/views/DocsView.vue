<script setup lang="ts">
import { ref } from 'vue'
import { textResource } from '../apiClient'

const props = defineProps<{
  copyText: (value: unknown, label: string) => void | Promise<void>
}>()

const docsError = ref('')
const docsLoading = ref(false)
const aiReference = ref('')

async function loadDocs() {
  if (docsLoading.value) return
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

loadDocs()
</script>

<template>
  <section class="panel docs-panel">
    <div class="panel-head">
      <div>
        <h2>API 文档</h2>
        <p class="muted">这些文档通过 SubAdmin 登录态保护。不会自动注入 sub2api 管理员 Key。</p>
      </div>
      <button class="secondary" :disabled="docsLoading" @click="loadDocs">{{ docsLoading ? '加载中...' : '刷新 AI Reference' }}</button>
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
        <button class="secondary" @click="props.copyText(aiReference, 'AI Reference')">复制全文</button>
      </div>
      <pre>{{ aiReference }}</pre>
    </section>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border: 1px solid rgba(148, 163, 184, 0.18); border-radius: 22px; background: rgba(15, 23, 42, 0.88); box-shadow: 0 24px 80px rgba(0, 0, 0, 0.28); }
.panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 14px; }
h2 { margin: 0; }
.muted { color: #94a3b8; }
.error { color: #fecaca; }
button, .button-link { border: 0; border-radius: 12px; padding: 11px 14px; color: #fff; background: linear-gradient(135deg, #7c3aed, #2563eb); cursor: pointer; font-weight: 700; text-decoration: none; }
button:disabled { opacity: 0.5; cursor: not-allowed; }
.secondary { background: rgba(148, 163, 184, 0.14); color: #e2e8f0; }
.docs-panel { display: grid; gap: 14px; }
.docs-links { display: flex; flex-wrap: wrap; gap: 10px; }
.docs-reader { display: grid; gap: 12px; margin-top: 8px; }
.docs-reader pre { margin: 0; max-height: 620px; overflow: auto; padding: 16px; border-radius: 14px; background: rgba(2, 6, 23, 0.55); color: #dbeafe; line-height: 1.55; white-space: pre-wrap; }
@media (max-width: 640px) { .panel { border-radius: 18px; padding: 14px; } .panel-head { align-items: flex-start; flex-direction: column; } .panel-head > button, .docs-links { width: 100%; } .docs-links { display: grid; grid-template-columns: 1fr; } .docs-reader pre { max-height: 520px; font-size: 12px; } }
</style>
