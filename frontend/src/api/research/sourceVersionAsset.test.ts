import assert from 'node:assert/strict'
import test from 'node:test'
import { sourceVersionAssetUrl } from './sourceVersionAsset'

test('all archived assets use the knowledge and version permission boundary', () => {
  for (const kind of ['original', 'snapshot', 'markdown'] as const) {
    assert.equal(sourceVersionAssetUrl('kb-1', 'source-2', 'version-3', kind),
      `/api/v1/knowledgebase/kb-1/research/sources/source-2/versions/version-3/assets/${kind}`)
  }
})
test('asset requests encode identity segments without accepting storage paths', () => {
  const url = sourceVersionAssetUrl('kb/1', 'source?2', 'version#3', 'original')
  assert.equal(url, '/api/v1/knowledgebase/kb%2F1/research/sources/source%3F2/versions/version%233/assets/original')
  assert.equal(url.includes('file_path='), false)
})
