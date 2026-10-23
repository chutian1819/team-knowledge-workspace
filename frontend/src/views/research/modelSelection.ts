type AvailableModel = { id?: string; is_default?: boolean }
type ResearchModelSettings = {
  summary_model_id?: string
  wiki_config?: { synthesis_model_id?: string }
}

// Resolve preferences only against models available to the current caller.
// A shared library's stored model may belong to another tenant.
export function researchModelId(models: AvailableModel[], settings?: ResearchModelSettings): string {
  const available = models.filter(model => !!model.id)
  const preferences = [settings?.wiki_config?.synthesis_model_id, settings?.summary_model_id]
  for (const id of preferences) {
    if (id && available.some(model => model.id === id)) return id
  }
  return available.find(model => model.is_default)?.id || available[0]?.id || ''
}
