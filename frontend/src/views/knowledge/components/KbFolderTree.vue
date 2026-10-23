<template>
  <aside :aria-label="t('knowledgeBase.folderTree.title')" class="kb-folder-tree" :class="{ 'is-collapsed': collapsed }">
    <div class="kb-folder-tree__header">
      <template v-if="!collapsed">
        <span class="kb-folder-tree__title">{{ t('knowledgeBase.folderTree.title') }}</span>
        <span v-if="tree && !loading" class="kb-folder-tree__total"
          :title="t('knowledgeBase.folderTree.totalDocuments', { count: tree.total_document_count })">
          {{ t('knowledgeBase.folderTree.totalDocuments', { count: tree.total_document_count }) }}
        </span>
        <t-tooltip :content="t('knowledgeBase.folderTree.collapse')" placement="top">
          <button
            type="button"
            class="kb-folder-tree__icon-btn"
            :aria-label="t('knowledgeBase.folderTree.collapse')"
            @click="emit('update:collapsed', true)"
          >
            <t-icon name="chevron-left-double" size="15px" />
          </button>
        </t-tooltip>
      </template>
      <t-tooltip v-else :content="t('knowledgeBase.folderTree.expand')" placement="right">
        <button
          type="button"
          class="kb-folder-tree__icon-btn"
          :aria-label="t('knowledgeBase.folderTree.expand')"
          @click="emit('update:collapsed', false)"
        >
          <t-icon name="chevron-right-double" size="15px" />
        </button>
      </t-tooltip>
    </div>

    <div v-if="!collapsed" ref="treeBodyRef" class="kb-folder-tree__body" role="tree" :aria-label="t('knowledgeBase.folderTree.title')" :aria-busy="loading">
      <template v-if="loading && !tree">
        <div v-for="n in 5" :key="'folder-skel-' + n" class="kb-folder-tree__skeleton">
          <t-skeleton animation="gradient" :row-col="[{ width: '100%', height: '16px' }]" />
        </div>
      </template>
      <template v-else>
        <div
          v-for="row in rows"
          :key="row.path || '__root__'"
          class="kb-folder-row"
          :class="{
            active: selectedPath === row.path,
            'is-root': row.kind === 'root',
            'is-editable': canEdit && row.kind === 'folder',
            'is-menu-open': menuOpenPath === row.path,
          }"
          :style="{ '--kb-folder-depth': row.depth }"
          :title="row.kind === 'root' ? t('knowledgeBase.folderTree.rootRowTip') : row.path"
          role="treeitem"
          :data-folder-path="row.path"
          :aria-level="row.depth + 1"
          :aria-selected="selectedPath === row.path"
          :aria-expanded="row.hasChildren ? isExpanded(row.path) : undefined"
          :tabindex="focusedPath === row.path ? 0 : -1"
          @focus="focusedPath = row.path"
          @click="emit('select', row.path)"
          @keydown.self="onFolderKeydown($event, row)"
        >
          <span
            v-if="row.hasChildren"
            class="kb-folder-row__toggle"
            role="button"
            tabindex="0"
            :aria-expanded="isExpanded(row.path)"
            @keydown.enter.stop.prevent="toggle(row.path)"
            @keydown.space.stop.prevent="toggle(row.path)"
            :aria-label="t(isExpanded(row.path)
              ? 'knowledgeBase.folderTree.collapseFolder'
              : 'knowledgeBase.folderTree.expandFolder')"
            @click.stop="toggle(row.path)"
          >
            <t-icon :name="isExpanded(row.path) ? 'chevron-down' : 'chevron-right'" />
          </span>
          <span v-else class="kb-folder-row__toggle-placeholder" aria-hidden="true" />

          <t-icon
            :name="row.kind === 'root' || (row.hasChildren && isExpanded(row.path)) ? 'folder-open' : 'folder'"
            class="kb-folder-row__icon"
          />

          <input
            v-if="isRenaming(row)"
            ref="renameInputRef"
            v-model="renameValue"
            class="kb-folder-row__rename"
            :aria-label="t('knowledgeBase.folderTree.rename')"
            :placeholder="t('knowledgeBase.folderTree.renamePlaceholder')"
            @click.stop
            @keydown.enter.stop.prevent="commitRename(row)"
            @keydown.esc.stop.prevent="cancelRename"
            @blur="commitRename(row)"
          />
          <template v-else>
            <span class="kb-folder-row__label">
              {{ row.kind === 'root' ? (rootLabel || t('knowledgeBase.folderTree.rootRow')) : row.name }}
            </span>
            <span class="kb-folder-row__trailing">
              <span class="kb-folder-row__count"
                :title="t('knowledgeBase.folderTree.countHint', { direct: row.documentCount, total: row.totalCount })">
                {{ row.documentCount }}
              </span>
              <t-popup
                v-if="canEdit && row.kind === 'folder'"
                :visible="menuOpenPath === row.path"
                trigger="click"
                placement="bottom-right"
                destroy-on-close
                overlay-class-name="card-more-popup"
                @visible-change="(visible: boolean) => onFolderMenuVisible(row.path, visible)"
              >
                <button
                  type="button"
                  class="kb-folder-row__more"
                  :aria-expanded="menuOpenPath === row.path"
                  :class="{ 'is-open': menuOpenPath === row.path }"
                  :aria-label="t('knowledgeBase.columnActions')"
                  @click.stop
                >
                  <t-icon name="more" />
                </button>
                <template #content>
                  <div class="popup-menu kb-folder-row__menu" @click.stop>
                    <button type="button" class="popup-menu-item" @click="onFolderMenuRename(row)">
                      <t-icon name="edit" class="menu-icon" />
                      <span>{{ t('knowledgeBase.folderTree.rename') }}</span>
                    </button>
                  </div>
                </template>
              </t-popup>
              <span v-else class="kb-folder-row__more-placeholder" aria-hidden="true"></span>
            </span>
          </template>
        </div>
      </template>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeFolderTree } from '@/api/knowledge-base/index'
