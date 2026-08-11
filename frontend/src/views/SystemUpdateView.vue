<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api'
import Icon from '../components/Icon.vue'

const status = ref(null)
const loading = ref(true)
const starting = ref(false)
const error = ref('')
const unavailable = ref(false)
const recovering = ref(false)
let pollTimer = null
let recoveryTimer = null

const isUpdating = computed(() => status.value?.state === 'updating')
const release = computed(() => status.value?.release || null)
const stateLabel = computed(() => ({
  idle: '等待更新',
  updating: '更新中',
  succeeded: '已完成',
  failed: '更新失败',
}[status.value?.state] || '未知状态'))
const stepLabel = computed(() => ({
  ready: '已就绪',
  queued: '等待执行',
  checking_repository: '校验更新源',
  checking_worktree: '检查本地代码',
  fetching_release: '获取发布版本',
  switching_source: '切换版本',
  rebuilding_containers: '重建并重启容器',
  complete: '容器已重启',
  failed: '任务未完成',
}[status.value?.step] || status.value?.step || ''))
const statusClass = computed(() => ({
  idle: 'muted',
  updating: 'running',
  succeeded: 'done',
  failed: 'failed',
}[status.value?.state] || 'muted'))
const releaseNotes = computed(() => (release.value?.body || '').trim())

function stopPolling() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = null
}

async function load({ quiet = false } = {}) {
  if (!quiet) loading.value = true
  if (!quiet) error.value = ''
  const r = await api(quiet ? '/system/update' : '/system/update?refresh=true')
  if (r.ok) {
    unavailable.value = false
    status.value = r.data
    if (status.value?.state === 'succeeded') beginRecovery()
    if (status.value?.state === 'failed') stopPolling()
  } else if (r.status === 503) {
    unavailable.value = true
    stopPolling()
  } else if (!quiet) {
    error.value = r.data?.detail || '无法读取更新状态'
  }
  loading.value = false
}

function startPolling() {
  stopPolling()
  pollTimer = setInterval(() => load({ quiet: true }), 2000)
}

async function startUpdate() {
  if (starting.value || isUpdating.value || !status.value?.has_update) return
  const version = status.value.latest_version || '最新版本'
  if (!window.confirm(`确认更新到 ${version} 吗？更新过程会重建并重启后端和网页容器。`)) return

  starting.value = true
  error.value = ''
  const r = await api('/system/update', { method: 'POST' })
  starting.value = false
  if (!r.ok) {
    error.value = r.data?.detail || '无法启动更新'
    return
  }
  status.value = r.data
  startPolling()
}

function beginRecovery() {
  if (recovering.value) return
  recovering.value = true
  stopPolling()
  let attempts = 0
  recoveryTimer = setInterval(async () => {
    attempts += 1
    try {
      const response = await fetch('/health', { cache: 'no-store' })
      if (response.ok) {
        clearInterval(recoveryTimer)
        recoveryTimer = null
        window.location.reload()
      }
    } catch {
      // The backend and web containers are expected to be unavailable briefly.
    }
    if (attempts >= 150) {
      clearInterval(recoveryTimer)
      recoveryTimer = null
      recovering.value = false
      error.value = '容器尚未恢复，请检查宿主机更新器日志。'
    }
  }, 2000)
}

function formatDate(value) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

onMounted(load)
onBeforeUnmount(() => {
  stopPolling()
  if (recoveryTimer) clearInterval(recoveryTimer)
})
</script>

<template>
  <section class="update-page">
    <header class="update-header">
      <div>
        <div class="eyebrow">系统维护</div>
        <h2>在线更新</h2>
        <p>发布到 GitHub Release 的稳定版本会在这里显示。</p>
      </div>
      <button class="icon-button" type="button" title="检查更新" :disabled="loading || isUpdating" @click="load()">
        <Icon name="refresh" class="w-4 h-4" :class="loading && 'spin'" />
      </button>
    </header>

    <div v-if="loading" class="state-block">正在读取版本信息…</div>

    <div v-else-if="unavailable" class="state-block warning">
      <Icon name="shield" class="w-5 h-5 shrink-0" />
      <div><strong>宿主机更新器尚未配置</strong><p>完成服务器上的更新器安装并设置共享密钥后，此页面会自动启用。</p></div>
    </div>

    <template v-else-if="status">
      <div class="version-grid">
        <div class="version-block">
          <span>当前版本</span>
          <strong>{{ status.current_version || '未识别' }}</strong>
        </div>
        <div class="version-arrow" aria-hidden="true">→</div>
        <div class="version-block target">
          <span>最新稳定版</span>
          <strong>{{ status.latest_version || '未识别' }}</strong>
        </div>
        <div class="version-status" :class="statusClass">
          <span></span>{{ stateLabel }}<small v-if="stepLabel"> · {{ stepLabel }}</small>
        </div>
      </div>

      <div v-if="status.error || error" class="message error">
        <Icon name="close" class="w-4 h-4 shrink-0" />{{ status.error || error }}
      </div>
      <div v-else-if="isUpdating" class="message progress">
        <Icon name="refresh" class="w-4 h-4 shrink-0 spin" />正在{{ stepLabel }}，请保持本页打开。
      </div>
      <div v-else-if="recovering" class="message success">
        <Icon name="check" class="w-4 h-4 shrink-0" />更新已完成，正在等待服务恢复…
      </div>

      <div class="update-actions">
        <button v-if="status.has_update && !isUpdating" type="button" class="btn-update" :disabled="starting" @click="startUpdate">
          <Icon name="download" class="w-4 h-4" />{{ starting ? '正在启动…' : '更新并重启' }}
        </button>
        <span v-else-if="status.state === 'idle'" class="up-to-date"><Icon name="check" class="w-4 h-4" />已是最新版本</span>
      </div>

      <section v-if="release" class="release-section">
        <div class="release-topline">
          <div><h3>{{ release.name || release.tag_name }}</h3><p>发布于 {{ formatDate(release.published_at) }}</p></div>
          <a v-if="release.html_url" :href="release.html_url" target="_blank" rel="noopener" title="在 GitHub 查看 Release"><Icon name="open" class="w-4 h-4" /></a>
        </div>
        <pre v-if="releaseNotes">{{ releaseNotes }}</pre>
        <p v-else class="empty-notes">该版本没有发布说明。</p>
      </section>
    </template>

    <div v-else class="state-block warning">{{ error || '未获得更新状态' }}</div>
  </section>
