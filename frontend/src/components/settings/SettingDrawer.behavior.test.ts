import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { fileURLToPath } from 'node:url'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'

const filename = fileURLToPath(new URL('./SettingDrawer.vue', import.meta.url))
const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
const script = compileScript(descriptor, { id: 'drawer-behavior' }).content
  .replace('__expose();', '')
  .replace('return __returned__', '__expose(__returned__); return __returned__')
const compiled = ts.transpileModule(script, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText

class FakeElement {
  isConnected = true
  focused = false
  focusCalls = 0
  disabled = false
  visibility = 'visible'
  display = 'block'
  hasLayout = true
  inert = false
  drawer: FakeElement | null = null
  style: Record<string, string> = {}
  constructor(readonly doc: { activeElement: FakeElement | null; body: FakeElement | null }) {}
  focus() {
    ++this.focusCalls
    if (!this.isConnected || this.disabled || this.visibility !== 'visible' || this.display === 'none' || !this.hasLayout || this.inert) return
    this.focused = true
    this.doc.activeElement = this
  }
  blur() { if (this.doc.activeElement === this) this.doc.activeElement = this.doc.body }
  getClientRects() { return this.hasLayout && this.display !== 'none' ? [{}] : [] }
  closest(selector: string) {
    if (selector === '.t-drawer') return this.drawer
    return this.inert ? this : this.drawer?.inert ? this.drawer : null
  }
}

const cleanup: Array<() => void> = []
test.afterEach(() => { while (cleanup.length) cleanup.pop()!() })

function fixture() {
  const emissions: unknown[][] = []
  const props = vue.reactive({ visible: false, title: 'Test', width: '560px', minWidth: 480, maxWidth: 1200,
    storageKey: '', confirmLoading: false, confirmDisabled: false, cancelDisabled: false })
  const exports: any = {}
  let exposed: any
  const document = {
    activeElement: null as FakeElement | null,
    body: null as FakeElement | null,
    addEventListener() {}, removeEventListener() {},
  }
  const element = () => vue.markRaw(new FakeElement(document))
  const trigger = element()
  const drawer = element()
  document.body = element()
  document.activeElement = trigger
  const frames = new Map<number, FrameRequestCallback>()
  let frameId = 0
  const unmountCallbacks: Array<() => void> = []
  const scope = vue.effectScope()
  runInNewContext(compiled, {
    exports, console, HTMLElement: FakeElement, document,
    window: {
      innerWidth: 390, localStorage: { getItem: () => null, setItem() {} },
      addEventListener() {}, removeEventListener() {},
      requestAnimationFrame(callback: FrameRequestCallback) { frames.set(++frameId, callback); return frameId },
      cancelAnimationFrame(id: number) { frames.delete(id) },
      getComputedStyle: (target: FakeElement) => ({ visibility: target.visibility, display: target.display }),
    },
    require(name: string) {
      if (name === 'vue') return { ...vue, provide() {}, useId: () => 'test', useAttrs: () => ({}),
        onMounted() {}, onUnmounted: (callback: () => void) => unmountCallbacks.push(callback) }
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
      throw new Error(name)
    },
  })
  scope.run(() => exports.default.setup(props, {
    emit: (...args: unknown[]) => emissions.push(args), expose: (value: any) => { exposed = value },
  }))
  let unmounted = false
  const unmount = () => {
    if (unmounted) return
    unmounted = true
    scope.stop()
    unmountCallbacks.forEach(callback => callback())
  }
  cleanup.push(unmount)
  return {
    props, emissions, exposed, trigger, document, drawer, frames, unmount,
    content() { const target = element(); target.drawer = drawer; return target },
    outside: element,
    frame() {
      const pending = Array.from(frames.values())
      frames.clear()
      pending.forEach(callback => callback(0))
    },
  }
}
const flush = async () => { await vue.nextTick(); await vue.nextTick() }

test('hidden initialization preserves page focus and closing only blurs its own controls', () => {
  const f = fixture()
  f.exposed.blurActiveElementBeforeClose()
  assert.equal(f.document.activeElement, f.trigger)
  f.exposed.bodyElement.value = f.content()
  f.exposed.blurActiveElementBeforeClose()
  assert.equal(f.document.activeElement, f.trigger)
  const input = f.content()
  input.focus()
  f.exposed.blurActiveElementBeforeClose()
  assert.equal(f.document.activeElement, f.document.body)
})

test('saving prevents footer, close-button and overlay cancellation', async () => {
  const f = fixture()
  f.props.confirmLoading = true
  f.exposed.handleCancel()
  f.exposed.drawerVisible.value = false
  f.exposed.handleConfirm()
  assert.deepEqual(f.emissions, [])
  f.props.confirmLoading = false
  f.exposed.handleCancel()
  assert.deepEqual(f.emissions, [['cancel'], ['update:visible', false]])
  await flush()
})

test('explicit cancellation and validation locks are respected independently', async () => {
  const f = fixture()
  f.props.cancelDisabled = true
  f.exposed.handleCancel()
  f.exposed.drawerVisible.value = false
  f.props.confirmDisabled = true
  f.exposed.handleConfirm()
  assert.deepEqual(f.emissions, [])
  f.props.confirmDisabled = false
  f.exposed.handleConfirm()
  assert.deepEqual(f.emissions, [['confirm']])
  await flush()
})

test('opening focuses visible content and closing restores its trigger at phone width', async () => {
  const f = fixture()
  const close = f.content()
  f.exposed.closeButton.value = close
  f.props.visible = true
  await flush()
  f.frame()
  assert.equal(f.document.activeElement, close)
  assert.equal(f.exposed.effectiveWidth.value, '390px')
  f.props.visible = false
  await flush()
  assert.equal(f.document.activeElement, f.trigger)
  assert.equal(f.frames.size, 0)
})

test('first opening waits for teleported refs and their actual CSS visibility', async () => {
  const f = fixture()
  f.props.visible = true
  await flush()
  f.frame()
  const close = f.content()
  close.visibility = 'hidden'
  f.exposed.closeButton.value = close
  await flush()
  f.frame()
  assert.equal(close.focusCalls, 0, 'connected elements with layout can still be visibility:hidden')
  assert.equal(f.document.activeElement, f.trigger)
  assert.equal(f.frames.size, 1)
  close.visibility = 'visible'
  f.frame()
  assert.equal(f.document.activeElement, close)
  assert.equal(f.frames.size, 0)
})

test('a target without layout or still inert is not focused', async () => {
  const f = fixture()
  const close = f.content()
  close.hasLayout = false
  f.exposed.closeButton.value = close
  f.props.visible = true
  await flush()
  f.frame()
  assert.equal(close.focusCalls, 0)
  close.hasLayout = true
  f.drawer.inert = true
  f.frame()
  assert.equal(close.focusCalls, 0)
  f.drawer.inert = false
  f.frame()
  assert.equal(f.document.activeElement, close)
})

test('closing cancels pending focus even if an old frame callback is delivered later', async () => {
  const f = fixture()
  const close = f.content()
  close.visibility = 'hidden'
  f.exposed.closeButton.value = close
  f.props.visible = true
  await flush()
  f.frame()
  const staleFrame = Array.from(f.frames.values())[0]!
  f.props.visible = false
  await flush()
  assert.equal(f.frames.size, 0)
  close.visibility = 'visible'
  staleFrame(0)
  assert.equal(close.focusCalls, 0)
  assert.equal(f.document.activeElement, f.trigger)
})

test('unmount cancels the pending opening task', async () => {
  const f = fixture()
  const close = f.content()
  close.visibility = 'hidden'
  f.exposed.closeButton.value = close
  f.props.visible = true
  await flush()
  const staleFrame = Array.from(f.frames.values())[0]!
  f.unmount()
  assert.equal(f.frames.size, 0)
  close.visibility = 'visible'
  staleFrame(0)
  assert.equal(close.focusCalls, 0)
  assert.equal(f.document.activeElement, f.trigger)
})

test('opening never steals focus already moved to another popup or drawer control', async () => {
  for (const outside of [true, false]) {
    const f = fixture()
    const close = f.content()
    close.visibility = 'hidden'
    f.exposed.closeButton.value = close
    f.props.visible = true
    await flush()
    f.frame()
    const chosen = outside ? f.outside() : f.content()
    chosen.focus()
    close.visibility = 'visible'
    f.frame()
    assert.equal(close.focusCalls, 0)
    assert.equal(f.document.activeElement, chosen)
    assert.equal(f.frames.size, 0)
    // Late-mounted content must not restart a cancelled initial focus task.
    f.exposed.bodyElement.value = f.content()
    await flush()
    assert.equal(f.frames.size, 0)
    assert.equal(f.document.activeElement, chosen)
  }
})

test('a disabled close button falls back to the focusable drawer body', async () => {
  const f = fixture()
  const close = f.content()
  close.disabled = true
  const body = f.content()
  f.exposed.closeButton.value = close
  f.exposed.bodyElement.value = body
  f.props.visible = true
  await flush()
  f.frame()
  assert.equal(close.focusCalls, 0)
  assert.equal(f.document.activeElement, body)
})

test('a stale opening callback cannot replace or cancel the next opening task', async () => {
  const f = fixture()
  const close = f.content()
  close.visibility = 'hidden'
  f.exposed.closeButton.value = close
  f.props.visible = true
  await flush()
  const staleFrame = Array.from(f.frames.values())[0]!
  f.props.visible = false
  await flush()
  f.props.visible = true
  await flush()
  assert.equal(f.frames.size, 1)
  staleFrame(0)
  assert.equal(close.focusCalls, 0)
  assert.equal(f.frames.size, 1)
  close.visibility = 'visible'
  f.frame()
  assert.equal(f.document.activeElement, close)
  assert.equal(f.frames.size, 0)
})