import {
  buildFolderRows,
  folderAncestorPaths,
  joinFolderPath,
  ROOT_FOLDER_PATH,
  type FolderRow,
} from '../folderTree'

const props = withDefaults(defineProps<{
  tree: KnowledgeFolderTree | null
  /** Selected folder path; the empty string is the knowledge base top level. */
  rootLabel?: string
  selectedPath: string
  loading?: boolean
  collapsed?: boolean
  canEdit?: boolean
}>(), {
  loading: false,
  collapsed: false,
  canEdit: false,
})

const emit = defineEmits<{
  select: [path: string]
  'update:collapsed': [collapsed: boolean]
  rename: [payload: { from: string; to: string }]
}>()

const { t } = useI18n()

// The root starts expanded so the uploaded structure is visible without a click.
const expanded = ref(new Set<string>([ROOT_FOLDER_PATH]))
// null, not '', because '' is the root's own path: a falsy sentinel would put
// the root row into rename mode permanently.
const renamingPath = ref<string | null>(null)
const menuOpenPath = ref<string | null>(null)
const renameValue = ref('')
const renameInputRef = ref<HTMLInputElement | HTMLInputElement[] | null>(null)

const rows = computed(() => buildFolderRows(props.tree, expanded.value))

const treeBodyRef = ref<HTMLElement | null>(null)
const focusedPath = ref(props.selectedPath)
const focusFolder = async (path: string) => {
  focusedPath.value = path
  await nextTick()
  Array.from(treeBodyRef.value?.querySelectorAll<HTMLElement>('[data-folder-path]') || [])
    .find((element) => element.dataset.folderPath === path)?.focus()
}
const onFolderKeydown = (event: KeyboardEvent, row: FolderRow) => {
  const index = rows.value.findIndex((item) => item.path === row.path)
  if (['Enter', ' '].includes(event.key)) {
    event.preventDefault()
    emit('select', row.path)
    return
  }
  if (!['ArrowDown', 'ArrowUp', 'ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  if (event.key === 'ArrowRight') {
    if (row.hasChildren && !isExpanded(row.path)) toggle(row.path)
    else if (row.hasChildren && rows.value[index + 1]) void focusFolder(rows.value[index + 1].path)
  } else if (event.key === 'ArrowLeft') {
    if (row.hasChildren && isExpanded(row.path)) toggle(row.path)
    else if (row.path) void focusFolder(row.path.includes('/') ? row.path.slice(0, row.path.lastIndexOf('/')) : ROOT_FOLDER_PATH)
  } else {
    const target = event.key === 'Home' ? 0 : event.key === 'End' ? rows.value.length - 1
      : Math.min(rows.value.length - 1, Math.max(0, index + (event.key === 'ArrowDown' ? 1 : -1)))
    if (rows.value[target]) void focusFolder(rows.value[target].path)
  }
}
watch(rows, (current) => {
  if (!current.some((row) => row.path === focusedPath.value)) focusedPath.value = current[0]?.path || ROOT_FOLDER_PATH
})

const isExpanded = (path: string) => expanded.value.has(path)

// The root has no name of its own to edit, and excluding it here means no
// sentinel value can ever put it into rename mode.
const isRenaming = (row: FolderRow) => row.kind === 'folder' && renamingPath.value === row.path

const toggle = (path: string) => {
  const next = new Set(expanded.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  expanded.value = next
}

const startRename = async (row: FolderRow) => {
  renamingPath.value = row.path
  renameValue.value = row.name
  await nextTick()
  const input = Array.isArray(renameInputRef.value) ? renameInputRef.value[0] : renameInputRef.value
  input?.focus()
  input?.select()
}

const onFolderMenuVisible = (path: string, visible: boolean) => {
  menuOpenPath.value = visible ? path : null
}

const onFolderMenuRename = async (row: FolderRow) => {
  menuOpenPath.value = null
  await startRename(row)
}

const cancelRename = () => {
  renamingPath.value = null
  renameValue.value = ''
}

const commitRename = (row: FolderRow) => {
  if (!isRenaming(row)) return
  const name = renameValue.value.trim()
  cancelRename()
  // Only the last segment is edited here; the folder keeps its place in the tree.
  if (!name || name === row.name) return
  const parent = row.path.slice(0, Math.max(0, row.path.length - row.name.length - 1))
  emit('rename', { from: row.path, to: joinFolderPath(parent, name) })
}

// Keep the selected folder reachable: expand the root and every folder above
// the active path, both on first load and when the selection changes from
// elsewhere (e.g. opening a folder from the document list).
watch(
  () => [props.selectedPath, props.tree] as const,
  () => {
    const next = new Set(expanded.value)
    folderAncestorPaths(props.selectedPath).forEach((path) => next.add(path))
    expanded.value = next
  },
  { immediate: true },
)

// First load also opens the top-level folders, so a two-level upload is visible
// in full without any expanding.
watch(
  () => props.tree,
  (tree) => {
    if (!tree?.folders?.length || expanded.value.size > 1) return
    const next = new Set(expanded.value)
    tree.folders.forEach((folder) => next.add(folder.path))
    expanded.value = next
  },
  { immediate: true },
)
</script>

<style scoped lang="less">
.kb-folder-tree {
  --kb-folder-indent: 14px;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  width: 232px;
  min-height: 0;
  padding-right: 16px;
  margin-right: 22px;
  border-right: 1px solid var(--app-border-subtle);
  box-sizing: border-box;

  &.is-collapsed {
    width: auto;
    padding-right: 8px;
    margin-right: 8px;
  }
}

.kb-folder-tree__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  height: 36px;
  padding-bottom: 14px;
  flex-shrink: 0;
}

.kb-folder-tree__title {
  font-size: var(--app-text-md);
  font-weight: 500;
  color: var(--td-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kb-folder-tree__total {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--app-text-xs);
  font-weight: 400;
  color: var(--td-text-color-placeholder);
}

.kb-folder-tree__icon-btn {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 0;
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover {
    color: var(--td-text-color-brand);
    background: var(--td-bg-color-container-hover);
  }
}

.kb-folder-tree__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 8px 0 12px;
  scrollbar-width: thin;

  &::-webkit-scrollbar {
    width: 4px;
  }

  &::-webkit-scrollbar-thumb {
    border-radius: var(--app-radius-xs);
    background: var(--td-scrollbar-color);
  }
}

.kb-folder-tree__skeleton {
  padding: 7px 8px;
}

.kb-folder-row {
  display: flex;
  align-items: center;
  gap: 4px;
  box-sizing: border-box;
  width: 100%;
  height: 38px;
  margin-bottom: 4px;
  padding: 0 4px 0 calc(4px + var(--kb-folder-depth, 0) * var(--kb-folder-indent));
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-primary);
  font-family: var(--app-font-family);
  font-size: var(--app-text-md);
  text-align: left;
  cursor: pointer;
  user-select: none;
  transition: background var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:focus-visible {
    outline: 2px solid var(--app-focus-border);
    outline-offset: 2px;
  }

  &.active {
    background: var(--app-selection-bg);

    .kb-folder-row__label,
    .kb-folder-row__icon {
      color: var(--td-text-color-brand);
    }
    .kb-folder-row__label { font-weight: 500; }
  }

  &.is-root .kb-folder-row__label {
    font-weight: 500;
  }
}

