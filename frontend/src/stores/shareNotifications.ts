import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { getCurrentUser, updateMyPreferences } from '@/api/auth'
import { listSharedKnowledgeBases, type SharedKnowledgeBase } from '@/api/organization'
import { useAuthStore } from './auth'
import { shareNotificationItems } from '@/utils/shareNotifications'

export const useShareNotificationsStore = defineStore('shareNotifications', () => {
  const auth = useAuthStore()
  const shares = ref<SharedKnowledgeBase[]>([])
  const readIds = ref<string[]>([])
  const loading = ref(false)
  const saving = ref(false)
  const error = ref(false)
  let generation = 0
  const items = computed(() => shareNotificationItems(shares.value, readIds.value))
  const unreadCount = computed(() => items.value.filter(item => !item.read).length)
  const principal = computed(() => `${auth.user?.id || ''}:${auth.effectiveTenantId || ''}`)

  async function refresh() {
    if (!auth.isLoggedIn || !auth.effectiveTenantId || loading.value) return
    const request = generation
    loading.value = true
    try {
      const [shared, profile] = await Promise.all([listSharedKnowledgeBases(), getCurrentUser()])
      if (request !== generation) return
      if (!shared.success || !profile.success || !profile.data) throw new Error('notification read failed')
      shares.value = shared.data || []
      readIds.value = [...new Set([...readIds.value, ...(profile.data.user.preferences?.read_share_ids || [])])]
      error.value = false
    } catch {
      if (request === generation) error.value = true
    } finally {
      if (request === generation) loading.value = false
    }
  }

  async function markRead(ids: string[]) {
    if (saving.value || !ids.length) return
    const request = generation
    const allowed = new Set(items.value.map(item => item.share_id))
    const next = [...new Set([...readIds.value, ...ids.filter(id => allowed.has(id))])].slice(-2000)
    saving.value = true
    try {
      const result = await updateMyPreferences({ read_share_ids: next })
      if (request !== generation) return
      if (!result.success || !result.data?.read_share_ids) throw new Error('notification update failed')
      readIds.value = result.data.read_share_ids
      error.value = false
    } catch {
      if (request === generation) error.value = true
    } finally {
      if (request === generation) saving.value = false
    }
  }

  watch(principal, () => {
    generation++
    shares.value = []
    readIds.value = []
    loading.value = false
    saving.value = false
    error.value = false
    void refresh()
  }, { immediate: true })
  return { items, unreadCount, loading, saving, error, refresh, markRead }
})
