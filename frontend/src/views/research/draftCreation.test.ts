import assert from 'node:assert/strict'
import test from 'node:test'
import { defaultDraftSourceScope, canCreateWithScope, draftCreationPayload } from './draftCreation'

test('no selected evidence defaults to a template that can be created without a model', () => {
  const scope = defaultDraftSourceScope([])
  assert.equal(scope, 'template')
  assert.equal(canCreateWithScope(scope, []), true)
  assert.deepEqual(draftCreationPayload('  项目一页纸  ', 'one-pager', scope, [], 'project'), {
    title: '项目一页纸', template_id: 'one-pager', source_scope: '', source_ids: [],
  })
})

test('explicit template mode never includes selected files or the whole project', () => {
  assert.deepEqual(draftCreationPayload('人工起稿', 'brief', 'template', ['private-doc'], 'private-project'), {
    title: '人工起稿', template_id: 'brief', source_scope: '', source_ids: [],
  })
})

test('selected evidence still defaults to selected mode and cannot silently become template or whole-project', () => {
  assert.equal(defaultDraftSourceScope(['doc-1']), 'selected')
  assert.equal(canCreateWithScope('selected', []), false)
  assert.throws(() => draftCreationPayload('标题', 'brief', 'selected', [], 'project'), /勾选资料/)
  assert.deepEqual(draftCreationPayload('标题', 'brief', 'selected', ['doc-1'], 'project'), {
    title: '标题', template_id: 'brief', source_scope: 'selected', source_ids: ['doc-1'], source_kb_id: 'project',
  })
})

test('whole-project scope remains an explicit request with its project identity', () => {
  assert.equal(canCreateWithScope('all', []), true)
  assert.deepEqual(draftCreationPayload('对比', 'comparison', 'all', ['doc-1'], 'project'), {
    title: '对比', template_id: 'comparison', source_scope: 'all', source_ids: [], source_kb_id: 'project',
  })
})

test('semantic creation sends only a question and scope, never client evidence or an implicit full read', () => {
  assert.deepEqual(draftCreationPayload('检索稿', 'brief', 'retrieval', ['previous-selection'], 'project', '  迁移能力的证据？  '), {
    title: '检索稿', template_id: 'brief', source_scope: 'retrieval', source_ids: [], source_kb_id: 'project', retrieval_kb_id: 'project', retrieval_query: '迁移能力的证据？',
  })
  assert.throws(() => draftCreationPayload('标题', '', 'retrieval', [], 'project'), /检索问题/)
  assert.throws(() => draftCreationPayload('标题', '', 'retrieval', [], '', '问题'), /知识库/)
})
