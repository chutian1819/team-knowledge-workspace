import type { WritingRule } from '@/api/research'

/** A selection is explicit, bounded, and valid only for this document type. */
export function applicableWritingRules(rules: WritingRule[], templateId: string): WritingRule[] {
  return rules.filter(rule => rule.status === 'active' && (!rule.template_id || rule.template_id === templateId))
}

export function writingRuleSelection(ids: string[], rules: WritingRule[], templateId: string): string[] {
  const allowed = new Set(applicableWritingRules(rules, templateId).map(rule => rule.id))
  return [...new Set(ids)].filter(id => allowed.has(id)).slice(0, 5)
}
