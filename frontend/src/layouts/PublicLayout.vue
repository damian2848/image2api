<script setup>
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { auth, isAuthed, isAdmin, openLogin } from '../auth'
import { isDark, toggleTheme } from '../theme'
import { site } from '../site'
import Icon from '../components/Icon.vue'
import Logo from '../components/Logo.vue'
import { pointsLabel } from '../credits'
import { draft } from '../playground'
import { announcement, openAnnouncement } from '../announcement'

const route = useRoute()
const SIDEBAR_KEY = 'image2api:public-sidebar-expanded'
const storedSidebar = localStorage.getItem(SIDEBAR_KEY)
const sidebarExpanded = ref(storedSidebar === null
  ? !window.matchMedia('(max-width: 767px)').matches
  : storedSidebar === 'true')

function setSidebarExpanded(value) {
  sidebarExpanded.value = value
  localStorage.setItem(SIDEBAR_KEY, String(value))
}

function toggleSidebar() {
  setSidebarExpanded(!sidebarExpanded.value)
}

function closeSidebarAfterNavigation() {
  if (window.matchMedia('(max-width: 767px)').matches && sidebarExpanded.value) {
    setSidebarExpanded(false)
  }
}

// 画图/记录 only show once signed in; clicking 设置 while logged out opens login.
const nav = computed(() => {
  const items = [{ to: '/', label: '首页', icon: 'overview' }]
  if (isAuthed()) {
    items.push({ to: '/user', label: '创作工作台', icon: 'spark' })
    items.push({ to: '/logs', label: '我的作品', icon: 'files' })
    items.push({ to: '/mylogs', label: '调用日志', icon: 'log' })
    items.push({ to: '/invite', label: '邀请奖励', icon: 'accounts' })
    items.push({ to: '/orders', label: '充值订单', icon: 'receipt' })
  }
  items.push({ to: '/docs', label: '开发文档', icon: 'book' })
  items.push({ to: '/about', label: '服务信息', icon: 'info' })
  return items
})

function onSettings(e) {
  if (!isAuthed()) { e.preventDefault(); openLogin('/settings') }
}

const credits = computed(() => Number(auth.user?.credits || 0))
const creditsLabel = computed(() => pointsLabel(credits.value))
const userName = computed(() => auth.user?.name || auth.user?.email?.split('@')[0] || '账户')

// On the 画图 workbench the header label tracks the active mode.
const currentLabel = computed(() => {
  if (route.path === '/user') return draft.mode === 'video' ? '视频创作' : '图像创作'
  return route.meta?.label || '首页'
})

const currentHint = computed(() => {
  if (route.path === '/') return '工作台概览'
  if (route.path === '/user') return '创建与管理生成任务'
  return '账户与服务管理'
})
</script>

