<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../components/Icon.vue'
import Logo from '../components/Logo.vue'
import { site } from '../site'
import { isDark, toggleTheme } from '../theme'

const route = useRoute()
const ADMIN_SIDEBAR_KEY = 'image2api:admin-sidebar-expanded'
const storedSidebar = localStorage.getItem(ADMIN_SIDEBAR_KEY)
const sidebarExpanded = ref(storedSidebar === null
  ? !window.matchMedia('(max-width: 767px)').matches
  : storedSidebar === 'true')

const tabs = [
  { label: '概览', to: '/admin/overview', icon: 'overview' },
  { label: '模型管理', to: '/admin/models', icon: 'models' },
  { label: '账号管理', to: '/admin/accounts', icon: 'plug' },
  { label: '用户管理', to: '/admin/users', icon: 'accounts' },
  { label: '并发分组', to: '/admin/concurrency', icon: 'shield' },
  { label: '违禁词管理', icon: 'ban', children: [
    { label: '违禁词列表', to: '/admin/banned-words' },
    { label: '命中记录', to: '/admin/banned-word-hits' },
  ] },
  { label: '订单管理', to: '/admin/orders', icon: 'receipt' },
  { label: '兑换码管理', to: '/admin/cdks', icon: 'spark' },
  { label: '邀请日志', to: '/admin/invites', icon: 'accounts' },
  { label: '作品管理', to: '/admin/images', icon: 'files' },
  { label: '首页内容', to: '/admin/showcase', icon: 'spark' },
  { label: '日志管理', to: '/admin/logs', icon: 'log' },
  { label: '系统配置', to: '/admin/config', icon: 'config' },
  { label: '在线更新', to: '/admin/update', icon: 'download' },
]

const currentLabel = computed(() => route.meta?.label || '概览')
const openGroups = ref(new Set())

function setSidebarExpanded(value) {
  sidebarExpanded.value = value
  localStorage.setItem(ADMIN_SIDEBAR_KEY, String(value))
}

function toggleSidebar() {
  setSidebarExpanded(!sidebarExpanded.value)
}

function closeSidebarAfterNavigation() {
  if (window.matchMedia('(max-width: 767px)').matches && sidebarExpanded.value) {
    setSidebarExpanded(false)
  }
}

function groupActive(t) { return (t.children || []).some((c) => route.path.startsWith(c.to)) }
function toggleGroup(label) {
  if (!sidebarExpanded.value) setSidebarExpanded(true)
  const next = new Set(openGroups.value)
  next.has(label) ? next.delete(label) : next.add(label)
  openGroups.value = next
}

watch(() => route.path, () => {
  for (const t of tabs) {
    if (t.children && groupActive(t) && !openGroups.value.has(t.label)) {
      const next = new Set(openGroups.value)
      next.add(t.label)
      openGroups.value = next
    }
  }
}, { immediate: true })
</script>

