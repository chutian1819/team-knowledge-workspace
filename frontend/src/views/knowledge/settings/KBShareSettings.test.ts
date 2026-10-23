import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import type { KnowledgeBaseShare } from '../../../api/organization'

// Execute the actual setup and watchers, mocking only network/store boundaries.
const filename = fileURLToPath(new URL('./KBShareSettings.vue', import.meta.url))
const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
const script = compileScript(descriptor, { id: 'kb-share-settings-test' }).content
  .replace('__expose();', '')
  .replace('return __returned__', '__expose(__returned__); return __returned__')
const compiled = ts.transpileModule(script, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

interface Result { success: boolean; message?: string; data?: { shares: KnowledgeBaseShare[] } }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

function share(id = 'share-a', kbId = 'kb-a'): KnowledgeBaseShare {
  return {
    id, knowledge_base_id: kbId, organization_id: `org-${id}`, organization_name: `Space ${id}`,
    shared_by_user_id: 'owner', source_tenant_id: 1, permission: 'viewer', created_at: '2026-10-21T00:00:00Z',
  }
}
const response = (rows: KnowledgeBaseShare[]): Result => ({ success: true, data: { shares: rows } })
const renderer = vue.createRenderer<any, any>({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}),
  insert() {}, remove() {}, setElementText() {}, setText() {}, patchProp() {},
  parentNode: () => null, nextSibling: () => null,
})
async function settle() {
  await new Promise<void>(resolve => setImmediate(resolve))
  await vue.nextTick()
}

async function fixture(options: {
  list?: (kbId: string) => Promise<Result>
  create?: () => Promise<Result>
  update?: () => Promise<Result>
  remove?: () => Promise<Result>
  canShare?: boolean
} = {}) {
  const calls = {
    list: [] as string[],
    create: [] as Array<{ kbId: string; organizationId: string; permission: string }>,
    update: [] as Array<{ kbId: string; id: string; permission: string }>,
    remove: [] as Array<{ kbId: string; id: string; organizationId: string }>,
  }
  const errors: string[] = []
  const successes: string[] = []
  const vueErrors: unknown[] = []
  const props = vue.reactive({ kbId: 'kb-a', canShare: options.canShare ?? true })
  const store = {
    organizations: [],
    async fetchOrganizations() {},
    async shareKnowledgeBase(kbId: string, data: { organization_id: string; permission: string }) {
      calls.create.push({ kbId, organizationId: data.organization_id, permission: data.permission })
      return options.create ? options.create() : { success: true }
    },
    async changeKnowledgeBaseSharePermission(kbId: string, id: string, data: { permission: string }) {
      calls.update.push({ kbId, id, permission: data.permission })
      return options.update ? options.update() : { success: true }
    },
    async unshareKnowledgeBase(kbId: string, id: string, organizationId: string) {
      calls.remove.push({ kbId, id, organizationId })
      return options.remove ? options.remove() : { success: true }
    },
  }
  const exports: any = {}
  runInNewContext(compiled, {
    exports, Error, console,
    require(name: string) {
      if (name === 'vue') return vue
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
      if (name === 'tdesign-vue-next') return {
        MessagePlugin: { success: (message: string) => successes.push(message), error: (message: string) => errors.push(message) },
      }
      if (name === '@/stores/organization') return { useOrganizationStore: () => store }
      if (name === '@/api/organization') return {
        async listKBShares(kbId: string) {
          calls.list.push(kbId)
          return options.list ? options.list(kbId) : response([share('share-a', kbId)])
        },
      }
      if (name.endsWith('.vue')) return { default: {} }
      throw new Error(`Unexpected dependency: ${name}`)
    },
  })
  const component = exports.default
  component.render = () => null
  const instance = vue.ref<any>()
  const app = renderer.createApp({ render: () => vue.h(component, { ...props, ref: instance }) })
  app.config.errorHandler = error => { vueErrors.push(error) }
  app.mount({})
  await settle()
  return {
    vm: instance.value, props, calls, errors, successes,
    close() { app.unmount(); assert.deepEqual(vueErrors, [], 'component watchers must not throw') },
  }
}
const ids = (vm: any): string[] => Array.from(vm.shares, (row: KnowledgeBaseShare) => row.id)

