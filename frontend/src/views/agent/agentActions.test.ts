import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import { computed, ref } from 'vue'
import ts from 'typescript'

// Evaluate the component's actual handlers. Only their API and view boundaries
// are substituted; no request or submission logic is duplicated in the fixture.
function componentHandlers(file: string, names: string[]) {
  const { descriptor } = parse(readFileSync(new URL(file, import.meta.url), 'utf8'))
  const source = ts.createSourceFile(file, descriptor.scriptSetup!.content, ts.ScriptTarget.ES2022, true, ts.ScriptKind.TS)
  const parts = names.map(name => {
    const statement = source.statements.find(node =>
      ts.isFunctionDeclaration(node) ? node.name?.text === name
        : ts.isVariableStatement(node) && node.declarationList.declarations.some(item => item.name.getText(source) === name))
    assert.ok(statement, `Missing component handler: ${name}`)
    return statement.getText(source)
  })
  return ts.transpileModule(`${parts.join('\n')}\nObject.assign(globalThis, { ${names.join(',')} });`, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText
}
const saveScript = componentHandlers('./AgentEditorModal.vue', ['handleSave'])
const listScript = componentHandlers('./AgentList.vue', ['RESERVED_SCOPES', 'spaceSelectionOrgId', 'loadSpaceAgents', 'runAgentAction'])
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

function editorFixture(request: () => Promise<any>) {
  const calls: unknown[] = []
  const errors: string[] = []
  const successes: string[] = []
  const events: unknown[] = []
  let closes = 0
  const vm: any = {
    saving: ref(false), editorInitializing: ref(false), editorInitializationGeneration: 1,
    props: { visible: true, readOnly: false }, isBuiltinAgent: ref(true), isAgentMode: ref(true),
    formData: ref({ id: 'agent-a', name: 'Name', config: {
      model_id: 'model-a', system_prompt: 'Keep this user prompt unchanged',
      question_suggestions: { starters: { items: ['Question'] } },
    } }),
    currentSection: ref('basic'), editorMode: ref('edit'), savedAgent: ref(null),
    hostOnly: ref(false), promptTemplates: ref(null), storedToolNames: ref([]), keepAllowedToolsUnset: ref(false),
    pruneSelectedSkills() {}, allowedToolsToSave: () => [], serializeAgentPrompts: (config: unknown) => config,
    t: (key: string) => key, MessagePlugin: { error: (x: string) => errors.push(x), success: (x: string) => successes.push(x) },
    async updateAgent(id: string, payload: unknown) { calls.push({ id, payload }); return request() },
    async createAgent(payload: unknown) { calls.push(payload); return request() },
    emit: (...args: unknown[]) => events.push(args), handleClose: () => { closes += 1 },
    notifyAllowedToolsWarnings() {}, modalShell: { markClean() {} }, markContextualGuideDone() {},
    loadAgentIntegrationCounts() {}, normalizeLegacyToolNames: (values: string[]) => values,
    Error,
  }
  runInNewContext(saveScript, vm)
  return { vm, calls, errors, successes, events, closes: () => closes }
}

test('edit save submits once, preserves prompt payload, and closes only after success', async () => {
  const pending = deferred<any>()
  const f = editorFixture(() => pending.promise)
  const first = f.vm.handleSave()
  await f.vm.handleSave()
  assert.equal(f.calls.length, 1)
  assert.equal(f.vm.saving.value, true)
  assert.equal(f.closes(), 0)
  assert.equal((f.calls[0] as any).payload.config.system_prompt, 'Keep this user prompt unchanged')
  pending.resolve({ data: { id: 'agent-a' } })
  await first
  assert.equal(f.vm.saving.value, false)
  assert.equal(f.closes(), 1)
  assert.deepEqual(f.events, [['success']])
})

test('failed save keeps editor open and releases its lock for retry', async () => {
  let attempts = 0
  const f = editorFixture(async () => { if (++attempts === 1) throw new Error('Offline'); return { data: { id: 'agent-a' } } })
  await f.vm.handleSave()
  assert.equal(f.vm.saving.value, false)
  assert.equal(f.closes(), 0)
  assert.deepEqual(f.errors, ['Offline'])
  await f.vm.handleSave()
  assert.equal(f.calls.length, 2)
  assert.equal(f.closes(), 1)
})

test('read-only and initializing editors cannot submit through direct calls', async () => {
  const f = editorFixture(async () => ({}))
  f.vm.props.readOnly = true
  await f.vm.handleSave()
  f.vm.props.readOnly = false
  f.vm.editorInitializing.value = true
  await f.vm.handleSave()
  assert.equal(f.calls.length, 0)
})

test('a save from an older editor session cannot close or unlock the current one', async () => {
  const pending = deferred<any>()
  const f = editorFixture(() => pending.promise)
  const old = f.vm.handleSave()
  ++f.vm.editorInitializationGeneration
  pending.resolve({ data: { id: 'agent-a' } })
  await old
  assert.equal(f.vm.saving.value, true)
  assert.equal(f.closes(), 0)
  assert.deepEqual(f.events, [])
  assert.deepEqual(f.successes, [])
})

test('first creation stays open with its saved id and cannot duplicate requests', async () => {
  const pending = deferred<any>()
  const f = editorFixture(() => pending.promise)
  f.vm.editorMode.value = 'create'
  const creating = f.vm.handleSave()
  await f.vm.handleSave()
  assert.equal(f.calls.length, 1)
  pending.resolve({ data: { id: 'new-agent', config: {} } })
  await creating
  assert.equal(f.vm.savedAgent.value.id, 'new-agent')
  assert.equal(f.vm.formData.value.id, 'new-agent')
  assert.equal(f.closes(), 0)
  assert.equal(f.vm.saving.value, false)
})

function listFixture(list: (id: string) => Promise<any>) {
  const requests: string[] = []
  const errors: string[] = []
  const vm: any = {
    computed, spaceSelection: ref('all'), spaceGeneration: 0,
    spaceAgentsList: ref([]), spaceAgentsLoading: ref(false), spaceLoadFailed: ref(false), spaceAgentCountByOrg: ref({}),
    async listOrganizationSharedAgents(id: string) { requests.push(id); return list(id) },
    pendingAgents: ref(new Set<string>()), openMoreAgentId: ref(null),
    fetchList: async () => {}, t: (key: string) => key,
    MessagePlugin: { success() {}, error: (x: string) => errors.push(x) },
  }
  runInNewContext(listScript, vm)
  return { vm, requests, errors }
}

test('reserved scopes never request organization agents', async () => {
  const f = listFixture(async () => { throw new Error('Should not fetch') })
  for (const scope of ['all', 'mine', 'favorites', 'recents', 'shared', '']) {
    f.vm.spaceSelection.value = scope
    await f.vm.loadSpaceAgents()
  }
  assert.deepEqual(f.requests, [])
})

test('new space response wins over an older response and switching to favorites clears loading', async () => {
  const old = deferred<any>()
  const fresh = deferred<any>()
  const f = listFixture(id => id === 'org-a' ? old.promise : fresh.promise)
  f.vm.spaceSelection.value = 'org-a'
  const first = f.vm.loadSpaceAgents()
  f.vm.spaceSelection.value = 'org-b'
  const second = f.vm.loadSpaceAgents()
  fresh.resolve({ success: true, data: [{ id: 'new' }] })
  await second
  old.resolve({ success: true, data: [{ id: 'old' }] })
  await first
  assert.equal(f.vm.spaceAgentsList.value[0].id, 'new')
  f.vm.spaceSelection.value = 'favorites'
  await f.vm.loadSpaceAgents()
  assert.equal(f.vm.spaceAgentsList.value.length, 0)
  assert.equal(f.vm.spaceAgentsLoading.value, false)
})

test('list row actions exclude competing requests, then unlock after rejection', async () => {
  const pending = deferred<any>()
  const f = listFixture(async () => ({}))
  let calls = 0
  const request = () => { ++calls; return pending.promise }
  const first = f.vm.runAgentAction('agent-a', request, 'ok', 'error')
  await f.vm.runAgentAction('agent-a', request, 'ok', 'error')
  assert.equal(calls, 1)
  assert.equal(f.vm.pendingAgents.value.has('agent-a'), true)
  pending.reject(new Error('Unavailable'))
  await first
  assert.equal(f.vm.pendingAgents.value.has('agent-a'), false)
  assert.deepEqual(f.errors, ['Unavailable'])
  await f.vm.runAgentAction('agent-a', async () => { ++calls; return { success: true } }, 'ok', 'error')
  assert.equal(calls, 2)
})