<template>
  <div class="admin-shell theme-x h-screen bg-[var(--app-bg)] text-[color:var(--fg-2)] overflow-hidden">
    <aside class="admin-sidebar" :class="{ 'is-expanded': sidebarExpanded }">
      <router-link to="/" class="admin-brand" @click="closeSidebarAfterNavigation">
        <img v-if="site.logo" :src="site.logo" :alt="site.title" class="w-9 h-9 rounded-lg object-contain ring-1 ring-[color:var(--hairline)]" />
        <Logo v-else :size="36" class="rounded-lg ring-1 ring-[color:var(--hairline)]" />
        <span class="admin-copy admin-brand-copy"><strong>{{ site.title }}</strong><small>管理后台</small></span>
      </router-link>

      <nav class="admin-navigation" aria-label="管理导航">
        <div class="admin-section-title"><span class="admin-copy">管理中心</span></div>
        <template v-for="t in tabs" :key="t.label">
          <router-link v-if="!t.children" :to="t.to" class="admin-link" active-class="active" :title="t.label" @click="closeSidebarAfterNavigation">
            <Icon :name="t.icon" class="w-[18px] h-[18px] shrink-0" />
            <span class="admin-copy admin-nav-copy">{{ t.label }}</span>
          </router-link>
          <div v-else>
            <button type="button" class="admin-link" :class="{ active: groupActive(t) && !openGroups.has(t.label) }" :title="t.label" @click="toggleGroup(t.label)">
              <Icon :name="t.icon" class="w-[18px] h-[18px] shrink-0" />
              <span class="admin-copy admin-nav-copy">{{ t.label }}</span>
              <svg class="admin-copy admin-chevron ml-auto w-3.5 h-3.5" :class="openGroups.has(t.label) && 'rotate-90'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18l6-6-6-6"/></svg>
            </button>
            <div v-if="sidebarExpanded && openGroups.has(t.label)" class="admin-submenu">
              <router-link v-for="c in t.children" :key="c.to" :to="c.to" class="admin-sublink" active-class="active" @click="closeSidebarAfterNavigation">{{ c.label }}</router-link>
            </div>
          </div>
        </template>
      </nav>

      <div class="admin-sidebar-footer">
        <router-link to="/user" class="admin-link" title="返回用户端" @click="closeSidebarAfterNavigation">
          <Icon name="spark" class="w-[18px] h-[18px] shrink-0" />
          <span class="admin-copy admin-nav-copy">返回用户端</span>
          <Icon name="open" class="admin-copy ml-auto w-3.5 h-3.5" />
        </router-link>
        <button type="button" class="admin-link" :title="isDark ? '切换到亮色' : '切换到暗色'" @click="toggleTheme">
          <svg v-if="isDark" class="w-[18px] h-[18px] shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>
          <svg v-else class="w-[18px] h-[18px] shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>
          <span class="admin-copy admin-nav-copy">{{ isDark ? '亮色模式' : '暗色模式' }}</span>
        </button>
        <button type="button" class="admin-link admin-collapse" :title="sidebarExpanded ? '收起侧边栏' : '展开侧边栏'" :aria-label="sidebarExpanded ? '收起侧边栏' : '展开侧边栏'" :aria-expanded="sidebarExpanded" @click="toggleSidebar">
          <Icon :name="sidebarExpanded ? 'panel-left-close' : 'panel-left-open'" class="w-[18px] h-[18px] shrink-0" />
          <span class="admin-copy admin-nav-copy">{{ sidebarExpanded ? '收起侧边栏' : '展开侧边栏' }}</span>
        </button>
      </div>
    </aside>

    <div v-if="sidebarExpanded" class="admin-mobile-backdrop" @click="setSidebarExpanded(false)"></div>

    <div class="admin-main" :class="{ 'sidebar-expanded': sidebarExpanded }">
      <header class="admin-header">
        <div class="admin-header-title min-w-0"><button type="button" class="admin-mobile-menu" aria-label="打开侧边栏" @click="setSidebarExpanded(true)"><Icon name="menu" class="w-5 h-5" /></button><div class="min-w-0"><h1>{{ currentLabel }}</h1><p>服务运营与资源配置</p></div></div>
        <div class="admin-header-status"><span></span>系统运行中</div>
      </header>
      <main :class="['theme-text admin-content', { 'public-dark': isDark }]">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in"><component :is="Component" /></transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.admin-sidebar { position: fixed; inset: 0 auto 0 0; z-index: 40; display: flex; width: 4.5rem; flex-direction: column; overflow: hidden; border-right: 1px solid var(--hairline); background: var(--surface); transition: width 0.2s ease, box-shadow 0.2s ease; }