</template>

<style scoped>
.update-page { max-width: 56rem; margin: 0 auto; }
.update-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin-bottom: 1.5rem; }
.eyebrow { color: rgb(37 99 235); font-size: 0.6875rem; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; }
.update-header h2 { margin-top: 0.35rem; color: var(--fg); font-size: 1.5rem; font-weight: 700; }
.update-header p { margin-top: 0.35rem; color: var(--fg-3); font-size: 0.8125rem; }
.icon-button { display: grid; width: 2.25rem; height: 2.25rem; place-items: center; border: 1px solid var(--hairline); border-radius: 0.4rem; color: var(--fg-2); transition: background 0.15s, color 0.15s; }.icon-button:hover:not(:disabled) { background: var(--hover); color: var(--fg); }.icon-button:disabled { opacity: 0.45; cursor: not-allowed; }
.version-grid { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 1rem; padding: 1.25rem; border: 1px solid var(--hairline); background: var(--surface); }
.version-block span { display: block; color: var(--fg-3); font-size: 0.6875rem; }.version-block strong { display: block; margin-top: 0.3rem; color: var(--fg); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 1.125rem; font-weight: 650; overflow-wrap: anywhere; }.version-block.target strong { color: rgb(37 99 235); }
.version-arrow { color: var(--fg-3); font-size: 1.25rem; }.version-status { grid-column: 1 / -1; display: flex; align-items: center; color: var(--fg-3); font-size: 0.75rem; }.version-status span { width: 0.45rem; height: 0.45rem; margin-right: 0.45rem; border-radius: 50%; background: currentColor; }.version-status small { color: inherit; font-size: inherit; }.version-status.running { color: rgb(217 119 6); }.version-status.done { color: rgb(5 150 105); }.version-status.failed { color: rgb(225 29 72); }
.message, .state-block { display: flex; align-items: flex-start; gap: 0.65rem; margin-top: 1rem; padding: 0.8rem 0.9rem; border: 1px solid var(--hairline); color: var(--fg-2); background: var(--surface); font-size: 0.8125rem; line-height: 1.5; }.state-block { margin-top: 0; }.state-block.warning { border-color: rgb(245 158 11 / 0.35); color: rgb(180 83 9); }.state-block strong { color: inherit; font-weight: 650; }.state-block p { margin-top: 0.2rem; color: var(--fg-3); }.message.error { border-color: rgb(244 63 94 / 0.35); color: rgb(225 29 72); }.message.progress { border-color: rgb(245 158 11 / 0.35); color: rgb(180 83 9); }.message.success { border-color: rgb(16 185 129 / 0.35); color: rgb(5 150 105); }
.update-actions { display: flex; align-items: center; min-height: 3.5rem; gap: 0.75rem; padding: 0.75rem 0; border-bottom: 1px solid var(--hairline); }.btn-update { display: inline-flex; align-items: center; gap: 0.45rem; min-height: 2.25rem; padding: 0 0.9rem; border-radius: 0.4rem; background: rgb(37 99 235); color: white; font-size: 0.8125rem; font-weight: 600; transition: background 0.15s, opacity 0.15s; }.btn-update:hover:not(:disabled) { background: rgb(29 78 216); }.btn-update:disabled { opacity: 0.55; cursor: not-allowed; }.up-to-date { display: inline-flex; align-items: center; gap: 0.4rem; color: rgb(5 150 105); font-size: 0.8125rem; }
.release-section { padding: 1.25rem 0 0; }.release-topline { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }.release-topline h3 { color: var(--fg); font-size: 0.9375rem; font-weight: 650; }.release-topline p, .empty-notes { margin-top: 0.25rem; color: var(--fg-3); font-size: 0.75rem; }.release-topline a { display: grid; width: 2rem; height: 2rem; place-items: center; border: 1px solid var(--hairline); border-radius: 0.4rem; color: var(--fg-2); }.release-topline a:hover { background: var(--hover); color: var(--fg); }.release-section pre { max-height: 20rem; margin-top: 0.9rem; overflow: auto; padding: 0.9rem; border: 1px solid var(--hairline); background: rgb(15 23 42 / 0.025); color: var(--fg-2); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.75rem; line-height: 1.65; white-space: pre-wrap; overflow-wrap: anywhere; }.empty-notes { padding: 0.9rem 0; }
.spin { animation: spin 0.9s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 640px) { .version-grid { grid-template-columns: 1fr; gap: 0.75rem; }.version-arrow { display: none; }.version-status { grid-column: auto; }.update-header h2 { font-size: 1.25rem; } }
</style>
