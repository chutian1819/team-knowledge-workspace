<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import * as api from '@/api/research'
import { errorMessage } from './workspaceState'
import { applicableWritingRules, writingRuleSelection } from './writingExperience'
import { diffWikiLines } from '@/utils/wikiLineDiff'
import { canExtractWritingEvent, mergeWritingEventPage, writingEventContentChange, writingEventName, writingLearningStatus } from './writingActivity'

const props = defineProps<{
  kbId: string; draftId: string; templateId: string; modelId: string; canGenerate: boolean;
  canEdit: boolean; disabled: boolean; templates: api.ResearchTemplate[];
  proposals: api.Proposal[]; modelValue: string[]; activityVersion?: string;
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const rules = ref<api.WritingRule[]>([])
const busy = ref(false)
const loading = ref(true)
const error = ref('')
const visible = ref(false)
const tab = ref<api.WritingRule['status']>('active')
const editing = ref<api.WritingRule | null>(null)
const formVisible = ref(false)
const name = ref('')
const instruction = ref('')
const template = ref('')
const formBaseline = ref('')
const extractionProposal = ref('')
const feedbackProposal = ref('')
const feedback = ref<api.WritingFeedback[]>([])
const feedbackReasons = ref<Record<string, string>>({})
const sourceProposal = ref<api.Proposal | null>(null)
const sourceEvent = ref<api.WritingEvent | null>(null)
const events = ref<api.WritingEvent[]>([])
const learning = ref<api.WritingLearning>({ enabled: false, model_id: '' })
const learningLoaded = ref(false)
const activityLoading = ref(false)
const olderActivityLoading = ref(false)
const hasOlderEvents = ref(false)
let olderPageLoaded = false
const managerSection = ref<'rules' | 'activity'>('rules')
let epoch = 0
let feedbackRequest = 0
let activityRequest = 0
let olderActivityRequest = 0
let activityTimer: ReturnType<typeof setTimeout> | undefined
const available = computed(() => applicableWritingRules(rules.value, props.templateId))
const selected = computed(() => writingRuleSelection(props.modelValue, rules.value, props.templateId))
const candidates = computed(() => rules.value.filter(rule => rule.status === 'candidate'))
const shown = computed(() => rules.value.filter(rule => rule.status === tab.value))
const accepted = computed(() => props.proposals.filter(item => item.status === 'accepted'))
const usedProposals = computed(() => props.proposals.filter(item => item.writing_rules?.length))
const used = computed(() => usedProposals.value.find(item => item.id === feedbackProposal.value))
const locked = computed(() => busy.value || props.disabled || loading.value)
const closeLocked = computed(() => busy.value || props.disabled)
const formDirty = computed(() => writingFormSnapshot() !== formBaseline.value)
const feedbackDirty = computed(() => Object.entries(feedbackReasons.value).some(([id, reason]) =>
  reason !== (feedback.value.find(item => item.rule_id === id && item.is_mine)?.reason || '')))
const formValid = computed(() => name.value.trim() && instruction.value.trim())
const sourceDiff = computed(() => {
  if (sourceEvent.value) {
    const change = writingEventContentChange(sourceEvent.value)
    return change ? diffWikiLines(change.before, change.after) : []
  }
  return sourceProposal.value ? diffWikiLines(sourceProposal.value.base_content, sourceProposal.value.content) : []
})

function eventTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}

async function refreshActivity(quiet = false) {
  if (activityTimer) clearTimeout(activityTimer)
  const current = ++activityRequest
  if (!quiet) activityLoading.value = true
  try {
    const [activity, config] = await Promise.all([api.listWritingEvents(props.kbId, props.draftId), api.getWritingLearning(props.kbId, props.draftId)])
    if (current !== activityRequest) return
    const page = activity.data || []
    const previousOldest = events.value[events.value.length - 1]?.id
    events.value = mergeWritingEventPage(events.value, page)
    if (previousOldest && !events.value.some(event => event.id === previousOldest)) olderPageLoaded = false
    if (!olderPageLoaded) hasOlderEvents.value = page.length >= 100
    learning.value = config.data; learningLoaded.value = true
    if (quiet && !busy.value) await refresh(true)
  } catch (err) { if (current === activityRequest && !quiet) error.value = errorMessage(err) }
  finally {
    if (current === activityRequest) {
      activityLoading.value = false
      if (visible.value || learning.value.enabled) activityTimer = setTimeout(() => { void refreshActivity(true) }, 5000)
    }
  }
}