test('sharing blocks duplicate submissions and only clears the draft after success', async () => {
  const pending = deferred<Result>()
  const f = await fixture({ create: () => pending.promise })
  try {
    f.vm.selectedOrgId = 'org-new'
    f.vm.selectedPermission = 'editor'
    f.vm.addPopupVisible = true
    const saving = f.vm.handleShare()
    assert.equal(f.vm.submitting, true)
    await f.vm.handleShare()
    assert.deepEqual(f.calls.create, [{ kbId: 'kb-a', organizationId: 'org-new', permission: 'editor' }])
    assert.equal(f.vm.selectedOrgId, 'org-new')
    assert.equal(f.vm.addPopupVisible, true)
    assert.equal(f.calls.list.length, 1, 'do not refresh before persistence succeeds')
    pending.resolve({ success: true })
    await saving
    assert.equal(f.vm.submitting, false)
    assert.equal(f.vm.selectedOrgId, '')
    assert.equal(f.vm.selectedPermission, 'viewer')
    assert.equal(f.vm.addPopupVisible, false)
    assert.deepEqual(f.calls.list, ['kb-a', 'kb-a'])
    assert.deepEqual(f.successes, ['organization.share.shareSuccess'])
  } finally { pending.resolve({ success: false }); f.close() }
})

test('failed sharing retains the destination and allows a retry', async () => {
  let attempts = 0
  const f = await fixture({ create: async () => ++attempts === 1
    ? { success: false, message: 'Sharing unavailable' } : { success: true } })
  try {
    f.vm.selectedOrgId = 'org-new'
    f.vm.selectedPermission = 'editor'
    f.vm.addPopupVisible = true
    await f.vm.handleShare()
    assert.equal(f.vm.submitting, false)
    assert.equal(f.vm.selectedOrgId, 'org-new')
    assert.equal(f.vm.selectedPermission, 'editor')
    assert.equal(f.vm.addPopupVisible, true)
    assert.deepEqual(f.errors, ['Sharing unavailable'])
    assert.equal(f.calls.list.length, 1)
    await f.vm.handleShare()
    assert.equal(f.calls.create.length, 2)
    assert.equal(f.vm.submitting, false)
    assert.equal(f.vm.addPopupVisible, false)
  } finally { f.close() }
})

for (const action of ['update', 'remove'] as const) {
  for (const failure of ['response', 'rejection'] as const) {
    test(`${action}: pending blocks competing row actions; ${failure} clears it and permits retry`, async () => {
      const pending = deferred<Result>()
      let attempts = 0
      const operation = () => ++attempts === 1 ? pending.promise : Promise.resolve({ success: true })
      const f = await fixture({ [action]: operation })
      try {
        const row = f.vm.shares[0]
        const invoke = () => action === 'update' ? f.vm.handleUpdatePermission(row, 'editor') : f.vm.handleUnshare(row)
        const saving = invoke()
        assert.equal(f.vm.pendingShares.has(row.id), true)
        await f.vm.handleUpdatePermission(row, 'editor')
        await f.vm.handleUnshare(row)
        assert.equal(f.calls[action].length, 1)
        assert.equal(f.calls[action === 'update' ? 'remove' : 'update'].length, 0)
        assert.equal(row.permission, 'viewer', 'permission must not change optimistically')
        if (failure === 'response') pending.resolve({ success: false, message: 'Permission service unavailable' })
        else pending.reject(new Error('Permission service unavailable'))
        await saving
        assert.equal(f.vm.pendingShares.size, 0)
        assert.deepEqual(ids(f.vm), ['share-a'])
        assert.equal(f.vm.shares[0].permission, 'viewer')
        assert.deepEqual(f.errors, ['Permission service unavailable'])
        assert.equal(f.calls.list.length, 1, 'failed mutations must not refresh or discard the row')
        await invoke()
        assert.equal(f.calls[action].length, 2, 'retry must reach the service')
        assert.equal(f.vm.pendingShares.size, 0)
        assert.equal(f.calls.list.length, 2)
      } finally { pending.resolve({ success: false }); f.close() }
    })
  }
}

