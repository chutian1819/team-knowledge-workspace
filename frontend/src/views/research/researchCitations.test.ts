import assert from 'node:assert/strict'
import test from 'node:test'
import { localizeResearchCitations, parseResearchCitation, researchSourceRefs } from './researchCitations'
test('native evidence links become inert anchors then resolve original document and chunk', () => {
  assert.equal(localizeResearchCitations('[paper](knowledge://abc-123/chunks/c-1)'), '[paper](#research-source/abc-123/c-1)')
  assert.deepEqual(parseResearchCitation('#research-source/abc-123/c-1'), { knowledgeId: 'abc-123', chunkId: 'c-1' })
  assert.equal(parseResearchCitation('javascript:alert(1)'), null)
  assert.equal(parseResearchCitation('#research-source/../../admin'), null)
})
test('native ID|Title references retain title and cannot inject paths', () => {
  assert.deepEqual(researchSourceRefs(['abc-1|Paper | Study', '../secrets|Bad']), [{ id: 'abc-1', title: 'Paper | Study' }])
})
