import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse as parseTemplate } from '@vue/compiler-dom'
import ts from 'typescript'

const source = readFileSync(new URL('./ResearchDraftEditor.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const ast = ts.createSourceFile('editor.ts', script, ts.ScriptTarget.Latest, true)
const functions = ast.statements.filter(statement => ts.isFunctionDeclaration(statement)
  && ['closeSource', 'closeDraftDialog', 'closeDraftDialogOnEscape'].includes(statement.name?.text || ''))
const compiled = ts.transpileModule(`${functions.map(statement => statement.getText(ast)).join('\n')}\nglobalThis.close = closeDraftDialog; globalThis.escape = closeDraftDialogOnEscape`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText

function dialogState() {
  const state: any = { busy: { value: '' }, sourceTarget: { value: null }, proposal: { value: null }, createVisible: { value: false }, showRevisions: { value: false }, sourceRequest: 0 }
  runInNewContext(compiled, state)
  return state
}

function escapeEvent(overrides = {}) {
  return { key: 'Escape', code: '', defaultPrevented: false, stopped: false,
    preventDefault() { this.defaultPrevented = true }, stopPropagation() { this.stopped = true }, ...overrides }
}

test('all four draft dialogs expose labeled button close controls and the actual Escape prop', () => {
  const template = source.slice(source.indexOf('<template>') + 10, source.lastIndexOf('</template>'))
  const root = parseTemplate(template)
  const dialogs: any[] = []
  const walk = (node: any) => { if (node.tag === 't-dialog') dialogs.push(node); (node.children || []).forEach(walk) }
  walk(root)
  assert.equal(dialogs.length, 4)
  for (const dialog of dialogs) {
    assert.ok(dialog.props.some((prop: any) => prop.name === 'bind' && prop.arg?.content === 'close-on-esc-keydown' && prop.exp?.content === '!busy'))
    assert.ok(!dialog.props.some((prop: any) => prop.arg?.content === 'cancel-on-esc-key'))
    const slot = dialog.children.find((child: any) => child.tag === 'template' && child.props.some((prop: any) => prop.name === 'slot' && prop.arg?.content === 'closeBtn'))
    const button = slot?.children.find((child: any) => child.tag === 't-button')
    assert.ok(button, 'close slot must use a keyboard-operable button')
    assert.match(button.props.find((prop: any) => prop.name === 'aria-label')?.value?.content || '', /^关闭/)
    assert.ok(button.props.some((prop: any) => prop.name === 'on' && prop.arg?.content === 'click' && prop.exp?.content.startsWith('closeDraftDialog(')))
  }
})

test('Escape with key but no code closes revision history without modifying the document', () => {
  const state = dialogState(); state.showRevisions.value = true
  const event = escapeEvent()
  state.escape(event)
  assert.equal(state.showRevisions.value, false)
  assert.equal(event.defaultPrevented, true)
})

test('close buttons dismiss each dialog and invalidate an in-flight source preview', () => {
  const state = dialogState()
  for (const [dialog, property, value] of [['source', 'sourceTarget', {}], ['proposal', 'proposal', {}], ['create', 'createVisible', true], ['revisions', 'showRevisions', true]] as const) {
    state[property].value = value
    state.close(dialog)
    assert.ok(!state[property].value)
  }
  assert.equal(state.sourceRequest, 1)
})

test('busy mutations and Escape already handled by a nested popup keep the dialog open', () => {
  const state = dialogState(); state.showRevisions.value = true; state.busy.value = 'revert'
  state.close('revisions'); state.escape(escapeEvent())
  assert.equal(state.showRevisions.value, true)
  state.busy.value = ''; state.escape(escapeEvent({ defaultPrevented: true }))
  assert.equal(state.showRevisions.value, true)
})

test('Escape is left untouched when no draft dialog is open', () => {
  const state = dialogState(); const event = escapeEvent()
  state.escape(event)
  assert.equal(event.defaultPrevented, false)
  assert.equal(event.stopped, false)
})

const experienceSource = readFileSync(new URL('./WritingExperiencePanel.vue', import.meta.url), 'utf8')
const experienceScript = experienceSource.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const experienceAst = ts.createSourceFile('experience.ts', experienceScript, ts.ScriptTarget.Latest, true)
const experienceFunctions = ['topExperienceDialog', 'writingFormSnapshot', 'closeExperienceDialog', 'closeExperienceDialogOnEscape', 'edit']
const experienceComputed = ['closeLocked', 'formDirty', 'feedbackDirty']
const experienceStatements = experienceAst.statements.filter(statement =>
  (ts.isFunctionDeclaration(statement) && experienceFunctions.includes(statement.name?.text || '')) ||
  (ts.isVariableStatement(statement) && statement.declarationList.declarations.some(declaration => experienceComputed.includes(declaration.name.getText(experienceAst)))))
const experienceCompiled = ts.transpileModule(`${experienceStatements.map(statement => statement.getText(experienceAst)).join('\n')}\nglobalThis.close = closeExperienceDialog; globalThis.escape = closeExperienceDialogOnEscape; globalThis.edit = edit`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText

function experienceState() {
  const state: any = {
    busy: { value: false }, props: { disabled: false, templateId: 'brief' }, visible: { value: true },
    sourceEvent: { value: null }, sourceProposal: { value: null }, formVisible: { value: false },
    editing: { value: null }, name: { value: '' }, instruction: { value: '' }, template: { value: '' },
    formBaseline: { value: '' }, feedback: { value: [] }, feedbackReasons: { value: {} },
    confirmResult: false, confirmations: [],
    computed: (getter: () => unknown) => ({ get value() { return getter() } }),
  }
  state.window = { confirm: (message: string) => { state.confirmations.push(message); return state.confirmResult } }
  runInNewContext(experienceCompiled, state)
  return state
}
function experienceEscape(overrides = {}) {
  return escapeEvent({ stopImmediatePropagation(this: { stopped: boolean }) { this.stopped = true }, ...overrides })
}

test('writing experience and both nested dialogs have named close buttons and guarded visibility', () => {
  const root = parseTemplate(experienceSource.slice(experienceSource.indexOf('<template>') + 10, experienceSource.lastIndexOf('</template>')))
  const dialogs: any[] = []
  const walk = (node: any) => { if (node.tag === 't-dialog') dialogs.push(node); (node.children || []).forEach(walk) }
  walk(root)
  assert.equal(dialogs.length, 3)
  for (const dialog of dialogs) {
    assert.ok(dialog.props.some((prop: any) => prop.name === 'bind' && prop.arg?.content === 'close-on-esc-keydown'))
    assert.ok(!dialog.props.some((prop: any) => prop.arg?.content === 'cancel-on-esc-key' || prop.name === 'model'))
    assert.ok(dialog.props.some((prop: any) => prop.name === 'on' && prop.arg?.content === 'close' && prop.exp?.content.startsWith('closeExperienceDialog(')))
    const slot = dialog.children.find((child: any) => child.tag === 'template' && child.props.some((prop: any) => prop.name === 'slot' && prop.arg?.content === 'closeBtn'))
    const button = slot?.children.find((child: any) => child.tag === 't-button')
    assert.match(button?.props.find((prop: any) => prop.name === 'aria-label')?.value?.content || '', /^关闭/)
    assert.ok(button.props.some((prop: any) => prop.arg?.content === 'disabled' && prop.exp?.content === 'closeLocked'))
  }
})

test('key-only Escape closes writing experience and consumes the event', () => {
  const state = experienceState(); const event = experienceEscape()
  state.escape(event)
  assert.equal(state.visible.value, false)
  assert.equal(event.defaultPrevented, true)
  assert.equal(event.stopped, true)
})

test('one Escape closes only the innermost source dialog, including duplicate close callbacks', () => {
  const state = experienceState(); state.sourceEvent.value = {}; state.edit()
  const event = experienceEscape(); state.escape(event)
  state.close('form', event); state.close('manager', event)
  assert.equal(state.sourceEvent.value, null)
  assert.equal(state.formVisible.value, true)
  assert.equal(state.visible.value, true)
  assert.equal(state.confirmations.length, 0)
})

test('canceling dirty form dismissal retains inputs; confirming closes only that form', () => {
  const state = experienceState(); state.edit(); state.name.value = '未保存名称'; state.instruction.value = '未保存要求'
  state.escape(experienceEscape())
  assert.equal(state.formVisible.value, true)
  assert.equal(state.name.value, '未保存名称')
  assert.equal(state.instruction.value, '未保存要求')
  assert.equal(state.confirmations.length, 1)
  state.confirmResult = true; state.escape(experienceEscape())
  assert.equal(state.formVisible.value, false)
  assert.equal(state.visible.value, true)
})

test('unsubmitted feedback is protected but saved feedback does not prompt on close', () => {
  const state = experienceState(); state.feedback.value = [{ rule_id: 'rule', is_mine: true, reason: '已保存' }]
  state.feedbackReasons.value = { rule: '未提交' }
  state.close('manager')
  assert.equal(state.visible.value, true)
  assert.equal(state.feedbackReasons.value.rule, '未提交')
  state.feedbackReasons.value.rule = '已保存'; state.close('manager')
  assert.equal(state.visible.value, false)
  assert.equal(state.confirmations.length, 1)
})

test('pending writes and parent mutations block buttons, cancellation, and Escape without closing outer layers', () => {
  for (const lock of ['busy', 'parent'] as const) {
    const state = experienceState(); state.edit()
    if (lock === 'busy') state.busy.value = true
    else state.props.disabled = true
    state.close('form')
    const event = experienceEscape(); state.escape(event)
    assert.equal(state.formVisible.value, true)
    assert.equal(state.visible.value, true)
    assert.equal(event.defaultPrevented, true)
    assert.equal(state.confirmations.length, 0)
  }
})

test('nested popup consumption and absence of dialogs leave Escape unchanged', () => {
  const state = experienceState(); state.escape(experienceEscape({ defaultPrevented: true }))
  assert.equal(state.visible.value, true)
  state.visible.value = false
  const event = experienceEscape(); state.escape(event)
  assert.equal(event.defaultPrevented, false)
  assert.equal(event.stopped, false)
})
