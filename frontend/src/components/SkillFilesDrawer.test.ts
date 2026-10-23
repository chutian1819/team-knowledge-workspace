import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createRenderer, defineComponent, h, nextTick, reactive } from 'vue'

const require = createRequire(import.meta.url)
const filename = fileURLToPath(new URL('./SkillFilesDrawer.vue', import.meta.url))
const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
const compiled = ts.transpileModule(compileScript(descriptor, { id: 'skill-files-drawer-test', inlineTemplate: true }).content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

async function fixture() {
  const props = reactive({ visible: false, skillName: 'Report skill', catalogId: 'catalog-one', configId: '', skillId: '' })
  let drawerMounts = 0, panelMounts = 0, measured = 720
  let drawer: any, panel: any
  let measure: (() => void) | undefined
  const listeners = new Map<string, () => void>()
  const window = { innerWidth: 1440, addEventListener: (key: string, callback: () => void) => listeners.set(key, callback), removeEventListener: (key: string) => listeners.delete(key) }
  const Drawer = defineComponent({
    props: ['visible', 'title', 'description', 'width', 'minWidth', 'maxWidth', 'storageKey', 'hideFooter', 'zIndex'],
    emits: ['update:visible'],
    setup(values, { slots, emit }) { drawerMounts++; drawer = { props: values, close: () => emit('update:visible', false) }; return () => h('section', slots.default?.()) },
  })
  const Panel = defineComponent({
    props: ['catalogId', 'configId', 'skillId', 'drawerWidth'],
    setup(values) { panelMounts++; panel = values; return () => h('div') },
  })
  const exports: any = {}
  runInNewContext(compiled, {
    exports, window,
    ResizeObserver: class { constructor(callback: () => void) { measure = callback } observe() {} disconnect() {} },
    require(name: string) {
      if (name === 'vue') return require('vue')
      if (name.endsWith('/SettingDrawer.vue')) return { __esModule: true, default: Drawer }
      if (name.endsWith('/SkillFilesPanel.vue')) return { __esModule: true, default: Panel }
      throw new Error(`Unexpected import: ${name}`)
    },
  })
  const renderer = createRenderer<any, any>({
    createElement: () => ({ getBoundingClientRect: () => ({ width: measured }) }),
    createText: () => ({}), createComment: () => ({}),
    insert() {}, remove() {}, setElementText() {}, setText() {}, patchProp() {},
    parentNode: () => null, nextSibling: () => null,
  })
  const app = renderer.createApp({ render: () => h(exports.default, { ...props, 'onUpdate:visible': (value: boolean) => { props.visible = value } }) })
  app.config.globalProperties.$t = (key: string) => key
  app.mount({})
  await nextTick()
  return {
    props, drawer: () => drawer, panel: () => panel, mounts: () => ({ drawerMounts, panelMounts }),
    resize: async (viewport: number, width: number) => { window.innerWidth = viewport; measured = width; listeners.get('resize')?.(); measure?.(); await nextTick() },
    close: () => app.unmount(),
  }
}

test('hidden file drawers do not mount a dialog or file panel and opening retains the shared drawer settings', async () => {
  const f = await fixture()
  try {
    assert.deepEqual(f.mounts(), { drawerMounts: 0, panelMounts: 0 })
    f.props.visible = true
    await nextTick()
    assert.deepEqual(f.mounts(), { drawerMounts: 1, panelMounts: 1 })
    assert.equal(f.drawer().props.storageKey, 'skill-files-drawer:width')
    assert.equal(f.drawer().props.width, '720px')
    assert.equal(f.drawer().props.minWidth, 480)
    assert.equal(f.drawer().props.maxWidth, 1200)
    assert.equal(f.drawer().props.zIndex, 2600)
    assert.equal(f.panel().catalogId, 'catalog-one')
    assert.equal(f.panel().drawerWidth, 720)
    f.drawer().close()
    await nextTick()
    assert.equal(f.props.visible, false)
  } finally { f.close() }
})

test('file panel tracks resized desktop width and disables the vertical splitter on narrow screens', async () => {
  const f = await fixture()
  try {
    f.props.visible = true
    await nextTick()
    await f.resize(1440, 960)
    assert.equal(f.panel().drawerWidth, 960)
    await f.resize(390, 390)
    assert.equal(f.drawer().props.maxWidth, 390)
    assert.equal(f.panel().drawerWidth, 0)
    await f.resize(1024, 800)
    assert.equal(f.drawer().props.maxWidth, 942)
    assert.equal(f.panel().drawerWidth, 800)
  } finally { f.close() }
})

test('installed skill files retain the configuration and skill identifiers', async () => {
  const f = await fixture()
  try {
    f.props.catalogId = ''
    f.props.configId = 'config-one'
    f.props.skillId = 'skill-one'
    f.props.visible = true
    await nextTick()
    assert.equal(f.panel().catalogId, '')
    assert.equal(f.panel().configId, 'config-one')
    assert.equal(f.panel().skillId, 'skill-one')
  } finally { f.close() }
})
