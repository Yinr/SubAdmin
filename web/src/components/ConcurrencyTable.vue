<script setup lang="ts">
type Row = Record<string, unknown>

const props = defineProps<{
  rows: Row[]
  nameLabel: string
  valueLabel: string
  emptyText: string
  nameOf: (row: Row) => string
  valueOf: (row: Row) => string
  percentOf: (row: Row) => number
  titleOf: (row: Row) => string
  keyOf: (row: Row) => string
}>()
</script>

<template>
  <div class="stats-table-wrap full-scroll concurrency-scroll">
    <table class="mini-table concurrency-table">
      <colgroup><col class="concurrency-account-col" /><col class="concurrency-value-col" /></colgroup>
      <thead><tr><th>{{ props.nameLabel }}</th><th :title="`${props.nameLabel} / ${props.valueLabel}`">{{ props.valueLabel }}</th></tr></thead>
      <tbody>
        <tr v-for="row in props.rows" :key="props.keyOf(row)">
          <td class="concurrency-name" :title="props.nameOf(row)">{{ props.nameOf(row) }}</td>
          <td class="concurrency-values" :title="props.titleOf(row)">
            <div class="usage-bar-shell"><div class="usage-bar" :style="{ width: `${props.percentOf(row)}%` }"></div></div>
            <span>{{ props.valueOf(row) }}</span>
          </td>
        </tr>
        <tr v-if="!props.rows.length"><td colspan="2" class="muted">{{ props.emptyText }}</td></tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.stats-table-wrap { overflow: auto; border-radius: 12px; border: 1px solid rgba(148, 163, 184, 0.12); }
.full-scroll { overflow: auto; max-width: 100%; }
.full-scroll::-webkit-scrollbar { height: 10px; width: 10px; }
.full-scroll::-webkit-scrollbar-track { background: rgba(15, 23, 42, 0.45); border-radius: 999px; }
.full-scroll::-webkit-scrollbar-thumb { background: rgba(148, 163, 184, 0.34); border: 2px solid transparent; background-clip: padding-box; border-radius: 999px; }
.full-scroll::-webkit-scrollbar-thumb:hover { background: rgba(148, 163, 184, 0.5); border: 2px solid transparent; background-clip: padding-box; }
.mini-table { width: 100%; border-collapse: collapse; min-width: 560px; }
.mini-table th, .mini-table td { padding: 10px 12px; border-bottom: 1px solid rgba(148, 163, 184, 0.1); text-align: left; font-size: 13px; }
.mini-table th { color: #c4b5fd; background: rgba(2, 6, 23, 0.28); }
.mini-table td { color: #e2e8f0; }
.muted { color: #94a3b8; }
.usage-bar-shell { height: 8px; overflow: hidden; border-radius: 999px; background: rgba(148, 163, 184, 0.18); }
.usage-bar { height: 100%; border-radius: inherit; background: linear-gradient(90deg, #22c55e, #38bdf8, #8b5cf6); }
.concurrency-scroll { max-height: 360px; overflow: auto; }
.concurrency-scroll .mini-table { min-width: 320px; table-layout: fixed; }
.concurrency-account-col { width: auto; }
.concurrency-value-col { width: 112px; }
.concurrency-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.concurrency-values { display: grid; gap: 6px; white-space: nowrap; }
.concurrency-values span { color: #cbd5e1; font-size: 12px; }
</style>
