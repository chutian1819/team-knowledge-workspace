import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import type { AgentShareResponse } from '../api/organization'

// Execute the actual setup and watchers, mocking only network/store boundaries.
const filename = fileURLToPath(new URL('./AgentShareSettings.vue', import.meta.url))
const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
const script = compileScript(descriptor, { id: 'agent-share-settings-test' }).content
  .replace(/__expose\(\{ loadShares \}\);?/, '')
  .replace('return __returned__', '__expose(__returned__); return __returned__')
const compiled = ts.transpileModule(script, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

interface Result { success: boolean; message?: string; data?: { shares: AgentShareResponse[] } }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

function share(id = 'share-a', agentId = 'agent-a'): AgentShareResponse {
  return {
    id, agent_id: agentId, organization_id: `org-${id}`, organization_name: `Space ${id}`,
    shared_by_user_id: 'owner', source_tenant_id: 1, permission: 'viewer', created_at: '2026-10-21T00:00:00Z',
  }
}
const response = (rows: AgentShareResponse[]): Result => ({ success: true, data: { shares: rows } })
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
  list?: (agentId: string) => Promise<Result>
  create?: () => Promise<Result>
  remove?: () => Promise<Result>
  canShare?: boolean
} = {}) {
  const calls = {
    list: [] as string[],
    create: [] as Array<{ agentId: string; organizationId: string; permission: string }>,
    remove: [] as Array<{ agentId: string; id: string; organizationId: string }>,
  }
  const errors: string[] = []
  const successes: string[] = []
  const vueErrors: unknown[] = []
  const props = vue.reactive({ agentId: 'agent-a', canShare: options.canShare ?? true })
  const store = {
    organizations: [],
    async fetchOrganizations() {},
    async shareAgent(agentId: string, data: { organization_id: string; permission: string }) {
      calls.create.push({ agentId, organizationId: data.organization_id, permission: data.permission })
      return options.create ? options.create() : { success: true }
    },
    async unshareAgent(agentId: string, id: string, organizationId: string) {
      calls.remove.push({ agentId, id, organizationId })
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
        async listAgentShares(agentId: string) {
          calls.list.push(agentId)
          return options.list ? options.list(agentId) : response([share('share-a', agentId)])
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
const ids = (vm: any): string[] => Array.from(vm.shares, (row: AgentShareResponse) => row.id)

test('loads once and blocks duplicate sharing until persistence succeeds', async () => {
  const pending = deferred<Result>()
  const f = await fixture({ create: () => pending.promise })
  try {
    assert.deepEqual(f.calls.list, ['agent-a'])
    f.vm.selectedOrgId = 'org-new'
    f.vm.addPopupVisible = true
    const saving = f.vm.handleShare()
    await f.vm.handleShare()
    f.vm.popupVisible = false
    assert.equal(f.vm.addPopupVisible, true)
    assert.equal(f.calls.create.length, 1)
    assert.equal(f.vm.selectedOrgId, 'org-new')
    pending.resolve({ success: true })
    await saving
    assert.equal(f.vm.submitting, false)
    assert.equal(f.vm.selectedOrgId, '')
    assert.equal(f.vm.addPopupVisible, false)
    assert.deepEqual(f.calls.list, ['agent-a', 'agent-a'])
  } finally { pending.resolve({ success: false }); f.close() }
})

for (const reject of [false, true]) {
  test(`failed unshare releases its row lock and permits retry (rejection=${reject})`, async () => {
    const pending = deferred<Result>()
    let attempts = 0
    const f = await fixture({ remove: () => ++attempts === 1 ? pending.promise : Promise.resolve({ success: true }) })
    try {
      const row = f.vm.shares[0]
      const removing = f.vm.handleUnshare(row)
      await f.vm.handleUnshare(row)
      assert.equal(f.calls.remove.length, 1)
      assert.equal(f.vm.pendingShares.has(row.id), true)
      if (reject) pending.reject(new Error('Unavailable'))
      else pending.resolve({ success: false, message: 'Unavailable' })
      await removing
      assert.equal(f.vm.pendingShares.has(row.id), false)
      assert.deepEqual(ids(f.vm), ['share-a'])
      assert.deepEqual(f.errors, ['Unavailable'])
      await f.vm.handleUnshare(row)
      assert.equal(f.calls.remove.length, 2)
    } finally { pending.resolve({ success: false }); f.close() }
  })
}

test('failed share preserves destination and releases submission lock', async () => {
  const f = await fixture({ create: async () => { throw new Error('Unavailable') } })
  try {
    f.vm.selectedOrgId = 'org-new'
    f.vm.addPopupVisible = true
    await f.vm.handleShare()
    assert.equal(f.vm.submitting, false)
    assert.equal(f.vm.selectedOrgId, 'org-new')
    assert.equal(f.vm.addPopupVisible, true)
    assert.deepEqual(f.errors, ['Unavailable'])
  } finally { f.close() }
})

test('switching agent invalidates old loads and stale errors', async () => {
  const old = deferred<Result>()
  const fresh = deferred<Result>()
  const f = await fixture({ list: id => id === 'agent-a' ? old.promise : fresh.promise })
  try {
    f.props.agentId = 'agent-b'
    await settle()
    old.reject(new Error('Old failure'))
    await settle()
    assert.equal(f.vm.loadingShares, true)
    assert.equal(f.vm.loadFailed, false)
    fresh.resolve(response([share('fresh', 'agent-b')]))
    await settle()
    assert.deepEqual(ids(f.vm), ['fresh'])
    assert.equal(f.vm.loadingShares, false)
    assert.deepEqual(f.errors, [])
  } finally { old.resolve(response([])); fresh.resolve(response([])); f.close() }
})

test('clearing agent prevents an old success from restoring shares or loading', async () => {
  const old = deferred<Result>()
  const f = await fixture({ list: () => old.promise })
  try {
    f.props.agentId = ''
    await settle()
    assert.equal(f.vm.loadingShares, false)
    old.resolve(response([share()]))
    await settle()
    assert.deepEqual(ids(f.vm), [])
    assert.equal(f.vm.loadingShares, false)
    assert.equal(f.vm.loadFailed, false)
    assert.deepEqual(f.calls.list, ['agent-a'])
  } finally { old.resolve(response([])); f.close() }
})

test('old mutations cannot clear the next agent draft or release its active lock', async () => {
  const old = deferred<Result>()
  const fresh = deferred<Result>()
  let attempts = 0
  const f = await fixture({ create: () => ++attempts === 1 ? old.promise : fresh.promise })
  try {
    f.vm.selectedOrgId = 'old-org'
    const first = f.vm.handleShare()
    f.props.agentId = 'agent-b'
    await settle()
    f.vm.selectedOrgId = 'new-org'
    f.vm.addPopupVisible = true
    const second = f.vm.handleShare()
    old.resolve({ success: true })
    await first
    assert.equal(f.vm.selectedOrgId, 'new-org')
    assert.equal(f.vm.addPopupVisible, true)
    assert.equal(f.vm.submitting, true)
    assert.deepEqual(f.successes, [])
    fresh.resolve({ success: true })
    await second
    assert.equal(f.vm.submitting, false)
  } finally { old.resolve({ success: false }); fresh.resolve({ success: false }); f.close() }
})

test('read-only sharing does not mutate through direct handler calls', async () => {
  const f = await fixture({ canShare: false })
  try {
    f.vm.selectedOrgId = 'org-new'
    await f.vm.handleShare()
    await f.vm.handleUnshare(f.vm.shares[0])
    assert.deepEqual(f.calls.create, [])
    assert.deepEqual(f.calls.remove, [])
  } finally { f.close() }
})
