<template>
  <t-badge :count="unreadCount" :max-count="99" :offset="[6, 4]" class="global-invitation-bell">
    <button type="button" class="global-invitation-bell__btn" :aria-label="t('sharingNotifications.title')" :title="t('sharingNotifications.title')" @click="openNotifications"><t-icon name="notification" size="18px" /></button>
  </t-badge>
  <t-dialog v-model:visible="visible" :header="t('sharingNotifications.title')" :footer="false" width="min(560px, calc(100vw - 32px))" :close-on-esc-keydown="true">
    <template #closeBtn><button type="button" class="notification-close" :aria-label="t('common.close')" @click="visible = false"><t-icon name="close" /></button></template>
    <button v-if="auth.pendingInvitationCount" type="button" class="invitation-notice" @click="visible = false; invitationsVisible = true"><t-icon name="usergroup-add" /><span>{{ t('sharingNotifications.invitations', { count: auth.pendingInvitationCount }) }}</span><t-icon name="chevron-right" /></button>
    <t-alert v-if="notifications.error" theme="warning" :message="t('sharingNotifications.error')"><template #operation><t-button variant="text" size="small" @click="notifications.refresh()">{{ t('tenantMember.retry') }}</t-button></template></t-alert>
    <div class="notification-toolbar"><span>{{ t('sharingNotifications.shares') }}</span><t-button v-if="notifications.unreadCount" variant="text" size="small" :loading="notifications.saving" @click="notifications.markRead(notifications.items.map(item => item.share_id))">{{ t('sharingNotifications.markAllRead') }}</t-button></div>
    <t-loading v-if="notifications.loading && !notifications.items.length" :text="t('tenantMember.loading')" />
    <p v-else-if="!notifications.items.length" class="notification-empty">{{ t('sharingNotifications.empty') }}</p>
    <ul v-else class="notification-list"><li v-for="item in notifications.items" :key="item.share_id"><button type="button" class="share-notice" :class="{ unread: !item.read }" @click="openShare(item.share_id)"><t-icon name="folder-add" size="20px" /><span class="share-notice-copy"><strong>{{ t('sharingNotifications.shared', { name: item.shared_by_username || item.org_name || t('sharingNotifications.colleague'), title: item.knowledge_base.name }) }}</strong><small>{{ item.org_name }} · {{ t(`sharingNotifications.${item.permission === 'viewer' ? 'viewer' : 'editor'}`) }} · {{ dateLabel(item.shared_at) }}</small></span><span v-if="!item.read" class="unread-dot" :aria-label="t('sharingNotifications.unread')" /><t-icon name="chevron-right" /></button></li></ul>
  </t-dialog>
  <MyInvitationsDialog v-model:visible="invitationsVisible" />
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useShareNotificationsStore } from '@/stores/shareNotifications'
import { sharedKnowledgeLocation } from '@/utils/shareNotifications'
import MyInvitationsDialog from '@/components/MyInvitationsDialog.vue'
const auth = useAuthStore()
const router = useRouter()
const { t, locale } = useI18n()
const notifications = useShareNotificationsStore()
const visible = ref(false)
const invitationsVisible = ref(false)
const unreadCount = computed(() => auth.pendingInvitationCount + notifications.unreadCount)
const dateLabel = (value: string) => {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? '' : date.toLocaleString(locale.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
function openNotifications() { visible.value = true; void notifications.refresh(); void auth.fetchPendingInvitationCount() }
async function openShare(id: string) {
  await notifications.refresh()
  if (notifications.error) return
  const item = notifications.items.find(item => item.share_id === id)
  if (!item) { MessagePlugin.warning(t('sharingNotifications.unavailable')); return }
  await notifications.markRead([id])
  visible.value = false
  await router.push(sharedKnowledgeLocation(item))
}
const poll = () => { if (!document.hidden) void notifications.refresh() }
const timer = setInterval(poll, 60_000)
window.addEventListener('focus', poll)
onBeforeUnmount(() => { clearInterval(timer); window.removeEventListener('focus', poll) })
</script>
<style lang="less" scoped>
.global-invitation-bell { position: fixed; top: 12px; right: 16px; z-index: 100; }
.global-invitation-bell__btn, .notification-close {
  display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px;
  padding: 0; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-lg);
  background: var(--td-bg-color-container); color: var(--td-text-color-secondary); cursor: pointer;
  &:hover { background: var(--td-bg-color-secondarycontainer); color: var(--td-brand-color); }
  &:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
}
.notification-close { border: 0; background: transparent; }
.invitation-notice, .share-notice {
  display: flex; align-items: center; gap: 12px; width: 100%; padding: 14px 12px; text-align: left;
  border: 0; border-radius: var(--app-radius-md); color: var(--td-text-color-primary); background: transparent; font: inherit; cursor: pointer;
  &:hover { background: var(--td-bg-color-secondarycontainer); }
  &:focus-visible { outline: 2px solid var(--td-brand-color); }
}
.invitation-notice { background: var(--td-bg-color-secondarycontainer); span { flex: 1; } }
.notification-toolbar { display: flex; align-items: center; justify-content: space-between; min-height: 48px; color: var(--td-text-color-secondary); }
.notification-list { margin: 0; padding: 0; list-style: none; max-height: min(60vh, 500px); overflow-y: auto; }
.share-notice-copy { flex: 1; min-width: 0; strong { display: block; font-weight: 400; overflow-wrap: anywhere; } small { display: block; margin-top: 6px; color: var(--td-text-color-secondary); } }
.unread .share-notice-copy strong { font-weight: 600; }
.unread-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--td-brand-color); flex-shrink: 0; }
.notification-empty { padding: 28px 0; text-align: center; color: var(--td-text-color-secondary); }
</style>
