import assert from 'node:assert/strict'
import test from 'node:test'
import { sourceState, quickNotePublishingReady, collectionSearchEnabled, exactMemberIds, chatSourceScope, quickNotePayload, sourceVersionFilename, terminalJob, isCurrentAudience, completedShareSince } from './workspaceState'

test('saved and queued ingestion never claims searchable completion', () => {
  assert.equal(sourceState({ id: 'a', parse_status: 'pending' }).label, '已保存 · 排队中')
  assert.equal(sourceState({ id: 'a', parse_status: 'completed' }).label, '可检索')
  assert.equal(sourceState({ id: 'a', parse_status: 'failed' }).tone, 'danger')
})
test('plain source storage can publish notes without embedding or summary models', () => {
  const kb = { indexing_strategy: { vector_enabled: false, keyword_enabled: false, wiki_enabled: false } }
  assert.equal(quickNotePublishingReady(kb), true)
  assert.equal(quickNotePayload('具身智能', '材料正文', quickNotePublishingReady(kb)).status, 'publish')
  assert.equal(collectionSearchEnabled(kb), false)
  assert.deepEqual(sourceState({ id: 'source', parse_status: 'completed' }, collectionSearchEnabled(kb)), { label: '已整理', tone: 'success' })
})
test('enabled vector or keyword pipelines still require their configured embedder', () => {
  for (const indexing_strategy of [{ vector_enabled: true }, { keyword_enabled: true }]) {
    assert.equal(quickNotePublishingReady({ indexing_strategy }), false)
    assert.equal(quickNotePublishingReady({ indexing_strategy, embedding_model_id: 'embedding' }), true)
  }
})
test('legacy collections retain embedding requirements and Wiki-only sources remain writable', () => {
  assert.equal(quickNotePublishingReady({}), false)
  assert.equal(quickNotePublishingReady({ embedding_model_id: 'embedding' }), true)
  assert.equal(quickNotePublishingReady({ indexing_strategy: { wiki_enabled: true } }), true)
  assert.equal(collectionSearchEnabled({ indexing_strategy: { wiki_enabled: true } }), true)
  assert.equal(quickNotePublishingReady(undefined), false)
})
test('configured quick notes use the native publish status and preserve Markdown', () => {
  const content = '  # 与创始人交流\n\n- 待验证：收入数据  \n'
  assert.deepEqual(quickNotePayload('  交流记录  ', content, true), {
    title: '交流记录', content, status: 'publish',
  })
})
test('quick notes without a model save as draft and never claim processing', () => {
  const content = '# 先记录\n无需模型也应保留正文。'
  assert.deepEqual(quickNotePayload('记录', content, false), {
    title: '记录', content, status: 'draft',
  })
  assert.deepEqual(sourceState({ id: 'note', parse_status: 'draft' }), {
    label: '已保存 · 待发布', tone: 'pending',
  })
})
test('empty quick notes fail validation before a request can be submitted', () => {
  assert.throws(() => quickNotePayload(' ', '# 内容', false), /标题和正文/)
  assert.throws(() => quickNotePayload('标题', '\n ', true), /标题和正文/)
})
test('historical originals retain their native extension and distinguish versions', () => {
  const version = { id: '12345678-abcd', knowledge_id: 'doc1', content_hash: 'sha256', captured_at: '', file_name: '机构/研究.第一版.pdf', file_type: 'pdf' }
  assert.equal(sourceVersionFilename(version, 'original'), '研究.第一版-12345678.pdf')
  assert.equal(sourceVersionFilename({ ...version, file_name: '研究' }, 'original'), '研究-12345678.pdf')
  assert.equal(sourceVersionFilename({ ...version, id: '87654321-abcd' }, 'original'), '研究.第一版-87654321.pdf')
})
test('webpage snapshots remain inert text files and legacy versions need no original metadata', () => {
  const version = { id: '12345678-abcd', knowledge_id: 'doc1', content_hash: 'sha256', captured_at: '' }
  assert.equal(sourceVersionFilename(version, 'snapshot', '文章'), '文章-12345678.html.txt')
  assert.equal(sourceVersionFilename(version, 'markdown', '文章'), '文章-12345678.md')
})
test('member confirmation includes every tenant once without invalid identifiers', () => {
  assert.deepEqual(exactMemberIds([{ tenant_id: 8 }, { tenant_id: 3 }, { tenant_id: 8 }, { tenant_id: 0 }]), [3, 8])
})
test('explicit document scope cannot accidentally include entire knowledge base', () => {
  assert.deepEqual(chatSourceScope('kb1', ['doc1', 'doc1']), { knowledgeBases: [], files: ['doc1'], fileKbMap: { doc1: 'kb1' } })
  assert.deepEqual(chatSourceScope('kb1', []), { knowledgeBases: ['kb1'], files: [], fileKbMap: {} })
})
test('partial imports stop polling but remain retryable', () => {
  assert.equal(terminalJob('partial'), true)
  assert.equal(terminalJob('processing'), false)
})

test('older org member responses cannot overwrite a newly selected audience', () => {
  assert.equal(isCurrentAudience(1, 2, 'org-a', 'org-b'), false)
  assert.equal(isCurrentAudience(2, 2, 'org-b', 'org-b'), true)
})
test('finished share refreshes projects once, existing completed jobs do not loop', () => {
  const done = [{ id: 'a', kind: 'share', status: 'completed' }]
  assert.equal(completedShareSince([{ id: 'a', kind: 'share', status: 'processing' }], done), true)
  assert.equal(completedShareSince(done, done), false)
  assert.equal(completedShareSince([], done), false)
})