test('read-only access prevents all share mutations even if handlers are called directly', async () => {
  const f = await fixture({ canShare: false })
  try {
    f.vm.selectedOrgId = 'org-new'
    await f.vm.handleShare()
    await f.vm.handleUpdatePermission(f.vm.shares[0], 'editor')
    await f.vm.handleUnshare(f.vm.shares[0])
    assert.equal(f.calls.create.length + f.calls.update.length + f.calls.remove.length, 0)
    assert.equal(f.vm.submitting, false)
    assert.equal(f.vm.pendingShares.size, 0)
  } finally { f.close() }
})

test('switching knowledge bases discards a late success from the previous knowledge base', async () => {
  const oldLoad = deferred<Result>()
  const newLoad = deferred<Result>()
  const f = await fixture({ list: kbId => kbId === 'kb-a' ? oldLoad.promise : newLoad.promise })
  try {
    f.vm.searchQuery = 'old query'
    f.vm.selectedOrgId = 'org-old'
    f.vm.addPopupVisible = true
    f.props.kbId = 'kb-b'
    await settle()
    assert.deepEqual(f.calls.list, ['kb-a', 'kb-b'])
    assert.deepEqual(ids(f.vm), [])
    assert.equal(f.vm.searchQuery, '')
    assert.equal(f.vm.selectedOrgId, '')
    assert.equal(f.vm.addPopupVisible, false)
    newLoad.resolve(response([share('share-b', 'kb-b')]))
    await settle()
    assert.deepEqual(ids(f.vm), ['share-b'])
    assert.equal(f.vm.loadingShares, false)
    oldLoad.resolve(response([share('share-a', 'kb-a')]))
    await settle()
    assert.deepEqual(ids(f.vm), ['share-b'], 'old results must not replace the current list')
    assert.equal(f.vm.loadFailed, false)
    assert.equal(f.vm.loadingShares, false)
  } finally { oldLoad.resolve(response([])); newLoad.resolve(response([])); f.close() }
})

test('a stale failure cannot hide the new loading state or display an error', async () => {
  const oldLoad = deferred<Result>()
  const newLoad = deferred<Result>()
  const f = await fixture({ list: kbId => kbId === 'kb-a' ? oldLoad.promise : newLoad.promise })
  try {
    f.props.kbId = 'kb-b'
    await settle()
    oldLoad.reject(new Error('Old request failed'))
    await settle()
    assert.equal(f.vm.loadingShares, true, 'the new request remains in flight')
    assert.equal(f.vm.loadFailed, false)
    assert.deepEqual(f.errors, [])
    newLoad.resolve(response([share('share-b', 'kb-b')]))
    await settle()
    assert.deepEqual(ids(f.vm), ['share-b'])
    assert.equal(f.vm.loadingShares, false)
    assert.equal(f.vm.loadFailed, false)
  } finally { oldLoad.resolve(response([])); newLoad.resolve(response([])); f.close() }
})

test('a newer refresh also wins when both requests target the same knowledge base', async () => {
  const oldLoad = deferred<Result>()
  const newLoad = deferred<Result>()
  let loads = 0
  const f = await fixture({ list: () => ++loads === 1 ? oldLoad.promise : newLoad.promise })
  try {
    const refresh = f.vm.loadShares()
    newLoad.resolve(response([share('latest')]))
    await refresh
    oldLoad.resolve(response([share('outdated')]))
    await settle()
    assert.deepEqual(ids(f.vm), ['latest'])
    assert.equal(f.vm.loadingShares, false)
  } finally { oldLoad.resolve(response([])); newLoad.resolve(response([])); f.close() }
})

test('clearing the knowledge base ends loading and prevents the pending response from restoring old shares', async () => {
  const pending = deferred<Result>()
  const f = await fixture({ list: () => pending.promise })
  try {
    assert.equal(f.vm.loadingShares, true)
    f.props.kbId = ''
    await settle()
    assert.equal(f.vm.loadingShares, false, 'an empty knowledge base must not stay in loading state')
    assert.equal(f.vm.loadFailed, false)
    assert.deepEqual(ids(f.vm), [])
    assert.deepEqual(f.calls.list, ['kb-a'], 'clearing selection must not request an empty knowledge base')
    pending.resolve(response([share('outdated', 'kb-a')]))
    await settle()
    assert.deepEqual(ids(f.vm), [], 'the old response must not restore a cleared list')
    assert.equal(f.vm.loadingShares, false)
    assert.equal(f.vm.loadFailed, false)
  } finally { pending.resolve(response([])); f.close() }
})