.kb-folder-row__toggle,
.kb-folder-row__toggle-placeholder {
  flex: 0 0 auto;
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-placeholder);
  border-radius: var(--app-radius-xs);
}

.kb-folder-row__toggle {
  cursor: pointer;

  &:hover {
    color: var(--td-text-color-primary);
    background: var(--td-bg-color-secondarycontainer);
  }

  .t-icon {
    font-size: var(--app-text-base);
  }
}

.kb-folder-row__icon {
  flex: 0 0 auto;
  font-size: var(--app-text-xl);
  color: var(--td-text-color-secondary);
}

.kb-folder-row__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-folder-row__trailing {
  flex: 0 0 auto;
  gap: 2px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-left: 2px;
}

.kb-folder-row__count {
  min-width: 16px;
  text-align: right;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.kb-folder-row__more-placeholder { width: 20px; }

.kb-folder-row__more {
  display: inline-flex;
  opacity: .65;
  pointer-events: auto;
  width: 20px;
  height: 20px;
  padding: 0;
  border: 0;
  border-radius: var(--app-radius-xs);
  background: transparent;
  color: var(--td-text-color-placeholder);
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: color var(--app-motion-fast) ease, background-color var(--app-motion-fast) ease;

  &:hover {
    color: var(--td-text-color-brand);
    background: var(--td-bg-color-secondarycontainer);
  }

  .t-icon {
    font-size: var(--app-text-base);
  }
}

.kb-folder-row.is-editable:hover,
.kb-folder-row.is-editable:focus-within,
.kb-folder-row.is-menu-open {
  .kb-folder-row__more {
    opacity: 1;
    pointer-events: auto;
  }
}

.kb-folder-row__more.is-open {
  color: var(--td-text-color-brand);
}

.kb-folder-row__menu {
  min-width: 160px;
  button { width: 100%; border: 0; background: transparent; text-align: left; font: inherit; cursor: pointer; }
}

.kb-folder-row__rename {
  flex: 1;
  min-width: 0;
  height: 22px;
  padding: 0 6px;
  border: 1px solid var(--td-brand-color);
  border-radius: var(--app-radius-xs);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font-family: var(--app-font-family);
  font-size: var(--app-text-md);
  outline: none;
}

.kb-folder-row__more:focus-visible, .kb-folder-tree__icon-btn:focus-visible, .kb-folder-row__toggle:focus-visible, .kb-folder-row__menu button:focus-visible { outline: 2px solid var(--app-focus-border); outline-offset: 2px; }
@media (hover: none) { .kb-folder-row__more { opacity: 1; width: 28px; height: 28px; } }
@media (prefers-reduced-motion: reduce) { .kb-folder-row, .kb-folder-row__more, .kb-folder-tree__icon-btn { transition: none; } }
</style>
