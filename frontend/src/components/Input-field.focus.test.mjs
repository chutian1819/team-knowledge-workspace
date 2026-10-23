import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

const source = readFileSync(new URL('./Input-field.vue', import.meta.url), 'utf8')
const focusCode = source.slice(source.indexOf('const focusInput ='), source.indexOf('const onInput ='))
const sendCode = source.slice(source.indexOf('const createSession ='), source.indexOf('const updateAgentModeDropdownPosition ='))

for (const mode of ['normal', 'embedded', 'after', 'inject']) {
  test(`${mode} send clears the draft and restores focus after the DOM update`, async () => {
    const effects = [], ticks = []
    const textarea = {
      isConnected: true,
      focus: options => effects.push(['focus', options.preventScroll]),
      blur: () => { throw new Error('sending must not blur the textarea') },
    }
    const context = {
      sendPending: { value: false }, composerDisposed: false,
      props: { isReplying: ['after', 'inject'].includes(mode), canSteer: true, embeddedMode: mode === 'embedded' },
      uploadedAttachments: { value: [] }, uploadedImages: { value: [] },
      allSelectedItems: { value: [] }, selectedModelId: { value: 'model' },
      selectedAgent: { value: { config: {} } },
      settingsStore: {}, chatResources: { isLoaded: () => true },
      collectAgentNotReadyReasons: () => ({ keys: [], labels: [] }),
      attachmentUploadRef: { value: null },
      emit: name => effects.push([name]),
      clearvalue: () => effects.push(['clear']),
      getTextareaEl: () => textarea,
      nextTick: () => new Promise(resolve => ticks.push(resolve)),
    }
    const send = vm.runInNewContext(ts.transpile(`${focusCode}\n${sendCode}\ncreateSession`), context)
    await send('hello', mode === 'inject' ? 'inject' : 'after')
    assert.deepEqual(effects, [[context.props.isReplying ? 'steer-msg' : 'send-msg'], ['clear']])
    for (const resolve of ticks) resolve()
    await new Promise(setImmediate)
    assert.deepEqual(effects.at(-1), ['focus', true])
  })
}

test('a queued focus does not focus a textarea removed during navigation', async () => {
  let resolveTick, connected = true, focused = false
  const focus = vm.runInNewContext(`${focusCode}\nfocusInput`, {
    nextTick: () => new Promise(resolve => { resolveTick = resolve }),
    getTextareaEl: () => ({ isConnected: connected, focus: () => { focused = true } }),
  })
  const pending = focus()
  connected = false
  resolveTick()
  await pending
  assert.equal(focused, false)
})

test('teardown blurs the active textarea before it is detached', () => {
  const code = source.slice(source.indexOf('onBeforeUnmount(() =>'), source.indexOf('onUnmounted(() =>'))
  let teardown, blurred = false
  const textarea = { isConnected: true, blur: () => { blurred = true } }
  vm.runInNewContext(code, {
    onBeforeUnmount: fn => { teardown = fn }, getTextareaEl: () => textarea,
    document: { activeElement: textarea },
  })
  teardown()
  assert.equal(blurred, true)
})

test('new-session focus survives consumption of the first query and runs on child mount', () => {
  const page = readFileSync(new URL('../views/chat/index.vue', import.meta.url), 'utf8')
  const snapshot = page.match(/const focusComposerOnMount = [^;]+;/)[0]
  const firstQuery = { value: 'initial question' }
  const autoFocus = vm.runInNewContext(`${snapshot}\nfirstQuery.value = ''; focusComposerOnMount`, { firstQuery })
  assert.equal(autoFocus, true)
  assert.match(page, /:auto-focus="focusComposerOnMount"/)
  const mountStart = source.slice(source.indexOf('onMounted(() => {'), source.indexOf('// Embed 渠道'))
  let mounted, focused = false
  vm.runInNewContext(`${mountStart}\n});`, {
    props: { autoFocus }, onMounted: fn => { mounted = fn }, focusInput: () => { focused = true },
  })
  assert.equal(focused, false)
  mounted()
  assert.equal(focused, true)
})

function composerHarness(overrides = {}) {
  const effects = []
  const context = {
    props: {}, sendPending: { value: false }, composerDisposed: false,
    uploadedAttachments: { value: [] }, uploadedImages: { value: [] },
    allSelectedItems: { value: [] }, selectedModelId: { value: 'model' },
    selectedAgent: { value: { config: {} } }, settingsStore: {},
    chatResources: { isLoaded: () => true },
    collectAgentNotReadyReasons: () => ({ keys: [], labels: [] }),
    attachmentUploadRef: { value: { clear: () => effects.push('attachments-cleared') } },
    emit: (...args) => effects.push(args), clearvalue: () => effects.push('cleared'),
    focusInput: async () => {}, nextTick: () => Promise.resolve(),
    URL: { revokeObjectURL: () => effects.push('revoked') },
    ...overrides,
  }
  const send = vm.runInNewContext(ts.transpile(`${sendCode}\ncreateSession`), context)
  return { send, context, effects }
}