.admin-sidebar.is-expanded { width: 16rem; box-shadow: 10px 0 24px rgb(15 23 42 / 0.05); }
.admin-brand { display: flex; height: 4.5rem; align-items: center; gap: 0.75rem; padding: 0 1.125rem; border-bottom: 1px solid var(--hairline); color: var(--fg); }
.admin-copy { min-width: 0; max-width: 0; overflow: hidden; opacity: 0; transform: translateX(-0.25rem); transition: max-width 0.2s ease, opacity 0.14s ease, transform 0.2s ease; white-space: nowrap; }
.is-expanded .admin-copy { max-width: 11rem; opacity: 1; transform: translateX(0); }
.admin-brand-copy { display: grid; gap: 0.1rem; line-height: 1.1; }.admin-brand-copy strong { font-size: 0.9375rem; font-weight: 650; }.admin-brand-copy small { color: var(--fg-3); font-size: 0.6875rem; font-weight: 500; }
.admin-navigation { flex: 1; overflow-y: auto; padding: 0.875rem 0.75rem; }.admin-section-title { height: 1.75rem; padding: 0 0.75rem; color: var(--fg-3); font-size: 0.6875rem; font-weight: 600; line-height: 1.75rem; letter-spacing: 0.04em; }
.admin-link { display: flex; width: 100%; height: 2.5rem; align-items: center; gap: 0.75rem; margin-bottom: 0.1875rem; padding: 0 0.75rem; border-radius: 0.5rem; color: var(--fg-2); font-size: 0.8125rem; font-weight: 500; text-align: left; transition: background 0.15s ease, color 0.15s ease; }.admin-link:hover { background: var(--hover); color: var(--fg); }.admin-link.active { background: rgb(37 99 235 / 0.1); color: rgb(37 99 235); }.admin-sidebar:not(.is-expanded) .admin-link { justify-content: center; padding: 0; }.admin-chevron { transition: transform 0.15s ease; }
.admin-submenu { margin: 0.125rem 0 0.25rem 1.3rem; padding-left: 0.65rem; border-left: 1px solid var(--hairline); }.admin-sublink { display: flex; align-items: center; min-height: 2rem; padding: 0 0.5rem; border-radius: 0.375rem; color: var(--fg-3); font-size: 0.75rem; font-weight: 500; }.admin-sublink:hover { background: var(--hover); color: var(--fg); }.admin-sublink.active { color: rgb(37 99 235); }
.admin-sidebar-footer { padding: 0.75rem; border-top: 1px solid var(--hairline); }.admin-collapse { margin-top: 0.375rem; border-top: 1px solid var(--hairline); border-radius: 0; padding-top: 0.875rem; }
.admin-main { display: flex; height: 100vh; margin-left: 4.5rem; flex-direction: column; transition: margin-left 0.2s ease; }.admin-main.sidebar-expanded { margin-left: 16rem; }.admin-header { display: flex; min-height: 4.5rem; align-items: center; justify-content: space-between; gap: 1rem; padding: 0.75rem 2rem; border-bottom: 1px solid var(--hairline); background: var(--surface); }.admin-header-title { display: flex; align-items: center; gap: 0.75rem; }.admin-mobile-menu { display: none; width: 2rem; height: 2rem; place-items: center; border: 1px solid var(--hairline); border-radius: 0.375rem; color: var(--fg-2); }.admin-header h1 { color: var(--fg); font-size: 1rem; font-weight: 650; line-height: 1.4; }.admin-header p { margin-top: 0.1rem; color: var(--fg-3); font-size: 0.75rem; }.admin-header-status { display: flex; align-items: center; gap: 0.4rem; color: var(--fg-3); font-size: 0.75rem; }.admin-header-status span { width: 0.45rem; height: 0.45rem; border-radius: 50%; background: rgb(16 185 129); }.admin-content { flex: 1; overflow-y: auto; overscroll-behavior: none; padding: 1.5rem 2rem 3rem; }
.admin-mobile-backdrop { display: none; }.fade-enter-active, .fade-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }.fade-enter-from { opacity: 0; transform: translateY(4px); }.fade-leave-to { opacity: 0; }
@media (max-width: 767px) { .admin-sidebar { transform: translateX(-100%); transition: transform 0.2s ease, width 0.2s ease; }.admin-sidebar.is-expanded { width: min(16rem, calc(100vw - 3rem)); transform: translateX(0); box-shadow: 12px 0 30px rgb(15 23 42 / 0.16); }.admin-mobile-backdrop { position: fixed; inset: 0; z-index: 35; display: block; background: rgb(15 23 42 / 0.35); }.admin-main, .admin-main.sidebar-expanded { margin-left: 0; }.admin-header { padding: 0.75rem 1rem; }.admin-mobile-menu { display: grid; }.admin-header-status { font-size: 0; }.admin-content { padding: 1rem 1rem 2rem; } }
</style>
