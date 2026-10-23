import assert from 'node:assert/strict'
import test from 'node:test'
import { displayAgentDescription } from './agentDescription.ts'

test('known built-ins use product copy in the current interface language', () => {
  const agent = { id: 'builtin-quick-answer', is_builtin: true, description: 'Legacy copy' }
  assert.equal(displayAgentDescription(agent, key => `zh:${key}`), 'zh:agent.builtinDescriptions.quickAnswer')
  assert.equal(displayAgentDescription(agent, key => `en:${key}`), 'en:agent.builtinDescriptions.quickAnswer')
})

test('member descriptions, unknown built-ins and missing agents are preserved', () => {
  const translate = () => 'Replacement'
  assert.equal(displayAgentDescription({ id: 'builtin-quick-answer', is_builtin: false, description: 'Member text' }, translate), 'Member text')
  assert.equal(displayAgentDescription({ id: 'custom-built-in', is_builtin: true, description: 'Custom deployment' }, translate), 'Custom deployment')
  assert.equal(displayAgentDescription(null, translate), '')
})