test('concurrent sends share one readiness check and preserve the first suggestion origin', async () => {
  let resolveModels, loads = 0
  const { send, effects, context } = composerHarness({
    chatResources: { isLoaded: () => false },
    loadChatModels: () => { loads++; return new Promise(resolve => { resolveModels = resolve }) },
  })
  const options = { questionOrigin: { knowledge_base_id: 'first-kb', knowledge_id: 'first-document' } }
  const pending = send('first draft', 'after', options)
  await send('duplicate draft', 'after', { questionOrigin: { knowledge_base_id: 'second-kb' } })
  assert.equal(loads, 1)
  assert.equal(context.sendPending.value, true)
  resolveModels()
  await pending
  assert.equal(effects.filter(item => Array.isArray(item) && item[0] === 'send-msg').length, 1)
  assert.equal(effects[0][1], 'first draft')
  assert.equal(effects[0][6], options, 'the accepted send must retain its own origin options')
  assert.equal(context.sendPending.value, false)
})

test('a new-session send retains its draft, images, and attachments until navigation succeeds', async () => {
  const image = { file: {}, preview: 'blob:preview' }
  const attachment = { status: 'local', name: 'notes.pdf' }
  const { send, effects, context } = composerHarness({
    props: { preserveDraftOnSend: true },
    uploadedImages: { value: [image] }, uploadedAttachments: { value: [attachment] },
  })
  await send('draft to keep')
  assert.equal(effects.length, 1)
  assert.equal(effects[0][0], 'send-msg')
  assert.deepEqual(context.uploadedImages.value, [image])
  assert.deepEqual(context.uploadedAttachments.value, [attachment])
})

for (const staleState of ['disposed', 'changed-session', 'locked']) {
  test(`a pending send does not submit after the composer is ${staleState}`, async () => {
    let resolveModels
    const { send, effects, context } = composerHarness({
      props: { sessionId: 'original' },
      chatResources: { isLoaded: () => false },
      loadChatModels: () => new Promise(resolve => { resolveModels = resolve }),
    })
    const pending = send('old draft')
    if (staleState === 'disposed') context.composerDisposed = true
    if (staleState === 'changed-session') context.props.sessionId = 'other'
    if (staleState === 'locked') context.props.composerLocked = true
    resolveModels()
    await pending
    assert.deepEqual(effects, [])
    assert.equal(context.sendPending.value, false)
  })
}

test('a failed readiness request releases the send lock and preserves the draft', async () => {
  const { send, context, effects } = composerHarness({
    chatResources: { isLoaded: () => false },
    loadChatModels: async () => { throw new Error('offline') },
  })
  await assert.rejects(send('draft'), /offline/)
  assert.deepEqual(effects, [])
  assert.equal(context.sendPending.value, false)
  context.chatResources.isLoaded = () => true
  await send('draft')
  assert.equal(effects[0][0], 'send-msg')
})

test('repeated stop clicks issue one stop request and release the lock after failure', async () => {
  const start = source.indexOf('const handleStop = async')
  const code = source.slice(start, source.indexOf('onBeforeRouteUpdate', start))
  let rejectStop, requests = 0
  const effects = [], stopPending = { value: false }
  const stop = vm.runInNewContext(ts.transpile(`${code}\nhandleStop`), {
    props: { sessionId: 'session', assistantMessageId: 'message' }, stopPending, composerDisposed: false,
    emit: event => effects.push(event),
    stopSession: () => { requests++; return new Promise((resolve, reject) => { rejectStop = reject }) },
    MessagePlugin: { error() {}, success() {} }, t: key => key, console: { log() {}, error() {} },
  })
  const pending = stop()
  await stop()
  assert.equal(requests, 1)
  rejectStop(new Error('offline'))
  await pending
  assert.deepEqual(effects, ['stop-generation', 'stop-failed'])
  assert.equal(stopPending.value, false)
})

test('a late stop response cannot clear the queue of a different session', async () => {
  const start = source.indexOf('const handleStop = async')
  const code = source.slice(start, source.indexOf('onBeforeRouteUpdate', start))
  let resolveStop
  const effects = [], stopPending = { value: false }
  const props = { sessionId: 'original', assistantMessageId: 'original-message' }
  const stop = vm.runInNewContext(ts.transpile(`${code}\nhandleStop`), {
    props, stopPending, composerDisposed: false,
    emit: event => effects.push(event),
    stopSession: () => new Promise(resolve => { resolveStop = resolve }),
    MessagePlugin: { success: () => effects.push('success') }, t: key => key, console: { log() {}, error() {} },
  })
  const pending = stop()
  props.sessionId = 'other-session'
  props.assistantMessageId = 'other-message'
  resolveStop()
  await pending
  assert.deepEqual(effects, ['stop-generation'])
  assert.equal(stopPending.value, false)
})

test('attachment removal cannot delete a file while its send is pending', () => {
  const uploadSource = readFileSync(new URL('./AttachmentUpload.vue', import.meta.url), 'utf8')
  const start = uploadSource.indexOf('const removeAttachment =')
  const code = uploadSource.slice(start, uploadSource.indexOf('const formatFileSize', start))
  const attachment = { id: 'file' }, attachments = { value: [attachment] }
  const remove = vm.runInNewContext(ts.transpile(`${code}\nremoveAttachment`), {
    props: { disabled: true }, attachments,
    emit: () => { throw new Error('a disabled control must not emit a removal') },
  })
  remove('file')
  assert.deepEqual(attachments.value, [attachment])
})