async function loadOlderActivity() {
  const cursor = events.value[events.value.length - 1]?.id
  if (!cursor || olderActivityLoading.value || locked.value) return
  const current = ++olderActivityRequest
  olderActivityLoading.value = true
  try {
    const response = await api.listWritingEvents(props.kbId, props.draftId, cursor)
    if (current !== olderActivityRequest) return
    const page = response.data || []
    events.value = mergeWritingEventPage(events.value, page, true)
    olderPageLoaded = true; hasOlderEvents.value = page.length >= 100
  } catch (err) { if (current === olderActivityRequest) error.value = errorMessage(err) }
  finally { if (current === olderActivityRequest) olderActivityLoading.value = false }
}

async function configureLearning(enabled: boolean) {
  if (!props.canEdit || locked.value || !learningLoaded.value || (enabled && !props.canGenerate)) return
  const current = epoch; busy.value = true; error.value = ''
  try {
    const result = await api.saveWritingLearning(props.kbId, props.draftId, { enabled, model_id: enabled ? props.modelId : learning.value.model_id })
    if (current !== epoch) return
    learning.value = result.data
    MessagePlugin.success(enabled ? '已开启后台学习，候选经验仍需你确认' : '已关闭自动提炼，写作记录继续保存')
    await refreshActivity()
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

async function extractEvent(event: api.WritingEvent) {
  if (!props.canEdit || !props.canGenerate || locked.value || !canExtractWritingEvent(event)) return
  const current = epoch; busy.value = true; error.value = ''
  try {
    const result = await api.extractWritingEvent(props.kbId, props.draftId, event.id, { model_id: props.modelId })
    if (current !== epoch) return
    tab.value = 'candidate'; managerSection.value = 'rules'; await refresh(); await refreshActivity()
    MessagePlugin.success(result.data?.length ? `已提炼 ${result.data.length} 条候选，请逐条确认` : '这次修改没有可复用的风格经验')
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

function openManager(section: 'rules' | 'activity') { managerSection.value = section; visible.value = true }

type ExperienceDialog = 'manager' | 'form' | 'source'
function topExperienceDialog(): ExperienceDialog | undefined {
  return sourceEvent.value || sourceProposal.value ? 'source' : formVisible.value ? 'form' : visible.value ? 'manager' : undefined
}
function writingFormSnapshot() { return JSON.stringify([name.value, instruction.value, template.value]) }
function closeExperienceDialog(dialog: ExperienceDialog, event?: Event) {
  if (dialog !== topExperienceDialog() || event?.defaultPrevented) return
  // Consume the same dismissal before another dialog handles it, including
  // when a pending write or an unsaved form keeps this layer open.
  event?.preventDefault(); event?.stopImmediatePropagation()
  if (closeLocked.value) return
  const dirty = dialog === 'form' ? formDirty.value : dialog === 'manager' && feedbackDirty.value
  if (dirty && !window.confirm(dialog === 'form' ? '经验内容尚未保存，关闭后将放弃这些修改。是否关闭？' : '反馈原因尚未提交，关闭后将放弃这些输入。是否关闭？')) return
  if (dialog === 'source') { sourceEvent.value = null; sourceProposal.value = null }
  else if (dialog === 'form') formVisible.value = false
  else visible.value = false
}
// TDesign handles Escape by code. Assistive/browser input can supply only key;
// run after document popup handlers, and dismiss just the innermost open layer.
function closeExperienceDialogOnEscape(event: KeyboardEvent) {
  if (event.defaultPrevented || (event.key !== 'Escape' && event.code !== 'Escape')) return
  const dialog = topExperienceDialog()
  if (dialog) closeExperienceDialog(dialog, event)
}

function templateName(id: string) { return props.templates.find(item => item.id === id)?.name || (id ? id : '本资料集所有文种') }
function statusName(status: string) { return ({ candidate: '待确认', active: '已启用', revoked: '已撤销', accepted: '已接受', rejected: '已拒绝', pending: '待审' } as Record<string, string>)[status] || status }
function proposalName(item: api.Proposal) { return `v${item.base_version} → ${statusName(item.status)} · ${item.id.slice(0, 8)}` }

async function refresh(quiet = false) {
  const current = ++epoch
  if (!quiet) { loading.value = true; error.value = '' }
  try {
    const result = await api.listWritingRules(props.kbId)
    if (current !== epoch) return
    rules.value = result.data || []
    emit('update:modelValue', writingRuleSelection(props.modelValue, rules.value, props.templateId))
  } catch (err) {
    if (current !== epoch) return
    if (!quiet) { rules.value = []; emit('update:modelValue', []); error.value = errorMessage(err) }
  } finally { if (current === epoch) loading.value = false }
}

function toggle(id: string, checked: boolean) {
  emit('update:modelValue', writingRuleSelection(checked ? [...selected.value, id] : selected.value.filter(item => item !== id), rules.value, props.templateId))
}

function edit(rule?: api.WritingRule) {
  editing.value = rule || null
  name.value = rule?.name || ''; instruction.value = rule?.instruction || ''
  template.value = rule?.template_id ?? props.templateId
  formBaseline.value = writingFormSnapshot()
  formVisible.value = true
}

async function submit() {
  if (!props.canEdit || locked.value || !formValid.value) return
  busy.value = true; error.value = ''; const current = epoch
  try {
    if (editing.value) {
      await api.decideWritingRule(props.kbId, editing.value.id, {
        base_version: editing.value.version, status: 'active', name: name.value.trim(), instruction: instruction.value.trim(),
      })
    } else {
      await api.createWritingRule(props.kbId, { name: name.value.trim(), instruction: instruction.value.trim(), template_id: template.value })
    }
    if (current !== epoch) return
    formVisible.value = false; tab.value = editing.value ? 'active' : 'candidate'
    MessagePlugin.success(editing.value ? '经验已确认，可在生成前选用' : '已保存候选，请审阅后确认启用')
    await refresh()
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

async function revoke(rule: api.WritingRule) {
  if (!props.canEdit || locked.value) return
  busy.value = true; error.value = ''; const current = epoch
  try {
    await api.decideWritingRule(props.kbId, rule.id, { base_version: rule.version, status: 'revoked' })
    if (current !== epoch) return
    await refresh(); MessagePlugin.success('经验已撤销，历史使用记录仍保留')
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

async function extract() {
  if (!props.canEdit || !props.canGenerate || !extractionProposal.value || locked.value) return
  busy.value = true; error.value = ''; const current = epoch
  try {
    const result = await api.extractWritingRules(props.kbId, props.draftId, { proposal_id: extractionProposal.value, model_id: props.modelId })
    if (current !== epoch) return
    tab.value = 'candidate'; await refresh()
    MessagePlugin.success(result.data?.length ? `已提炼 ${result.data.length} 条候选，请逐条确认` : '这次修改没有可复用的风格经验')
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

async function loadFeedback() {
  const current = ++feedbackRequest; feedback.value = []; feedbackReasons.value = {}
  const proposalId = feedbackProposal.value
  if (!proposalId) return
  try {
    const result = await api.listWritingFeedback(props.kbId, props.draftId, proposalId)
    if (current !== feedbackRequest) return
    feedback.value = result.data || []
    for (const item of feedback.value.filter(item => item.is_mine)) feedbackReasons.value[item.rule_id] = item.reason || ''
  } catch (err) { if (current === feedbackRequest) error.value = errorMessage(err) }
}

async function viewSource(rule: api.WritingRule) {
  if (!rule.source_page_id || (!rule.source_proposal_id && !rule.source_event_id) || locked.value) return
  busy.value = true; error.value = ''; const current = epoch
  try {
    if (rule.source_event_id) {
      const result = await api.getWritingEvent(props.kbId, rule.source_page_id, rule.source_event_id)
      if (current !== epoch) return
      sourceEvent.value = result.data || null
      if (!sourceEvent.value) error.value = '来源写作记录当前不可访问，请刷新经验列表。'
    } else {
      const result = await api.listProposals(props.kbId, rule.source_page_id)
      if (current !== epoch) return
      sourceProposal.value = result.data.find(item => item.id === rule.source_proposal_id) || null
      if (!sourceProposal.value) error.value = '来源提案当前不可访问，请刷新经验列表。'
    }
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

async function rate(ruleId: string, rating: api.WritingRating) {
  if (!props.canEdit || locked.value || !used.value) return
  busy.value = true; error.value = ''; const current = epoch
  try {
    await api.saveWritingFeedback(props.kbId, props.draftId, used.value.id, { rule_id: ruleId, rating, reason: feedbackReasons.value[ruleId] || '' })
    if (current !== epoch) return
    await loadFeedback(); MessagePlugin.success('已保存本次使用反馈')
  } catch (err) { if (current === epoch) error.value = errorMessage(err) }
  finally { busy.value = false }
}

watch(() => props.templateId, () => emit('update:modelValue', writingRuleSelection(props.modelValue, rules.value, props.templateId)))
watch(accepted, items => { if (!items.some(item => item.id === extractionProposal.value)) extractionProposal.value = items[0]?.id || '' }, { immediate: true })
watch(usedProposals, items => { if (!items.some(item => item.id === feedbackProposal.value)) feedbackProposal.value = items[0]?.id || '' }, { immediate: true })
watch(feedbackProposal, () => { if (visible.value) void loadFeedback() })
watch(visible, value => { if (value) { void refresh(); void refreshActivity(); void loadFeedback() } })
watch(() => props.activityVersion, () => { void refreshActivity() })
void refresh()
void refreshActivity()
window.addEventListener('keydown', closeExperienceDialogOnEscape)
onBeforeUnmount(() => { epoch++; feedbackRequest++; activityRequest++; olderActivityRequest++; if (activityTimer) clearTimeout(activityTimer); window.removeEventListener('keydown', closeExperienceDialogOnEscape) })
</script>

<template>
  <section class="writing-experience" aria-label="写作经验">
    <div class="section-heading"><h4>写作经验 <span v-if="selected.length">{{ selected.length }}/5</span></h4><t-button size="small" variant="text" :disabled="busy || disabled" @click="openManager('rules')">管理<span v-if="candidates.length"> · {{ candidates.length }} 待确认</span></t-button></div>
    <p class="note">修改留痕 → 提炼候选 → 确认并勾选 → 后续复用。经验适用于当前研究稿资料集；仅显示来源仍可访问的经验。</p>
    <p v-if="loading" class="note" role="status">正在加载经验…</p>
    <template v-else>
      <p v-if="!available.length" class="note">确认写作经验后，可在这里选用。</p>
      <label v-for="rule in available" :key="rule.id" class="rule-option" :title="rule.instruction">
        <input type="checkbox" :checked="selected.includes(rule.id)" :disabled="locked || !canEdit || (!selected.includes(rule.id) && selected.length >= 5)" @change="toggle(rule.id, ($event.target as HTMLInputElement).checked)" />
        <span>{{ rule.name }}<small>{{ rule.instruction }}</small></span>
      </label>
      <p v-if="available.length" class="note">仅将勾选的经验用于本次修改。</p>
    </template>
    <div class="learning-control"><label><input type="checkbox" :checked="learning.enabled" :disabled="locked || !canEdit || !learningLoaded || (!learning.enabled && !canGenerate)" aria-label="自动学习我的写作习惯" @change="configureLearning(($event.target as HTMLInputElement).checked)" /><span>自动学习我的写作习惯</span></label><p class="note">{{ learning.enabled ? '正文保存后在后台提炼候选，确认后再复用。' : '提示词和版本差异已自动记录，可按需提炼。' }}</p><p v-if="learning.enabled" class="note" role="status">{{ writingLearningStatus(learning.status) }}</p><p v-if="learning.last_error" class="panel-error" role="alert">自动提炼：{{ learning.last_error }}</p><t-button size="small" variant="text" :disabled="busy || disabled" @click="openManager('activity')">写作记录与学习</t-button></div>
    <p v-if="error && !visible" class="panel-error" role="alert">{{ error }} <button type="button" @click="refresh()">重新加载</button></p>
    <t-dialog :visible="visible" header="写作经验" width="min(840px, 94vw)" :footer="false" :close-on-overlay-click="!closeLocked && topExperienceDialog() === 'manager'" :close-on-esc-keydown="!closeLocked && topExperienceDialog() === 'manager'" @close="closeExperienceDialog('manager', $event.e)">
      <template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭写作经验" :disabled="closeLocked" @click.stop="closeExperienceDialog('manager')"><t-icon name="close" aria-hidden="true" /></t-button></template>
      <div class="experience-manager">
        <t-alert v-if="error" theme="error" :message="error" />
        <p class="note">经验在当前研究稿资料集内共享。根据真实修改提炼，审阅后启用；事实和数据仍以引用来源为准。</p>
        <div class="status-tabs manager-sections" role="group" aria-label="写作经验管理视图"><button type="button" :class="{ active: managerSection === 'rules' }" :aria-pressed="managerSection === 'rules'" @click="managerSection = 'rules'">经验库</button><button type="button" :class="{ active: managerSection === 'activity' }" :aria-pressed="managerSection === 'activity'" @click="managerSection = 'activity'">写作记录与学习 · {{ events.length }}</button></div>
        <template v-if="managerSection === 'activity'">
          <div class="learn-box"><label class="auto-learning-option"><input type="checkbox" :checked="learning.enabled" :disabled="locked || !canEdit || !learningLoaded || (!learning.enabled && !canGenerate)" aria-label="后台自动提炼候选经验" @change="configureLearning(($event.target as HTMLInputElement).checked)" /><span>后台自动提炼候选经验</span></label><p class="note">提交的真实提示词、AI 提案和每次保存的前后稿差异会自动记录。开启后，在保存正文或接受修改时学习；候选经验需要你确认，不会自动用于写作。</p><p class="note">当前状态：{{ learning.enabled ? writingLearningStatus(learning.status) : '自动提炼已关闭，继续记录写作过程' }}</p><p v-if="!canGenerate" class="note">请先在 AI 修订区选择可用模型，再开启自动提炼或按次提炼。</p><p v-if="learning.last_error" class="panel-error" role="alert">{{ learning.last_error }}</p><t-button v-if="learning.enabled && learning.model_id !== modelId && canGenerate && canEdit" size="small" variant="outline" :disabled="locked" @click="configureLearning(true)">改用当前选择的模型学习</t-button></div>
          <div class="section-heading"><h4>提示词与版本差异</h4><t-button size="small" variant="text" :disabled="locked || activityLoading" @click="refreshActivity()">刷新记录</t-button></div>
          <p v-if="activityLoading && !events.length" class="note" role="status">正在加载写作记录…</p><p v-else-if="!events.length" class="note empty">后续提交的提示词和保存的版本会自动出现在这里。已有版本记录仍可从编辑器查看。</p>
          <article v-for="event in events" :key="event.id" class="experience-card activity-card"><div class="card-heading"><strong>{{ writingEventName(event.kind) }}</strong><small>{{ eventTime(event.created_at) }}</small></div><p v-if="event.instruction" class="event-instruction">{{ event.instruction }}</p><p v-else class="note">{{ event.kind === 'save' ? '手工编辑保存，未提交 AI 提示词。' : event.kind === 'create' ? '建立文稿，尚未提交 AI 提示词。' : '未新增提示词，关联本次版本操作。' }}</p><div class="event-meta"><small>v{{ event.base_version }}<template v-if="event.result_version"> → v{{ event.result_version }}</template><template v-if="event.proposal_id"> · 提案 {{ event.proposal_id.slice(0, 8) }}</template></small><small v-if="event.learning_status" :class="{ 'panel-error': event.learning_status === 'failed' }">{{ writingLearningStatus(event.learning_status) }}</small></div><p v-if="event.error || event.learning_error" class="panel-error" role="alert">{{ event.error || event.learning_error }}</p><div class="actions"><t-button size="small" variant="text" :disabled="locked" @click="sourceEvent = event">查看提示词与差异</t-button><t-button v-if="canEdit && canExtractWritingEvent(event)" size="small" variant="outline" :disabled="locked || !canGenerate || ['queued', 'pending', 'running', 'processing'].includes(event.learning_status || '')" :loading="busy" @click="extractEvent(event)">{{ event.learning_status === 'failed' ? '重新提炼' : '提炼这次修改' }}</t-button></div></article>
          <t-button v-if="hasOlderEvents" variant="outline" :disabled="locked || activityLoading || olderActivityLoading" :loading="olderActivityLoading" @click="loadOlderActivity">加载更早的记录</t-button>
        </template>
        <template v-else>
        <div v-if="canEdit" class="actions"><t-button variant="outline" :disabled="locked" @click="edit()">手动添加候选</t-button><t-button variant="text" :disabled="locked" @click="managerSection = 'activity'">从正常写作中学习</t-button></div>
        <details v-if="canEdit && accepted.length" class="legacy-learning"><summary>从历史已接受提案提炼</summary><div class="learn-box"><label class="field">选择历史提案<t-select v-model="extractionProposal" aria-label="提炼经验的来源提案" :disabled="locked"><t-option v-for="item in accepted" :key="item.id" :value="item.id" :label="proposalName(item)" /></t-select></label><t-button :disabled="locked || !canGenerate || !extractionProposal" :loading="busy" @click="extract">提炼候选经验</t-button></div></details>
        <div class="manager-toolbar"><div class="status-tabs" role="group" aria-label="经验状态"><button v-for="state in (['active', 'candidate', 'revoked'] as const)" :key="state" type="button" :class="{ active: tab === state }" :aria-pressed="tab === state" @click="tab = state">{{ statusName(state) }} · {{ rules.filter(rule => rule.status === state).length }}</button></div><t-button variant="text" size="small" :disabled="locked" @click="refresh()">刷新</t-button></div>
        <p v-if="!shown.length" class="note empty">暂无{{ statusName(tab) }}经验</p>
        <article v-for="rule in shown" :key="rule.id" class="experience-card">
          <div class="card-heading"><strong>{{ rule.name }}</strong><small>v{{ rule.version }} · {{ templateName(rule.template_id) }}</small></div>
          <p>{{ rule.instruction }}</p>
          <div v-if="rule.source_event_id || rule.source_proposal_id" class="source-label"><small>来源：{{ rule.source_event_id ? `写作记录 ${rule.source_event_id.slice(0, 8)}` : `已接受提案 ${rule.source_proposal_id?.slice(0, 8)}` }}</small><t-button size="small" variant="text" :disabled="locked" @click="viewSource(rule)">查看修改</t-button></div><small v-else>来源：人工添加</small>
          <div v-if="canEdit && rule.status !== 'revoked'" class="actions"><t-button v-if="rule.status === 'candidate'" size="small" :disabled="locked" @click="edit(rule)">审阅并确认</t-button><t-popconfirm :content="rule.status === 'candidate' ? '撤销这条候选经验？' : '撤销后将不再用于新的修改，历史记录仍保留。'" @confirm="revoke(rule)"><t-button size="small" variant="text" :disabled="locked">{{ rule.status === 'candidate' ? '放弃候选' : '撤销' }}</t-button></t-popconfirm></div>
        </article>
        <section v-if="usedProposals.length" class="feedback-section" aria-label="经验使用反馈">
          <h4>本稿的使用反馈</h4><p class="note">反馈针对这次提案实际使用的经验版本，不会自动改变经验状态。</p>
          <t-select v-model="feedbackProposal" aria-label="反馈对应的提案" :disabled="locked"><t-option v-for="item in usedProposals" :key="item.id" :value="item.id" :label="proposalName(item)" /></t-select>
          <article v-for="rule in used?.writing_rules || []" :key="rule.id" class="experience-card">
            <strong>{{ rule.name }} · v{{ rule.version }}</strong><p>{{ rule.instruction }}</p>
            <p v-for="item in feedback.filter(item => item.rule_id === rule.id)" :key="`${item.created_by}:${item.updated_at}`" class="note">{{ item.is_mine ? '我的反馈' : '成员反馈' }}：{{ ({ useful: '有用', useless: '无用', misapplied: '误用' })[item.rating] }}<span v-if="item.reason"> · {{ item.reason }}</span></p>
            <template v-if="canEdit"><t-input v-model="feedbackReasons[rule.id]" placeholder="具体影响（选填）" aria-label="经验反馈原因" :maxlength="2000" :disabled="locked" /><div class="actions"><t-button v-for="rating in (['useful', 'useless', 'misapplied'] as const)" :key="rating" size="small" variant="outline" :disabled="locked" @click="rate(rule.id, rating)">{{ ({ useful: '有用', useless: '无用', misapplied: '误用' })[rating] }}</t-button></div></template>
          </article>
        </section>
        </template>
      </div>
    </t-dialog>
    <t-dialog :visible="formVisible" :header="editing ? '审阅并确认经验' : '添加候选经验'" width="min(560px, 94vw)" :close-on-overlay-click="!closeLocked && topExperienceDialog() === 'form'" :close-on-esc-keydown="!closeLocked && topExperienceDialog() === 'form'" :cancel-btn="{ disabled: closeLocked }" :confirm-btn="{ content: editing ? '确认启用' : '保存候选', loading: busy, disabled: locked || !formValid }" @close="closeExperienceDialog('form', $event.e)" @confirm="submit">
      <template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭经验编辑" :disabled="closeLocked" @click.stop="closeExperienceDialog('form')"><t-icon name="close" aria-hidden="true" /></t-button></template>
      <div class="rule-form"><t-alert v-if="error" theme="error" :message="error" /><label class="field">经验名称<t-input v-model="name" aria-label="经验名称" :maxlength="80" :disabled="locked" /></label><label class="field">风格与方法要求<t-textarea v-model="instruction" aria-label="写作经验内容" :autosize="{ minRows: 4, maxRows: 9 }" :disabled="locked" placeholder="例如：先给核心判断，再分段写依据和待验证事项。" /></label><label class="field">适用文种<t-select v-model="template" aria-label="经验适用文种" :disabled="locked || !!editing"><t-option value="" label="本资料集所有文种" /><t-option v-for="item in templates" :key="item.id" :value="item.id" :label="item.name" /></t-select></label><p class="note">确认前请删除具体公司、数字和事实结论，只保留可复用的写作方式。</p></div>
    </t-dialog>
    <t-dialog :visible="!!sourceProposal || !!sourceEvent" :header="sourceEvent ? '写作记录与版本差异' : '经验来源修改'" width="min(840px, 94vw)" :footer="false" :close-on-overlay-click="!closeLocked" :close-on-esc-keydown="!closeLocked" @close="closeExperienceDialog('source', $event.e)"><template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭经验来源与差异" :disabled="closeLocked" @click.stop="closeExperienceDialog('source')"><t-icon name="close" aria-hidden="true" /></t-button></template><template v-if="sourceEvent || sourceProposal"><p class="note">{{ sourceEvent ? writingEventName(sourceEvent.kind) : '已接受提案' }} · 基于 v{{ sourceEvent?.base_version ?? sourceProposal?.base_version }}<template v-if="sourceEvent?.result_version"> → v{{ sourceEvent.result_version }}</template> · {{ (sourceEvent?.id || sourceProposal?.id || '').slice(0, 8) }}</p><p class="source-instruction">提示词：{{ sourceEvent?.instruction || sourceProposal?.instruction || '这次操作未提交 AI 提示词。' }}</p><p v-if="sourceEvent?.error" class="panel-error">{{ sourceEvent.error }}</p><p v-if="sourceEvent && ['prompt', 'generation_failed'].includes(sourceEvent.kind)" class="note">此记录未生成或保存新正文。</p><p v-else-if="!sourceDiff.length" class="note">这次操作没有产生正文差异。</p><pre v-if="sourceDiff.length" class="source-diff" aria-label="经验来源前后稿差异"><span v-for="(line, index) in sourceDiff" :key="index" :class="line.type">{{ line.type === 'add' ? '+' : line.type === 'del' ? '−' : ' ' }} {{ line.text }}
</span></pre></template></t-dialog>
  </section>
</template>

<style scoped lang="less">
.writing-experience { border-top: 1px solid var(--app-border-subtle); padding-top: 16px; min-width: 0; }
h4, p { margin: 0; }
.section-heading, .manager-toolbar, .card-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.section-heading h4, h4 { font-size: var(--app-text-md); font-weight: 500; }
.section-heading h4 span { color: var(--td-text-color-secondary); font-size: var(--app-text-xs); }
.note, small { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.7; }
.rule-option { display: flex; align-items: flex-start; gap: 8px; margin-top: 12px; cursor: pointer; font-size: var(--app-text-sm); line-height: 1.6; input { margin: 5px 0 0; accent-color: var(--td-brand-color); } span { min-width: 0; overflow-wrap: anywhere; } small { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; margin-top: 3px; font-size: var(--app-text-xs); } }
.writing-experience > .note { margin-top: 8px; }
.learning-control { margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--app-border-subtle); > label, .auto-learning-option { display: flex; align-items: flex-start; gap: 8px; font-size: var(--app-text-sm); line-height: 1.7; cursor: pointer; } input { margin-top: 5px; accent-color: var(--td-brand-color); } > p { margin-top: 7px; } > .t-button { margin-top: 6px; margin-left: -8px; } }
.auto-learning-option { display: flex; align-items: center; gap: 9px; font-size: var(--app-text-md); cursor: pointer; input { accent-color: var(--td-brand-color); } }
.event-meta { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.activity-card .event-instruction { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
.legacy-learning { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); summary { cursor: pointer; padding: 4px 0; } > div { margin-top: 10px; } }
.manager-sections { padding-bottom: 12px; border-bottom: 1px solid var(--app-border-subtle); }
.experience-manager, .rule-form, .feedback-section { display: grid; gap: 16px; }
.experience-manager { max-height: min(62dvh, 640px); overflow-y: auto; padding-right: 4px; }
.field { display: grid; gap: 8px; font-size: var(--app-text-md); }
.learn-box { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--app-border-subtle); background: var(--app-surface-muted); border-radius: var(--app-radius-md); }
.actions { display: flex; gap: 8px; flex-wrap: wrap; }
.manager-toolbar { flex-wrap: wrap; }
.status-tabs { display: flex; gap: 6px; flex-wrap: wrap; button { color: var(--td-text-color-secondary); background: transparent; border: 1px solid transparent; padding: 7px 10px; border-radius: var(--app-radius-sm); cursor: pointer; font: inherit; font-size: var(--app-text-sm); &.active { color: var(--td-text-color-link); border-color: var(--app-border-subtle); background: var(--app-surface-muted); } } }
.experience-card { display: grid; gap: 10px; padding: 16px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-md); overflow-wrap: anywhere; strong { font-size: var(--app-text-md); font-weight: 500; } p { white-space: pre-wrap; line-height: 1.8; } }
.card-heading { flex-wrap: wrap; }
.feedback-section { border-top: 1px solid var(--app-border-subtle); padding-top: 20px; }
.source-label { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
.source-instruction { margin-top: 12px; white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; }
.source-diff { max-height: 50vh; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; padding: 14px; margin-top: 12px; background: var(--app-surface-muted); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-md); font-size: var(--app-text-sm); line-height: 1.8; .add { background: var(--td-success-color-light); } .del { background: var(--td-error-color-light); } }
.empty { text-align: center; padding: 20px; }
.panel-error { font-size: var(--app-text-xs); color: var(--td-error-color); line-height: 1.8; button { background: transparent; border: 0; color: var(--td-text-color-link); cursor: pointer; } }
button:focus-visible, input:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
</style>
