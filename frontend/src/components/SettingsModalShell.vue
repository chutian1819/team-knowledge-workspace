<template>
  <Teleport to="body">
    <Transition name="settings-modal-shell">
      <div v-if="visible" class="settings-modal-shell settings-overlay" :class="overlayClass" :style="{ zIndex }"
        @click.self="emit('close')">
        <div ref="dialogRef" class="settings-modal" role="dialog" aria-modal="true" :aria-labelledby="titleId"
          @keydown.tab="trapTab" @focusin="rememberFocus">
          <div v-if="loading" class="editor-initializing" role="status" :aria-label="$t('common.loading')">
            <t-loading size="medium" :text="$t('common.loading')" />
          </div>

          <button ref="closeRef" class="close-btn" type="button" @click="emit('close')" :aria-label="$t('common.close')">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M15 5L5 15M5 5L15 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>

          <div class="settings-container">
            <aside class="settings-sidebar" :class="{ 'is-mobile-expanded': mobileNavOpen }">
              <div class="sidebar-header">
                <h2 :id="titleId" class="sidebar-title">{{ title }}</h2>
                <button ref="mobileNavRef" type="button" class="mobile-nav-toggle" :aria-expanded="mobileNavOpen" :aria-controls="navId"
                  :aria-label="$t(mobileNavOpen ? 'menu.collapseSidebar' : 'menu.expandSidebar')"
                  @click="mobileNavOpen = !mobileNavOpen">
                  <t-icon :name="mobileNavOpen ? 'chevron-up' : 'chevron-down'" />
                </button>
                <slot name="sidebar-header-extra" />
              </div>
              <nav :id="navId" class="settings-nav" :aria-label="title" :data-guide="navGuide || undefined" @click.capture="handleNavSelection">
                <slot name="nav">
                  <template v-for="group in navGroups" :key="group.key">
                    <div class="nav-group-title">{{ group.label }}</div>
                    <button v-for="item in group.items" :key="item.key" type="button" :class="['nav-item', { active: modelValue === item.key }]"
                      :aria-current="modelValue === item.key ? 'page' : undefined"
                      :data-guide="navItemGuidePrefix ? `${navItemGuidePrefix}-${item.key}` : undefined"
                      @click="emit('update:modelValue', item.key)">
                      <slot name="nav-icon" :item="item" :active="modelValue === item.key">
                        <t-icon :name="item.icon" class="nav-icon" />
                      </slot>
                      <span class="nav-label">{{ item.label }}</span>
                      <span v-if="showBadge(item)" :class="['nav-badge', item.badgeClass]">{{ item.badge }}</span>
                    </button>
                  </template>
                </slot>
              </nav>
            </aside>

            <div class="settings-content">
              <div ref="bodyRef" class="settings-body" tabindex="-1">
                <slot />
              </div>
              <div v-if="$slots.footer || $slots['footer-note']" class="settings-footer">
                <slot name="footer-note" />
                <div class="settings-footer-actions">
                  <slot name="footer" />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
/**
 * 全屏"设置类"弹窗壳：遮罩 + 1080×780 面板 + 左侧分组导航 + 右侧内容 + 可选底栏。
 * 被 Settings / AgentEditorModal / OrganizationSettingsModal / KnowledgeBaseEditorModal 共用，
 * 关闭行为（Esc / 遮罩 / 未保存守卫）由消费方通过 useModalShell 决定，本组件只发出 `close`。
 *
 * 样式为非 scoped、以 `.settings-modal-shell` 为根前缀，这样通过 `nav` 插槽自定义导航的消费方
 * 也能复用 .nav-item / .nav-icon 等基础样式。
 */
export interface SettingsModalNavItem {
  key: string
  label: string
  icon?: string
  badge?: number | string | null
  /** 额外的徽标 class，例如 'nav-badge-count' */
  badgeClass?: string
  /** 徽标为 0 时是否仍显示 */
  showZeroBadge?: boolean
  [extra: string]: unknown
}

export interface SettingsModalNavGroup {
  key: string
  label: string
  items: SettingsModalNavItem[]
}

