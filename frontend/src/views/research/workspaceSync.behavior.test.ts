import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'

const source = readFileSync(new URL('./ResearchWorkspace.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const ast = ts.createSourceFile('workspace.ts', script, ts.ScriptTarget.Latest, true)
const functions = ast.statements.filter(statement => ts.isFunctionDeclaration(statement)
  && ['tick', 'loadSources'].includes(statement.name?.text || ''))
const compiled = ts.transpileModule(functions.map(statement => statement.getText(ast)).join('\n')
  + '\nglobalThis.actions = { tick, loadSources }', { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText

function workspace() {
  const state: Record<string, any> = {
    disposed: false, document: { hidden: false }, loadToken: 0, historyRequest: 0, pollTimer: undefined,
    kbId: { value: 'shared' }, section: { value: 'team' }, tab: { value: 'sources' },
    isOwn: { value: false }, busy: { value: '' }, liveJobs: { value: [] }, assetJobs: { value: [] },
    sourceList: { value: [{ id: 'file', parse_status: 'completed', title: 'Old' }] },
    selectedIds: { value: ['file'] }, sourceVersions: { value: ['old-version'] },
    historyItem: { value: { id: 'file', title: 'Old' } }, replacementFile: { value: null },
    total: { value: 1 }, page: { value: 1 }, query: { value: '' }, loading: { value: false },
    reads: 0, timers: 0, histories: [], errors: [],
    setTimeout: () => { state.timers++; return 1 },
    fail: (error: any) => state.errors.push(error),
    listKnowledgeFiles: async () => { state.reads++; return { data: [{ id: 'file', title: 'Updated' }], total: 1 } },
    sourceHistory: async (item: any) => state.histories.push(item.title),
  }
  runInNewContext(compiled, state)
  return state
}

test('idle team readers see another member’s updates and current version history', async () => {
  const state = workspace()
  await state.actions.tick()
  assert.equal(state.reads, 1)
  assert.equal(state.sourceList.value[0].title, 'Updated')
  assert.deepEqual(state.histories, ['Updated'])
  assert.equal(state.timers, 1)
})

test('hidden tabs, draft tabs, and private idle collections avoid collaboration polling', async () => {
  for (const change of [
    (s: any) => { s.document.hidden = true },
    (s: any) => { s.tab.value = 'drafts' },
    (s: any) => { s.section.value = 'library'; s.isOwn.value = true },
  ]) {
    const state = workspace(); change(state)
    await state.actions.tick()
    assert.equal(state.reads, 0)
  }
})

test('revocation clears cached rows, selection and version history on the next read', async () => {
  const state = workspace()
  state.listKnowledgeFiles = async () => { throw { $httpStatus: 403 } }
  await state.actions.tick()
  assert.equal(state.sourceList.value.length, 0)
  assert.equal(state.selectedIds.value.length, 0)
  assert.equal(state.sourceVersions.value.length, 0)
  assert.equal(state.historyItem.value, null)
  assert.equal(state.total.value, 0)
  assert.equal(state.histories.length, 0)
})

test('a slow response cannot replace another collection and unmount stops rescheduling', async () => {
  const state = workspace()
  state.listKnowledgeFiles = async () => {
    state.loadToken++; state.disposed = true
    return { data: [{ id: 'stale' }], total: 1 }
  }
  await state.actions.tick()
  assert.equal(state.sourceList.value[0].id, 'file')
  assert.equal(state.timers, 0)
})
