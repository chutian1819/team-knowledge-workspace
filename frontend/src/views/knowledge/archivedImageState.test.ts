import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'

function variableFromComponent(file: string, name: string): string {
  const source = readFileSync(new URL(file, import.meta.url), 'utf8')
  const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
  const ast = ts.createSourceFile('component.ts', script, ts.ScriptTarget.Latest, true)
  const declaration = ast.statements.find(statement => ts.isVariableStatement(statement)
    && statement.declarationList.declarations.some(item => ts.isIdentifier(item.name) && item.name.text === name))
  assert.ok(declaration)
  return ts.transpileModule(`${declaration.getText(ast)}\nglobalThis.action = ${name}`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
}

test('native list shows archive state instead of unknown or stale processing status', () => {
  const context: any = { t: (key: string) => key, shownStall: () => 'stalled' }
  runInNewContext(variableFromComponent('./components/DocumentListView.vue', 'computeStatus'), context)
  const status = context.action({ parse_status: 'archived', stalled_minutes: 120, summary_status: 'pending' })
  assert.equal(status.label, 'knowledgeBase.statusArchived')
  assert.equal(status.hint, 'knowledgeBase.archivedImageHint')
  assert.equal(status.spin, undefined)
})

test('native image recognition explicitly clears the persisted archive-only option after confirmation', async () => {
  let confirmed: any; let submitted: any
  const context: any = {
    isFAQ: { value: false }, canEdit: { value: true }, kbInfo: { value: { id: 'kb' } },
    isParseInFlight: () => false, closeCardMoreMenu: () => {},
    getKnowledgeDetails: async () => ({ success: true, data: { metadata: { process_overrides: { image_archive_only: true } } } }),
    uploadConfirmStore: { open: async (request: any) => { confirmed = request; return { mode: 'reparse', reparse: request.reparse, processConfig: { enable_multimodel: true } } } },
    submitReparse: async (id: string, config: any) => { submitted = { id, config } },
  }
  runInNewContext(variableFromComponent('./KnowledgeBase.vue', 'confirmRebuildKnowledge'), context)
  await context.action(0, { id: 'image', parse_status: 'archived', file_name: 'robot.png' })
  assert.equal(confirmed.reparse.processOverrides.image_archive_only, false)
  assert.equal(confirmed.reparse.processOverrides.vlm_config.enabled, true)
  assert.equal(submitted.id, 'image')
  assert.equal(submitted.config.image_archive_only, false)
  assert.equal(submitted.config.enable_multimodel, true)
})
