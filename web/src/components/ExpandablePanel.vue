<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'

const props = defineProps<{
  title: string
  panelClass?: string
}>()

const open = defineModel<boolean>({ default: false })

function close() {
  open.value = false
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') close()
}

watch(open, (value) => {
  if (value) {
    document.addEventListener('keydown', handleKeydown)
    document.body.classList.add('modal-open')
    return
  }
  document.removeEventListener('keydown', handleKeydown)
  document.body.classList.remove('modal-open')
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleKeydown)
  document.body.classList.remove('modal-open')
})
</script>

<template>
  <article class="stats-section" :class="props.panelClass">
    <div class="panel-head compact-head expandable-head">
      <div>
        <h3>{{ props.title }}</h3>
        <slot name="meta" />
      </div>
      <button class="mini-panel-button" type="button" title="放大查看" aria-label="放大查看" @click="open = true">⛶</button>
    </div>
    <div v-if="!open" class="panel-body">
      <slot />
    </div>
  </article>

  <Teleport to="body">
    <div v-if="open" class="panel-modal-backdrop" @click.self="close">
      <article class="panel-modal-card">
        <div class="panel-head compact-head expandable-head">
          <div>
            <h3>{{ props.title }}</h3>
            <slot name="meta" />
          </div>
          <button class="mini-panel-button" type="button" title="关闭" aria-label="关闭" @click="close">×</button>
        </div>
        <div class="panel-modal-body">
          <slot />
        </div>
      </article>
    </div>
  </Teleport>
</template>

<style scoped>
.stats-section { min-width: 0; padding: 14px; border: 1px solid rgba(148, 163, 184, 0.16); border-radius: 16px; background: rgba(15, 23, 42, 0.38); }
.stats-section-trend { grid-column: span 6; }
.stats-section-user-concurrency,
.stats-section-account-concurrency { grid-column: span 3; }
.stats-section-ranking { grid-column: span 5; }
.stats-section-models { grid-column: span 7; }
.panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 14px; }
.compact-head { margin-bottom: 10px; }
.compact-head h3 { margin: 0; font-size: 15px; }
.compact-head :slotted(.muted) { display: block; margin-top: 4px; font-size: 11px; line-height: 1.35; }
.expandable-head { align-items: flex-start; }
.mini-panel-button {
  flex: 0 0 auto; margin-left: auto; width: 28px; height: 28px; padding: 0; border: 1px solid rgba(148, 163, 184, 0.1);
  border-radius: 9px; color: rgba(226, 232, 240, 0.62); background: rgba(148, 163, 184, 0.06); font-size: 14px; line-height: 1;
}
.mini-panel-button:hover { color: #e2e8f0; background: rgba(148, 163, 184, 0.12); }
.panel-modal-backdrop {
  position: fixed; inset: 0; z-index: 80; display: grid; padding: 20px; overflow: auto;
  background: rgba(2, 6, 23, 0.78); backdrop-filter: blur(10px);
}
.panel-modal-card {
  width: min(1180px, 100%); min-height: min(760px, calc(100vh - 40px)); margin: auto; padding: 18px;
  border: 1px solid rgba(148, 163, 184, 0.18); border-radius: 20px;
  background: radial-gradient(circle at top left, rgba(124, 58, 237, 0.2), transparent 34%), #0f172a;
  box-shadow: 0 24px 80px rgba(0, 0, 0, 0.42);
}
.panel-modal-body { display: grid; gap: 12px; }
:global(body.modal-open) { overflow: hidden; }
@media (max-width: 680px) {
  .panel-modal-backdrop { padding: 10px; }
  .panel-modal-card { min-height: calc(100vh - 20px); padding: 14px; border-radius: 16px; }
}
@media (max-width: 900px) {
  .stats-section-trend,
  .stats-section-user-concurrency,
  .stats-section-account-concurrency,
  .stats-section-ranking,
  .stats-section-models { grid-column: auto; }
}
</style>
