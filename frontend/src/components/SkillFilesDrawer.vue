<template>
  <SettingDrawer
    v-if="visible"
    :visible="visible"
    :title="skillName"
    :description="$t('settings.sandbox.skillFilesTitle')"
    icon="folder"
    width="720px"
    :min-width="480"
    :max-width="maxWidth"
    storage-key="skill-files-drawer:width"
    :z-index="2600"
    hide-footer
    class="skill-files-drawer"
    @update:visible="emit('update:visible', $event)"
  >
    <div ref="shellElement" class="skill-files-drawer__shell">
      <SkillFilesPanel
        v-if="catalogId || (configId && skillId)"
        :config-id="configId"
        :skill-id="skillId"
        :catalog-id="catalogId"
        :drawer-width="panelWidth"
      />
    </div>
  </SettingDrawer>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import SkillFilesPanel from '@/components/SkillFilesPanel.vue'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'

defineProps<{
  visible: boolean
  configId?: string
  skillId?: string
  catalogId?: string
  skillName: string
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const viewportWidth = ref(typeof window === 'undefined' ? 1200 : window.innerWidth)
const maxWidth = computed(() => viewportWidth.value <= 720
  ? viewportWidth.value
  : Math.min(1200, Math.floor(viewportWidth.value * 0.92)))
const shellElement = ref<HTMLElement | null>(null)
const measuredWidth = ref(0)
// The panel's vertical split handle is only useful in the two-column layout.
const panelWidth = computed(() => measuredWidth.value > 600 ? measuredWidth.value : 0)
let observer: ResizeObserver | null = null

function measurePanel() {
  measuredWidth.value = Math.round(shellElement.value?.getBoundingClientRect().width || 0)
}

watch(shellElement, (element) => {
  observer?.disconnect()
  observer = null
  measurePanel()
  if (element && typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measurePanel)
    observer.observe(element)
  }
}, { flush: 'post' })

function onWindowResize() {
  viewportWidth.value = window.innerWidth
  measurePanel()
}

onMounted(() => window.addEventListener('resize', onWindowResize, { passive: true }))
onUnmounted(() => {
  window.removeEventListener('resize', onWindowResize)
  observer?.disconnect()
})
</script>

<style lang="less" scoped>
.skill-files-drawer__shell {
  display: flex;
  flex-direction: column;
  flex: 1;
  height: 100%;
  min-height: 0;
  min-width: 0;
  width: 100%;
  overflow: hidden;
  background: var(--td-bg-color-container);
  container: skill-files / inline-size;
}

@container skill-files (max-width: 600px) {
  :deep(.skill-files-panel) { flex-direction: column; }
  :deep(.skill-files-panel__nav) {
    width: 100%;
    height: auto;
    max-height: min(30vh, 220px);
    box-sizing: border-box;
    border-right: 0;
    border-bottom: 1px solid var(--app-border-subtle);
  }
  :deep(.skill-files-panel__main) { height: auto; }
  :deep(.skill-files-panel__bar) { flex-wrap: wrap; }
}
</style>

<style lang="less">
.t-drawer.skill-files-drawer .t-drawer__content-wrapper,
.t-drawer.skill-files-drawer .t-drawer__content {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--td-bg-color-container);
}

.t-drawer.skill-files-drawer .t-drawer__body {
  padding: 0;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.t-drawer.skill-files-drawer .setting-drawer__body {
  flex: 1;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}
</style>
