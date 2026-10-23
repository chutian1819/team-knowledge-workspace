import assert from 'node:assert/strict'
import test from 'node:test'
import { applicableWritingRules, writingRuleSelection } from './writingExperience'
import type { WritingRule } from '@/api/research'

function rule(id: string, status: WritingRule['status'] = 'active', template_id = ''): WritingRule {
  return { id, status, template_id, version: 1, knowledge_base_id: 'kb', name: id, instruction: '先写判断' }
}

test('未确认、已撤销和其他文种的经验不会进入本稿选择', () => {
  const rules = [rule('general'), rule('brief', 'active', 'brief'), rule('other', 'active', 'comparison'), rule('pending', 'candidate'), rule('revoked', 'revoked')]
  assert.deepEqual(applicableWritingRules(rules, 'brief').map(item => item.id), ['general', 'brief'])
  assert.deepEqual(writingRuleSelection(['pending', 'other', 'general', 'brief', 'brief', 'revoked', 'unknown'], rules, 'brief'), ['general', 'brief'])
  assert.deepEqual(writingRuleSelection(['brief'], rules, ''), [])
})

test('刷新撤销状态后清除选择；不默认加载新经验；最多五条', () => {
  assert.deepEqual(writingRuleSelection([], [rule('new')], ''), [])
  assert.deepEqual(writingRuleSelection(['changed'], [rule('changed', 'revoked')], ''), [])
  const rules = Array.from({ length: 7 }, (_, i) => rule(`${i}`))
  assert.deepEqual(writingRuleSelection(rules.map(item => item.id), rules, ''), ['0', '1', '2', '3', '4'])
})
