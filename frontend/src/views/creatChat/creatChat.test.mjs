import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

const source = readFileSync(new URL('./creatChat.vue', import.meta.url), 'utf8')

test('new-session createSessions includes project_dir only through withOptionalProjectDir', () => {
  assert.match(source, /withOptionalProjectDir/)
  assert.match(source, /createSessions\(withOptionalProjectDir/)
  assert.match(source, /selectedProjectDir/)
  assert.match(source, /hostSandboxEnabled/)
  assert.doesNotMatch(source, /approvalMode|GetApprovalMode|SetApprovalMode/)
})

test('new-session page has a single project button flush above the composer', () => {
  assert.match(source, /pickHostProjectDir/)
  assert.match(source, /create-chat-composer/)
  assert.match(source, /createChat\.openProject/)
  assert.doesNotMatch(source, /wailsjs\/go\/main\/App/)
  assert.doesNotMatch(source, /PickProjectFile/)
  assert.doesNotMatch(source, /createChat\.openFile/)
  assert.doesNotMatch(source, /t-select/)
  assert.doesNotMatch(source, /project-dir-picker/)
})

const createCode = source.slice(source.indexOf('async function createNewSession'), source.indexOf('const navigateToSession'))

function createHarness() {
  let resolveRequest, rejectRequest, requests = 0
  const navigations = [], errors = []
  const context = {
    creatingSession: { value: false }, pageDisposed: false,
    settingsStore: { settings: {}, agentConfig: {} }, selectedProjectDir: { value: '' },
    withOptionalProjectDir: data => data,
    createSessions: () => { requests++; return new Promise((resolve, reject) => { resolveRequest = resolve; rejectRequest = reject }) },
    navigateToSession: async (...args) => navigations.push(args),
    MessagePlugin: { error: message => errors.push(message) }, t: key => key, console: { error() {} },
  }
  const create = vm.runInNewContext(ts.transpile(`${createCode}\ncreateNewSession`), context)
  return { create, context, navigations, errors, get requests() { return requests },
    resolve: value => resolveRequest(value), reject: error => rejectRequest(error) }
}

test('a new-session request ignores double submission and carries the original draft forward', async () => {
  const harness = createHarness()
  const pending = harness.create('first draft', 'model')
  await harness.create('second draft', 'model')
  assert.equal(harness.requests, 1)
  harness.resolve({ data: { id: 'created' } })
  await pending
  assert.equal(harness.navigations.length, 1)
  assert.deepEqual(harness.navigations[0].slice(0, 3), ['created', 'first draft', 'model'])
  assert.equal(harness.context.creatingSession.value, false)
})

test('failed creation keeps the composer draft and allows a retry', async () => {
  assert.match(source, /:composer-locked="creatingSession" preserve-draft-on-send/)
  const harness = createHarness()
  const pending = harness.create('keep this draft', 'model')
  harness.reject(new Error('offline'))
  await pending
  assert.equal(harness.context.creatingSession.value, false)
  assert.equal(harness.errors.length, 1)
  assert.equal(harness.navigations.length, 0)
  const retry = harness.create('keep this draft', 'model')
  assert.equal(harness.requests, 2)
  harness.resolve({ data: { id: 'created' } })
  await retry
  assert.equal(harness.navigations[0][1], 'keep this draft')
})

test('leaving the new-session page prevents a late response from redirecting the user', async () => {
  const harness = createHarness()
  const pending = harness.create('draft', 'model')
  harness.context.pageDisposed = true
  harness.resolve({ data: { id: 'created' } })
  await pending
  assert.equal(harness.navigations.length, 0)
  assert.equal(harness.context.creatingSession.value, false)
})