const props = withDefaults(
  defineProps<{
    visible: boolean
    title: string
    /** 当前分区 key（v-model） */
    modelValue?: string
    navGroups?: SettingsModalNavGroup[]
    loading?: boolean
    zIndex?: number
    overlayClass?: string
    navGuide?: string
    navItemGuidePrefix?: string
  }>(),
  {
    modelValue: '',
    navGroups: () => [],
    loading: false,
    zIndex: 1100,
    overlayClass: '',
    navGuide: '',
    navItemGuidePrefix: '',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', key: string): void
  (e: 'close'): void
}>()

const titleId = `settings-title-${useId()}`
const navId = `settings-nav-${useId()}`
const dialogRef = ref<HTMLElement>()
const closeRef = ref<HTMLButtonElement>()
const bodyRef = ref<HTMLElement>()
const mobileNavRef = ref<HTMLButtonElement>()
const mobileNavOpen = ref(false)
let returnFocus: HTMLElement | null = null
let lastDialogFocus: HTMLElement | null = null

function rememberFocus(event: FocusEvent) {
  if (event.target instanceof HTMLElement) lastDialogFocus = event.target
}

async function restoreHiddenNavFocus() {
  await nextTick()
  if (!props.visible || !lastDialogFocus || lastDialogFocus.getClientRects().length > 0) return
  if (document.activeElement !== document.body && document.activeElement !== lastDialogFocus) return
  const target = mobileNavRef.value?.getClientRects().length ? mobileNavRef.value : closeRef.value
  target?.focus({ preventScroll: true })
}

onMounted(() => window.addEventListener('resize', restoreHiddenNavFocus, { passive: true }))

function restoreFocus() {
  if (returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
  returnFocus = null
}

watch(() => props.visible, async (visible) => {
  if (visible) {
    returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    mobileNavOpen.value = false
    await nextTick()
    if (props.visible) closeRef.value?.focus({ preventScroll: true })
  } else {
    lastDialogFocus = null
    restoreFocus()
  }
}, { immediate: true })

onBeforeUnmount(() => {
  window.removeEventListener('resize', restoreHiddenNavFocus)
  restoreFocus()
})

function trapTab(event: KeyboardEvent) {
  const focusable = Array.from(dialogRef.value?.querySelectorAll<HTMLElement>(
    'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]',
  ) || []).filter(el => el.getClientRects().length > 0 && !el.closest('[inert], [aria-hidden="true"]'))
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (!first || !last) return
  if (event.shiftKey && (document.activeElement === first || document.activeElement === bodyRef.value)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

async function handleNavSelection(event: MouseEvent) {
  if (!mobileNavOpen.value || !(event.target instanceof Element)) return
  const item = event.target.closest('.nav-item, .submenu-item')
  if (!item || item.classList.contains('has-submenu')) return
  mobileNavOpen.value = false
  await nextTick()
  bodyRef.value?.focus({ preventScroll: true })
}

function showBadge(item: SettingsModalNavItem): boolean {
  if (item.badge == null || item.badge === '') return false
  if (typeof item.badge === 'number' && item.badge <= 0) return !!item.showZeroBadge
  return true
}
</script>

<style lang="less">
.settings-modal-shell.settings-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 18, 0.42);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  backdrop-filter: blur(4px);
  overscroll-behavior: none;
}

.settings-modal-shell {
  .settings-modal {
    position: relative;
    width: 100%;
    // 1080×780：给成员表 / 系统设置这类多列内容留足空间；外层 20px padding 后 1120，
    // 1280+ 的笔记本都放得下；更窄的视口由 width: 100% 收缩。
    max-width: 1080px;
    height: 780px;
    max-height: calc(100vh - 40px);
    max-height: calc(100dvh - 40px);
    background: var(--td-bg-color-container);
    border-radius: var(--app-radius-xl);
    border: 1px solid var(--app-border-subtle);
    box-shadow: var(--app-shadow-md);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .editor-initializing {
    position: absolute;
    inset: 0;
    z-index: 20;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--td-bg-color-container);
  }

  .close-btn {
    position: absolute;
    top: 16px;
    right: 16px;
    width: 32px;
    height: 32px;
    border: none;
    background: transparent;
    border-radius: var(--app-radius-sm);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--td-text-color-secondary);
    transition: background var(--app-motion-base) ease, color var(--app-motion-base) ease;
    z-index: 10;

    &:hover {
      background: var(--td-bg-color-container-hover);
      color: var(--td-text-color-primary);
    }
  }

  .settings-container {
    display: flex;
    height: 100%;
    width: 100%;
    overflow: hidden;
  }

  .settings-sidebar {
    width: 208px;
    background-color: var(--app-surface-muted);
    border-right: 1px solid var(--td-component-stroke);
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .sidebar-header {
    padding: 24px 20px 20px;
    border-bottom: 1px solid var(--td-component-stroke);
    flex-shrink: 0;
  }

  .sidebar-title {
    margin: 0;
    font-size: var(--app-text-xl);
    font-weight: 600;
    color: var(--td-text-color-primary);
  }

  .mobile-nav-toggle { display: none; }

  .settings-nav {
    flex: 1;
    padding: 8px 8px 12px;
    overflow-y: auto;
    min-height: 0;

    &::-webkit-scrollbar {
      width: 6px;
    }

    &::-webkit-scrollbar-track {
      background: var(--td-bg-color-secondarycontainer);
    }

    &::-webkit-scrollbar-thumb {
      background: var(--td-gray-color-5);
      border-radius: var(--app-radius-pill);
    }

    &::-webkit-scrollbar-thumb:hover {
      background: var(--td-gray-color-6);
    }
  }

  .nav-group-title {
    padding: 8px 14px 2px;
    color: var(--td-text-color-placeholder);
    font-size: var(--app-text-sm);
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .settings-nav > .nav-group-title:first-child {
    padding-top: 2px;
  }

  .nav-item {
    width: 100%;
    min-height: 36px;
    border: 0;
    background: transparent;
    font-family: inherit;
    text-align: left;
    display: flex;
    align-items: center;
    padding: 6px 12px;
    margin-bottom: 2px;
    border-radius: var(--app-radius-sm);
    cursor: pointer;
    transition: background-color var(--app-motion-base) ease, color var(--app-motion-base) ease;
    font-size: var(--app-text-base);
    color: var(--td-text-color-primary);
    user-select: none;

    &:hover {
      background-color: var(--td-bg-color-container-hover);
      color: var(--td-text-color-primary);
    }

    &.active {
      background-color: var(--app-selection-bg);
      color: var(--app-selection-text);
      font-weight: 500;
    }
  }

  .nav-icon {
    margin-right: 9px;
    font-size: var(--app-text-xl);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    color: inherit;

    &.nav-icon-img {
      width: 16px;
      height: 16px;
    }

    &.nav-icon-emoji {
      font-size: var(--app-text-base);
      line-height: 1;
    }
  }

  .nav-label {
    flex: 1;
  }

  .nav-badge {
    flex-shrink: 0;
    margin-left: 2px;
    padding: 0 6px;
    border-radius: var(--app-radius-md);
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-secondary);
    font-size: var(--app-text-xs);
    line-height: 16px;
    font-weight: 500;
    text-align: center;

    &.nav-badge-count {
      min-width: 20px;
    }
  }

  .settings-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background-color: var(--td-bg-color-container);
  }

  // 既是滚动容器，也是 flex 列：消费方的 .content-wrapper 想自己滚动时设 flex:1 + overflow:auto，
  // 想让整块内容随页面滚动时保持自然高度即可。
  .settings-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow-y: auto;

    &::-webkit-scrollbar {
      width: 6px;
    }

    &::-webkit-scrollbar-track {
      background: var(--td-bg-color-container);
    }

    &::-webkit-scrollbar-thumb {
      background: var(--td-gray-color-5);
      border-radius: var(--app-radius-pill);
    }

    &::-webkit-scrollbar-thumb:hover {
      background: var(--td-gray-color-6);
    }
  }

  .settings-footer {
    padding: 12px 40px;
    border-top: 1px solid var(--td-component-stroke);
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 16px;
    flex-shrink: 0;
    background-color: var(--td-bg-color-container);
  }

  .settings-footer-note {
    margin: 0;
    margin-right: auto;
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: var(--app-text-md);
    line-height: 20px;
    color: var(--td-text-color-secondary);

    strong {
      margin-right: 4px;
      color: var(--td-text-color-primary);
      font-weight: 500;
    }

    &__icon {
      flex-shrink: 0;
      margin-top: 2px;
      font-size: var(--app-text-base);
      color: var(--td-success-color);
    }
  }

  .settings-footer-actions {
    display: flex;
    gap: 12px;
    flex-shrink: 0;
  }
}