<template>
  <div class="console-shell theme-x min-h-screen bg-[var(--app-bg)] text-[color:var(--fg-2)]">
    <aside class="console-sidebar" :class="{ 'is-expanded': sidebarExpanded }">
      <router-link to="/" class="console-brand" @click="closeSidebarAfterNavigation">
        <img v-if="site.logo" :src="site.logo" :alt="site.title" class="w-9 h-9 rounded-lg object-contain ring-1 ring-[color:var(--hairline)]" />
        <Logo v-else :size="36" class="rounded-lg ring-1 ring-[color:var(--hairline)]" />
        <span class="sidebar-copy brand-copy">
          <strong>{{ site.title }}</strong>
          <small>AI Studio</small>
        </span>
      </router-link>

      <nav class="console-nav" aria-label="主导航">
        <div class="sidebar-section-title"><span class="sidebar-copy">工作台</span></div>
        <router-link
          v-for="n in nav" :key="n.to" :to="n.to"
          :exact-active-class="n.to === '/' ? 'active' : ''"
          :active-class="n.to === '/' ? '' : 'active'"
          :title="n.label"
          class="console-nav-link"
          @click="closeSidebarAfterNavigation">
          <Icon :name="n.icon" class="w-[18px] h-[18px] shrink-0" />
          <span class="sidebar-copy nav-copy">{{ n.label }}</span>
        </router-link>
      </nav>

      <div class="console-sidebar-footer">
        <router-link v-if="isAdmin()" to="/admin/overview" title="管理后台" class="console-nav-link" @click="closeSidebarAfterNavigation">
          <Icon name="shield" class="w-[18px] h-[18px] shrink-0" />
          <span class="sidebar-copy nav-copy">管理后台</span>
          <Icon name="open" class="sidebar-copy ml-auto w-3.5 h-3.5" />
        </router-link>
        <router-link v-if="isAuthed()" to="/settings" title="账户设置" class="console-nav-link" active-class="active" @click="closeSidebarAfterNavigation">
          <span class="account-avatar">{{ userName.slice(0, 1).toUpperCase() }}</span>
          <span class="sidebar-copy nav-copy min-w-0">
            <span class="block truncate">{{ userName }}</span>
            <small>账户设置</small>
          </span>
        </router-link>
        <button v-if="isAuthed() && announcement.content.trim()" type="button" class="console-nav-link" title="公告" @click="openAnnouncement">
          <Icon name="info" class="w-[18px] h-[18px] shrink-0" />
          <span class="sidebar-copy nav-copy">系统公告</span>
        </button>
        <button type="button" class="console-nav-link" :title="isDark ? '切换到亮色' : '切换到暗色'" @click="toggleTheme">
          <svg v-if="isDark" class="w-[18px] h-[18px] shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>
          <svg v-else class="w-[18px] h-[18px] shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>
          <span class="sidebar-copy nav-copy">{{ isDark ? '亮色模式' : '暗色模式' }}</span>
        </button>
        <button type="button" class="console-nav-link sidebar-collapse" :title="sidebarExpanded ? '收起侧边栏' : '展开侧边栏'" :aria-label="sidebarExpanded ? '收起侧边栏' : '展开侧边栏'" :aria-expanded="sidebarExpanded" @click="toggleSidebar">
          <Icon :name="sidebarExpanded ? 'panel-left-close' : 'panel-left-open'" class="w-[18px] h-[18px] shrink-0" />
          <span class="sidebar-copy nav-copy">{{ sidebarExpanded ? '收起侧边栏' : '展开侧边栏' }}</span>
        </button>
      </div>
    </aside>

    <div v-if="sidebarExpanded" class="console-mobile-backdrop" @click="setSidebarExpanded(false)"></div>

    <div class="console-main" :class="{ 'sidebar-expanded': sidebarExpanded }">
      <header class="console-header">
        <div class="console-header-title min-w-0">
          <button type="button" class="console-mobile-menu" aria-label="打开侧边栏" @click="setSidebarExpanded(true)"><Icon name="menu" class="w-5 h-5" /></button>
          <div class="min-w-0">
          <h1>{{ currentLabel }}</h1>
          <p>{{ currentHint }}</p>
          </div>
        </div>
        <div class="console-header-actions">
          <router-link to="/docs" class="header-link" title="开发文档"><Icon name="book" class="w-4 h-4" /><span>文档</span></router-link>
          <router-link v-if="isAuthed()" to="/settings" class="balance-chip">
            <span>可用积分</span><strong>{{ creditsLabel }}</strong>
          </router-link>
          <button v-else type="button" class="header-login" @click="openLogin('/user')">登录</button>
        </div>
      </header>

      <main :class="['console-content', { 'public-dark': isDark }]">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.console-sidebar {
  position: fixed;
  inset: 0 auto 0 0;
  z-index: 40;
  display: flex;
  width: 4.5rem;
  flex-direction: column;
  overflow: hidden;
  border-right: 1px solid var(--hairline);
  background: var(--surface);
  transition: width 0.2s ease, box-shadow 0.2s ease;
}
.console-sidebar.is-expanded { width: 16rem; box-shadow: 10px 0 24px rgb(15 23 42 / 0.05); }
.console-brand {
  display: flex;
  height: 4.5rem;
  align-items: center;
  gap: 0.75rem;
  padding: 0 1.125rem;
  border-bottom: 1px solid var(--hairline);
  color: var(--fg);
}
.sidebar-copy {
  min-width: 0;
  max-width: 0;
  overflow: hidden;
  opacity: 0;
  transform: translateX(-0.25rem);
  transition: max-width 0.2s ease, opacity 0.14s ease, transform 0.2s ease;
  white-space: nowrap;
}
.is-expanded .sidebar-copy { max-width: 11rem; opacity: 1; transform: translateX(0); }
.brand-copy { display: grid; gap: 0.1rem; line-height: 1.1; }
.brand-copy strong { font-size: 0.9375rem; font-weight: 650; }
.brand-copy small, .nav-copy small { color: var(--fg-3); font-size: 0.6875rem; font-weight: 500; }
.console-nav { flex: 1; overflow-y: auto; padding: 0.875rem 0.75rem; }
.sidebar-section-title { height: 1.75rem; padding: 0 0.75rem; color: var(--fg-3); font-size: 0.6875rem; font-weight: 600; line-height: 1.75rem; letter-spacing: 0.04em; }
.console-nav-link {
  display: flex;
  width: 100%;
  height: 2.5rem;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.1875rem;
  padding: 0 0.75rem;
  border-radius: 0.5rem;
  color: var(--fg-2);
  font-size: 0.8125rem;
  font-weight: 500;
  text-align: left;
  transition: background 0.15s ease, color 0.15s ease;
}
.console-nav-link:hover { background: var(--hover); color: var(--fg); }
.console-nav-link.active { background: rgb(37 99 235 / 0.1); color: rgb(37 99 235); }
.console-sidebar:not(.is-expanded) .console-nav-link { justify-content: center; padding: 0; }
.console-sidebar-footer { padding: 0.75rem; border-top: 1px solid var(--hairline); }
.account-avatar { display: grid; width: 1.625rem; height: 1.625rem; flex: none; place-items: center; border-radius: 0.375rem; background: rgb(37 99 235); color: #fff; font-size: 0.6875rem; font-weight: 700; }
.sidebar-collapse { margin-top: 0.375rem; border-top: 1px solid var(--hairline); border-radius: 0; padding-top: 0.875rem; }
.console-main { min-height: 100vh; margin-left: 4.5rem; transition: margin-left 0.2s ease; }
.console-main.sidebar-expanded { margin-left: 16rem; }
.console-header { position: sticky; top: 0; z-index: 30; display: flex; min-height: 4.5rem; align-items: center; justify-content: space-between; gap: 1rem; padding: 0.75rem 2rem; border-bottom: 1px solid var(--hairline); background: var(--surface); }
.console-header h1 { color: var(--fg); font-size: 1rem; font-weight: 650; line-height: 1.4; }
.console-header p { margin-top: 0.1rem; color: var(--fg-3); font-size: 0.75rem; }
.console-header-title { display: flex; align-items: center; gap: 0.75rem; }
.console-mobile-menu { display: none; width: 2rem; height: 2rem; place-items: center; border: 1px solid var(--hairline); border-radius: 0.375rem; color: var(--fg-2); }
.console-header-actions { display: flex; align-items: center; gap: 0.625rem; }
.header-link, .header-login { display: inline-flex; height: 2rem; align-items: center; gap: 0.375rem; padding: 0 0.625rem; border-radius: 0.375rem; color: var(--fg-2); font-size: 0.75rem; font-weight: 500; }
.header-link:hover { background: var(--hover); color: var(--fg); }
.header-login { background: rgb(37 99 235); color: #fff; }
.header-login:hover { background: rgb(29 78 216); }
.balance-chip { display: flex; align-items: baseline; gap: 0.5rem; padding: 0.4rem 0.625rem; border: 1px solid rgb(37 99 235 / 0.16); border-radius: 0.375rem; background: rgb(37 99 235 / 0.06); color: rgb(30 64 175); font-size: 0.6875rem; }
.balance-chip strong { font-size: 0.75rem; font-weight: 650; }
.console-content { min-height: calc(100vh - 4.5rem); padding: 1.5rem 2rem 3rem; }
.console-mobile-backdrop { display: none; }
.fade-enter-active, .fade-leave-active { transition: opacity 0.16s ease, transform 0.16s ease; }
.fade-enter-from { opacity: 0; transform: translateY(4px); }
.fade-leave-to { opacity: 0; }

@media (max-width: 767px) {
  .console-sidebar { transform: translateX(-100%); transition: transform 0.2s ease, width 0.2s ease; }
  .console-sidebar.is-expanded { transform: translateX(0); width: min(16rem, calc(100vw - 3rem)); box-shadow: 12px 0 30px rgb(15 23 42 / 0.16); }
  .console-mobile-backdrop { position: fixed; inset: 0; z-index: 35; display: block; background: rgb(15 23 42 / 0.35); }
  .console-main, .console-main.sidebar-expanded { margin-left: 0; }
  .console-header { padding: 0.75rem 1rem; }
  .console-mobile-menu { display: grid; }
  .console-content { padding: 1rem 1rem 2rem; }
  .header-link span { display: none; }
  .balance-chip { display: none; }
}
</style>
