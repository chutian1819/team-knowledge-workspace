import assert from 'node:assert/strict'
import test from 'node:test'
import { researchModelId } from './modelSelection'

const models = [{ id: 'preserved-provider', is_default: true }, { id: 'working-provider' }]

test('reopening a research library uses its configured provider while preserving the old model', () => {
  assert.equal(researchModelId(models, { wiki_config: { synthesis_model_id: 'working-provider' } }), 'working-provider')
  assert.equal(models.length, 2)
})

test('a legacy research library can select its summary model', () => {
  assert.equal(researchModelId(models, { summary_model_id: 'working-provider' }), 'working-provider')
})

test('a shared library cannot select a model inaccessible to its caller', () => {
  assert.equal(researchModelId(models, { wiki_config: { synthesis_model_id: 'other-tenant-model' } }), 'preserved-provider')
  assert.equal(researchModelId([], { summary_model_id: 'working-provider' }), '')
})
