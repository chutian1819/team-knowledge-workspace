import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { computed, ref } from 'vue'

const source = readFileSync(new URL('./KnowledgeBase.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const ast = ts.createSourceFile('KnowledgeBase.ts', script, ts.ScriptTarget.Latest, true)
const declaration = ast.statements.find(statement => ts.isVariableStatement(statement)
  && statement.declarationList.declarations.some(node => ts.isIdentifier(node.name) && node.name.text === 'showKbDetailContextualGuide'))
assert.ok(declaration)
const compiled = ts.transpileModule(declaration.getText(ast), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText

function guideVisible(overrides: Record<string, unknown> = {}) {
  const state = {
    kbId: 'kb-a', kbInfo: { id: 'kb-a' }, kbLoading: false,
    isFAQ: false, canEdit: true, docListLoading: false,
    shownKbTab: 'documents', isFiltering: false, selectedFolderPath: '',
    hasFolders: false, cardList: [], ...overrides,
  }
  return runInNewContext(`${compiled}\nshowKbDetailContextualGuide.value`, {
    computed, ...Object.fromEntries(Object.entries(state).map(([key, value]) => [key, ref(value)])),
  })
}

test('a loaded empty editable knowledge base can show the first-document guide', () => {
  assert.equal(guideVisible(), true)
})

test('no search results must not open a blocking first-document guide', () => {
  assert.equal(guideVisible({ isFiltering: true }), false)
  assert.equal(guideVisible({ selectedFolderPath: 'Projects/Archive' }), false)
  assert.equal(guideVisible({ hasFolders: true }), false)
})

test('loading, read-only, and non-document views do not invite document creation', () => {
  for (const state of [{ kbInfo: null }, { kbLoading: true }, { docListLoading: true }, { canEdit: false }, { shownKbTab: 'gallery' }, { isFAQ: true }, { cardList: [{ id: 'document' }] }]) {
    assert.equal(guideVisible(state), false, JSON.stringify(state))
  }
})