@media (max-width: 720px) {
  .settings-modal-shell.settings-overlay {
    padding: 8px;
  }

  .settings-modal-shell {
    .settings-modal {
      height: calc(100dvh - 16px);
      max-height: calc(100dvh - 16px);
    }
    .settings-container { flex-direction: column; }
    .settings-sidebar {
      width: 100%;
      max-height: 50%;
      border-right: 0;
      border-bottom: 1px solid var(--td-component-stroke);
    }
    .sidebar-header {
      box-sizing: border-box;
      min-height: 60px;
      padding: 12px 52px 12px 16px;
      display: flex;
      align-items: center;
      gap: 8px;
      border-bottom: 0;
    }
    .sidebar-title { font-size: var(--app-text-lg); }
    .mobile-nav-toggle {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 32px;
      height: 32px;
      border: 1px solid var(--td-component-stroke);
      border-radius: var(--app-radius-sm);
      background: var(--td-bg-color-container);
      color: var(--td-text-color-secondary);
      cursor: pointer;
    }
    .settings-nav { display: none; }
    .is-mobile-expanded .settings-nav { display: block; padding-bottom: 12px; }
    .settings-nav .nav-item { min-height: 40px; }
    .settings-content .content-wrapper { padding: 24px 16px; }
    .settings-content .setting-row { flex-direction: column; gap: 12px; }
    .settings-content .setting-info,
    .settings-content .setting-control {
      flex: 0 1 auto;
      width: 100%;
      min-width: 0;
      max-width: 100%;
      padding-right: 0;
    }
    .settings-content .setting-control { justify-content: flex-start; }
    .settings-content .setting-control > * { max-width: 100%; }
    .close-btn { top: 14px; right: 12px; }
    .settings-footer {
      padding: 12px 16px;
      flex-wrap: wrap;
      gap: 12px;
    }
    .settings-footer-note { flex-basis: 100%; }
    .settings-footer-actions { margin-left: auto; gap: 8px; }
  }
}

.settings-modal-shell-leave-active {
  transition: opacity var(--app-motion-base) ease;

  .settings-modal {
    transition: transform var(--app-motion-base) ease, opacity var(--app-motion-base) ease;
  }
}

.settings-modal-shell-leave-to {
  opacity: 0;

  .settings-modal {
    transform: scale(0.95);
    opacity: 0;
  }
}
</style>
