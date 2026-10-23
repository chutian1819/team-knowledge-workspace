const BUILTIN_DESCRIPTION_KEYS: Record<string, string> = {
  'builtin-quick-answer': 'agent.builtinDescriptions.quickAnswer',
  'builtin-smart-reasoning': 'agent.builtinDescriptions.reasoning',
  'builtin-wiki-researcher': 'agent.builtinDescriptions.wiki',
  'builtin-data-analyst': 'agent.builtinDescriptions.dataAnalysis',
}

/** Product copy for built-ins; descriptions written by members stay unchanged. */
export function displayAgentDescription(
  agent: { id?: string; is_builtin?: boolean; description?: string } | null | undefined,
  translate: (key: string) => string,
): string {
  const key = agent?.is_builtin && agent.id ? BUILTIN_DESCRIPTION_KEYS[agent.id] : undefined
  return key ? translate(key) : agent?.description || ''
}
