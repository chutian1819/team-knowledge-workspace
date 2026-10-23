import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { resolveWorkspaceLocation, sectionSourceBases } from './workspaceNavigation'

// Run the actual component transitions so a correct helper cannot mask stale UI wiring.
const source = readFileSync(new URL('./ResearchWorkspace.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const ast = ts.createSourceFile('ResearchWorkspace.ts', script, ts.ScriptTarget.Latest, true)
const transitions = ast.statements.filter(statement => ts.isFunctionDeclaration(statement)
  && ['changeSection', 'chooseProject', 'syncLocation'].includes(statement.name?.text || ''))
const guard = ast.statements.find(statement => ts.isExpressionStatement(statement)
  && ts.isCallExpression(statement.expression) && statement.expression.expression.getText(ast) === 'onBeforeRouteUpdate')
assert.equal(transitions.length, 3)
assert.ok(guard)
const compiled = ts.transpileModule(`${transitions.map(statement => statement.getText(ast)).join('\n')}\n${guard.getText(ast)}\nglobalThis.transitions = { changeSection, chooseProject }`, {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText

function workspace(
  own = [{ id: 'inbox', name: '我的收件箱' }, { id: 'research', name: '具身智能' }],
  shared = [{ organization_id: 'alpha', knowledge_base: { id: 'team-a' } }],
) {
  const state: Record<string, any> = {
    inboxId: { value: 'inbox' }, busy: { value: '' }, draftEditor: { value: null }, initialSectionResolved: true,
    section: { value: 'library' }, kbId: { value: 'inbox' }, selectedProject: { value: '' },
    lastSectionKb: new Map(), knowledgeBases: { value: own }, sharedBases: { value: shared },
    route: { name: 'researchWorkspace', query: { section: 'library', kb: 'inbox' } },
    window: { confirm: () => true }, resolveWorkspaceLocation, routeUpdates: [],
  }
  state.sectionBases = { get value() { return sectionSourceBases(state.section.value, own, state.sharedBases.value, state.selectedProject.value, state.inboxId.value) } }
  state.router = { replace: (location: any) => { state.routeUpdates.push(location); return Promise.resolve() } }
  state.onBeforeRouteUpdate = (handler: any) => { state.guard = handler }
  runInNewContext(compiled, state)
  return state
}

test('switching to the team persists its section and restores the last personal collection on return', () => {
  const state = workspace()
  state.kbId.value = 'research'
  state.transitions.changeSection('team')
  assert.equal(state.section.value, 'team')
  assert.equal(state.kbId.value, 'team-a')
  assert.equal(state.routeUpdates.at(-1).query.section, 'team')
  assert.equal(state.routeUpdates.at(-1).query.kb, 'team-a')
  state.transitions.changeSection('library')
  assert.equal(state.kbId.value, 'research')
})

test('a single renamed demo stays available when returning from an empty team section', () => {
  const state = workspace([{ id: 'inbox', name: '演示知识库' }], [])
  state.transitions.changeSection('team')
  assert.equal(state.kbId.value, '')
  assert.equal(state.routeUpdates.at(-1).query.kb, undefined)
  state.transitions.changeSection('library')
  assert.equal(state.kbId.value, 'inbox')
})

test('declining to discard a draft preserves the current section and collection', () => {
  const state = workspace()
  state.draftEditor.value = { dirty: true }
  state.window.confirm = () => false
  state.transitions.changeSection('team')
  assert.equal(state.section.value, 'library')
  assert.equal(state.kbId.value, 'inbox')
  assert.equal(state.routeUpdates.length, 0)
  assert.equal(state.guard({ query: { section: 'library', kb: 'research' } }), false)
})

test('busy operations block external collection changes but allow the current URL to normalize', () => {
  const state = workspace()
  state.busy.value = 'capture'
  state.transitions.changeSection('team')
  assert.equal(state.kbId.value, 'inbox')
  assert.equal(state.guard({ query: { section: 'library', kb: 'research' } }), false)
  assert.equal(state.guard({ query: { section: 'inbox', kb: 'inbox' } }), true)
})
