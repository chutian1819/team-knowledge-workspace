<template>
  <div class="login-layout">
    <!-- Logo - Top Left -->
    <a href="https://github.com/Tencent/WeKnora" target="_blank" rel="noopener noreferrer" class="header-logo" :title="$t('common.github')">
      <img src="@/assets/img/weknora.png" alt="WeKnora" class="logo-image" />
    </a>

    <!-- Header Links - Top Right -->
    <div class="header-links">
      <a href="https://weknora.weixin.qq.com" target="_blank" rel="noopener noreferrer" class="header-link" :title="$t('common.website')">
        <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"
          stroke-linecap="round">
          <circle cx="12" cy="12" r="10" />
          <line x1="2" y1="12" x2="22" y2="12" />
          <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
        </svg>
        <span class="link-text">{{ $t('common.website') }}</span>
      </a>

      <a href="https://github.com/Tencent/WeKnora" target="_blank" rel="noopener noreferrer" class="header-link" :title="$t('common.github')">
        <svg width="17" height="17" viewBox="0 0 24 24" fill="currentColor">
          <path
            d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
        </svg>
        <span class="link-text">GitHub</span>
      </a>

      <div class="language-switch">
        <button type="button" @click="toggleLanguageMenu" @keydown.esc="showLanguageMenu = false" class="header-link" :title="currentLangOption?.label" :aria-expanded="showLanguageMenu" aria-controls="login-language-options">
          <t-icon name="translate" size="17px" aria-hidden="true" />
          <span class="link-text">{{ currentLangOption?.shortLabel }}</span>
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"
            stroke-linecap="round">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </button>

        <!-- Language Dropdown -->
        <div v-if="showLanguageMenu" id="login-language-options" class="language-dropdown" @keydown.esc="showLanguageMenu = false">
          <button v-for="lang in languageOptions" :key="lang.value" type="button" @click="selectLanguage(lang.value)"
            class="language-option" :class="{ active: currentLanguage === lang.value }" :aria-pressed="currentLanguage === lang.value">
            <span class="lang-label">{{ lang.label }}</span>
            <span v-if="currentLanguage === lang.value" class="check-icon" aria-hidden="true">✓</span>
          </button>
        </div>
      </div>
    </div>

    <section class="showcase-section" aria-labelledby="platform-title">
      <div class="showcase-content">
        <div class="showcase-mark" aria-hidden="true"><t-icon name="layers" size="28px" /></div>
        <h1 id="platform-title" class="showcase-subtitle">{{ $t('platform.subtitle') }}</h1>
        <p class="showcase-description">{{ $t('platform.description') }}</p>
        <div class="showcase-features">
          <div class="showcase-feature">
            <t-icon name="folder" size="20px" aria-hidden="true" />
            <div><h2>{{ $t('platform.knowledgeBaseManagement') }}</h2><p>{{ $t('platform.carousel.smartDocRetrievalDesc') }}</p></div>
          </div>
          <div class="showcase-feature">
            <t-icon name="search" size="20px" aria-hidden="true" />
            <div><h2>{{ $t('platform.hybridSearch') }}</h2><p>{{ $t('platform.carousel.hybridSearchDesc') }}</p></div>
          </div>
          <div class="showcase-feature">
            <t-icon name="chat" size="20px" aria-hidden="true" />
            <div><h2>{{ $t('platform.ragQandA') }}</h2><p>{{ $t('platform.carousel.agenticRagDesc') }}</p></div>
          </div>
        </div>
      </div>
    </section>

    <!-- Right Form Section -->
    <div class="form-section">
      <div class="form-panel">
        <!-- Login Card -->
        <div class="form-card" v-if="!isRegisterMode">
          <!-- 登录已有账号时保留邀请上下文。 -->
          <div v-if="inviteLookup" class="invite-banner">
            <t-icon name="link" class="invite-banner__icon" />
            <div class="invite-banner__text">
              <div class="invite-banner__title">
                {{ $t('inviteRegister.bannerTitle', { tenant: inviteLookup.tenant_name || '' }) }}
              </div>
              <div class="invite-banner__hint">
                {{ $t('inviteRegister.bannerHintLogin') }}
              </div>
            </div>
          </div>
          <div v-else-if="inviteLookupError" class="invite-banner invite-banner--error">
            {{ inviteLookupError }}
          </div>
          <div class="form-header">
            <h2 class="form-title">{{ $t('auth.login') }}</h2>
            <p class="form-welcome">{{ $t('auth.subtitle') }}</p>
          </div>

          <div class="form-content">
            <t-form ref="formRef" :data="formData" :rules="formRules" @submit="handleLogin" layout="vertical"
              label-align="top">
              <t-form-item :label="$t('auth.email')" name="email">
                <t-input v-model="formData.email" :placeholder="$t('auth.emailPlaceholder')" type="text"
                  inputmode="email" autocomplete="email" size="large" :disabled="loading" />
              </t-form-item>

              <t-form-item :label="$t('auth.password')" name="password">
                <t-input v-model="formData.password" :placeholder="$t('auth.passwordPlaceholder')" type="password"
                  autocomplete="current-password" size="large" :disabled="loading" />
              </t-form-item>

              <t-button type="submit" theme="primary" size="large" block :loading="loading" class="submit-button">
                {{ loading ? $t('auth.loggingIn') : $t('auth.login') }}
              </t-button>

              <div class="register-cta" v-if="registrationEnabled">
                <div class="register-cta__divider">
                  <span>{{ $t('auth.firstTime') }}</span>
                </div>
                <t-button theme="default" variant="outline" size="large" block class="register-cta__button"
                  :disabled="loading" @click="toggleMode">
                  {{ $t('auth.createAccount') }}
                </t-button>
              </div>

              <div v-if="oidcEnabled" class="oidc-divider">
                <span>{{ $t('auth.orContinueWith') }}</span>
              </div>

              <t-button v-if="oidcEnabled" theme="default" size="large" block :loading="oidcLoading" :disabled="loading"
                class="oidc-button" @click="handleOIDCLogin">
                {{ oidcLoading ? $t('auth.redirectingToOIDC') : oidcLoginText }}
              </t-button>
            </t-form>

          </div>
        </div>

        <!-- Registration must be allowed for this mode and invitation. -->
        <div class="form-card" v-if="isRegisterMode && registrationEnabled">
          <!-- Share-link banner: shown only when ?token= resolved to a
               real invitation row. Sits above the form header so the
               invitee instantly sees who invited them and into which
               workspace, without bumping the existing register UX. -->
          <div v-if="inviteLookup" class="invite-banner">
            <t-icon name="link" class="invite-banner__icon" />
            <div class="invite-banner__text">
              <div class="invite-banner__title">
                {{ $t('inviteRegister.bannerTitle', { tenant: inviteLookup.tenant_name || '' }) }}
              </div>
              <div class="invite-banner__hint">
                {{ $t('inviteRegister.bannerHint') }}
              </div>
            </div>
          </div>
          <div v-else-if="inviteLookupError" class="invite-banner invite-banner--error">
            {{ inviteLookupError }}
          </div>
          <div class="form-header">
            <h2 class="form-title">{{ $t('auth.createAccount') }}</h2>
            <p class="form-subtitle">{{ $t('auth.registerSubtitle') }}</p>
          </div>

          <div class="form-content">
            <t-form ref="registerFormRef" :data="registerData" :rules="registerRules" @submit="handleRegister"
              layout="vertical" label-align="top">
              <t-form-item :label="$t('auth.username')" name="username">
                <t-input v-model="registerData.username" :placeholder="$t('auth.usernamePlaceholder')" autocomplete="username" size="large"
                  :disabled="loading" />
              </t-form-item>

              <t-form-item :label="$t('auth.email')" name="email">
                <t-input v-model="registerData.email" :placeholder="$t('auth.emailPlaceholder')" type="text"
                  inputmode="email" autocomplete="email" size="large" :disabled="loading" />
              </t-form-item>

              <t-form-item :label="$t('auth.password')" name="password">
                <t-input v-model="registerData.password" :placeholder="$t('auth.passwordPlaceholder')" type="password"
                  autocomplete="new-password" size="large" :disabled="loading" />
              </t-form-item>

              <t-form-item :label="$t('auth.confirmPassword')" name="confirmPassword">
                <t-input v-model="registerData.confirmPassword" :placeholder="$t('auth.confirmPasswordPlaceholder')"
                  type="password" autocomplete="new-password" size="large" :disabled="loading" />
              </t-form-item>

              <t-button type="submit" theme="primary" size="large" block :loading="loading" class="submit-button">
                {{ loading ? $t('auth.registering') : $t('auth.register') }}
              </t-button>
            </t-form>

            <div class="form-footer">
              <span>{{ $t('auth.haveAccount') }}</span>
              <a href="#" @click.prevent="toggleMode" class="link-button">
                {{ $t('auth.backToLogin') }}
              </a>
            </div>


          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, nextTick, onMounted, onBeforeUnmount, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { useRoleLabel } from '@/composables/useRoleLabel'
import { notifyLoginSuccess } from '@/utils/loginNotify'
import { canRegister } from './registrationPolicy'
import { newPasswordRules } from '@/utils/passwordPolicy'
import {
  login,
  register,
  getOIDCAuthorizationURL,
  getOIDCConfig,
  autoSetup,
  getAuthConfig,
  userInfoFromApi,
  getInvitationByToken,
  registerByInvite,
  type InviteLookup,
} from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const { t, tm, locale } = useI18n()
const { formatRole, roleIcon } = useRoleLabel()

// Form references
const formRef = ref()
const registerFormRef = ref()

// State management
const loading = ref(false)
const oidcLoading = ref(false)
const isRegisterMode = ref(false)
const showLanguageMenu = ref(false)
const oidcEnabled = ref(false)
const oidcProviderName = ref('')
// Wait for configuration before showing a registration entry.
const registrationMode = ref('')
const registrationEnabled = computed(() => canRegister(registrationMode.value, !!inviteLookup.value))
const complexPasswordEnabled = ref(false)

// invite-link state. When the URL carries ?token=xxx we resolve it to
// the originating tenant + role and switch the form into a "register
// via invitation" mode when invitation registration is enabled. Submitting the register
// form with this set hits /auth/register-by-invite (auto-login on
// success) instead of /auth/register.
const inviteToken = ref('')
const inviteLookup = ref<InviteLookup | null>(null)
const inviteLookupError = ref('')
const inviteLookupLoading = ref(false)

// Language options
const languageOptions = [
  { value: 'zh-CN', label: '简体中文', shortLabel: '中文', flag: '🇨🇳' },
  { value: 'zh-TW', label: '繁體中文', shortLabel: '繁中', flag: '繁' },
  { value: 'en-US', label: 'English', shortLabel: 'EN', flag: '🇺🇸' },
  { value: 'ru-RU', label: 'Русский', shortLabel: 'RU', flag: '🇷🇺' },
  { value: 'ko-KR', label: '한국어', shortLabel: '한국어', flag: '🇰🇷' },
  { value: 'ja-JP', label: '日本語', shortLabel: '日本語', flag: '🇯🇵' }
]

const currentLanguage = computed(() => locale.value)
const oidcLoginText = computed(() => {
  if (oidcProviderName.value) {
    return t('auth.oidcLoginWithProvider', { provider: oidcProviderName.value })
  }
  return t('auth.oidcLogin')
})
const currentLangOption = computed(() => languageOptions.find(l => l.value === currentLanguage.value))

// Login form data
const formData = reactive<{ [key: string]: any }>({
  email: '',
  password: '',
})

// Register form data
const registerData = reactive<{ [key: string]: any }>({
  username: '',
  email: '',
  password: '',
  confirmPassword: ''
})

// Login form validation rules
const formRules = computed(() => ({
  email: [
    { required: true, message: t('auth.emailRequired'), type: 'error' },
    { email: true, message: t('auth.emailInvalid'), type: 'error' }
  ],
  password: [
    { required: true, message: t('auth.passwordRequired'), type: 'error' },
    { min: 8, message: t('auth.passwordMinLength'), type: 'error' },
    { max: 32, message: t('auth.passwordMaxLength'), type: 'error' }
  ],
}))

// Register form validation rules
const registerRules = computed(() => ({
  username: [
    { required: true, message: t('auth.usernameRequired'), type: 'error' },
    { min: 2, message: t('auth.usernameMinLength'), type: 'error' },
    { max: 20, message: t('auth.usernameMaxLength'), type: 'error' },
    {
      pattern: /^[a-zA-Z0-9_\u4e00-\u9fa5]+$/,
      message: t('auth.usernameInvalid'),
      type: 'error'
    }
  ],
  email: [
    { required: true, message: t('auth.emailRequired'), type: 'error' },
    { email: true, message: t('auth.emailInvalid'), type: 'error' }
  ],
  password: newPasswordRules(t, complexPasswordEnabled.value),
  confirmPassword: [
    { required: true, message: t('auth.confirmPasswordRequired'), type: 'error' },
    {
      validator: (val: string) => val === registerData.password,
      message: t('auth.passwordMismatch'),
      type: 'error'
    }
  ]
}))

// Toggle login/register mode
const toggleMode = () => {
  isRegisterMode.value = !isRegisterMode.value && registrationEnabled.value

  Object.keys(registerData).forEach(key => {
    (registerData as any)[key] = ''
  })
}

// Toggle language menu
const toggleLanguageMenu = () => {
  showLanguageMenu.value = !showLanguageMenu.value
}

// Select language
const selectLanguage = (lang: string) => {
  locale.value = lang
  localStorage.setItem('locale', lang)
  showLanguageMenu.value = false
  MessagePlugin.success(t('language.languageSaved'))
}

// Close language menu when clicking outside
const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (!target.closest('.language-switch')) {
    showLanguageMenu.value = false
  }
}

// Add click outside listener
onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})

const persistLoginResponse = async (response: any, skipRedirect = false) => {
  // Backend renamed `tenant` to `active_tenant` and added `memberships`
  // when tenant-level RBAC landed (issue #1303). The two are otherwise
  // identical — `active_tenant` is the tenant whose ID is encoded in the
  // JWT, defaulting to the user's home tenant on a fresh login.
  const activeTenant = response.active_tenant || response.tenant
  if (response.user && response.token) {
    // user.tenant_id must be the user's HOME tenant (the immutable row
    // on the users table); useHomeTenant() and the home-badge logic both
    // assume so. The ACTIVE tenant (which can differ from home when the
    // server honoured a remembered last-active-tenant preference) is
    // expressed separately via setSelectedTenant below.
    const homeTenantIdRaw = response.user.tenant_id ?? activeTenant?.id ?? ''
    authStore.setUser(userInfoFromApi(response.user, homeTenantIdRaw))
    authStore.setToken(response.token)
    if (response.refresh_token) {
      authStore.setRefreshToken(response.refresh_token)
    }
    if (activeTenant) {
      authStore.setTenant({
        id: String(activeTenant.id) || '',
        name: activeTenant.name || '',
        owner_id: response.user.id || '',
        created_at: activeTenant.created_at || new Date().toISOString(),
        updated_at: activeTenant.updated_at || new Date().toISOString()
      })
    } else {
      authStore.setTenant(null)
    }
    if (Array.isArray(response.memberships)) {
      authStore.setMemberships(response.memberships)
    }
    // If the backend dropped us into a non-home tenant (honoured a
    // remembered "last active tenant" preference), set the override so
    // subsequent requests carry X-Tenant-ID and the UI stays consistent.
    // Otherwise clear any stale override left in localStorage by a
    // previous session for a different account.
    const activeIdNum = Number(activeTenant?.id)
    const homeIdNum = Number(homeTenantIdRaw)
    if (Number.isFinite(activeIdNum) && Number.isFinite(homeIdNum) && activeIdNum !== homeIdNum) {
      authStore.setSelectedTenant(activeIdNum, activeTenant?.name || null)
    } else {
      authStore.setSelectedTenant(null, null)
    }
  }

  // Pull runtime capabilities (including whether ordinary users may create
  // workspaces) before entering the main UI so create actions never flash
  // briefly when the deployment is invitation-only.
  await authStore.refreshFromAuthMe()
  await nextTick()
  if (skipRedirect) return
  router.replace(authStore.hasValidTenant ? '/platform/research' : '/onboarding/workspace')
}

const getBackendOIDCRedirectURI = () => `${window.location.origin}/api/v1/auth/oidc/callback`

const loadOIDCConfig = async () => {
  try {
    const response = await getOIDCConfig()
    oidcEnabled.value = !!response.success && !!response.enabled
    oidcProviderName.value = response.provider_display_name || ''
  } catch {
    oidcEnabled.value = false
    oidcProviderName.value = ''
  }
}

// Registration stays hidden if configuration cannot be loaded.
const loadAuthConfig = async () => {
  try {
    const response = await getAuthConfig()
    registrationMode.value = response.success ? response.registration_mode : ''
    complexPasswordEnabled.value = response.complex_password_enabled
  } catch {
    registrationMode.value = ''
    complexPasswordEnabled.value = false
  }
}

const handleOIDCLogin = async () => {
  try {
    oidcLoading.value = true
    const response = await getOIDCAuthorizationURL(getBackendOIDCRedirectURI())
    const authorizationURL = response.authorization_url

    if (!response.success || !authorizationURL) {
      MessagePlugin.error(response.message || t('auth.oidcLoginFailed'))
      return
    }

    // 跳转 IdP 会丢失 URL 中的 token，暂存到 sessionStorage，回调后由 App.vue 兑换。
    if (inviteToken.value) {
      sessionStorage.setItem('weknora_pending_invite_token', inviteToken.value)
    }
    window.location.href = authorizationURL
  } catch (error: any) {
    console.error('OIDC 登录跳转失败:', error)
    MessagePlugin.error(error.message || t('auth.oidcLoginFailed'))
  } finally {
    oidcLoading.value = false
  }
}

// 用 token 加入空间并进入应用。会话此时已有效，故即便 token 失效也照常进入（避免困在登录页）。
const acceptAndEnter = async (token: string) => {
  loading.value = true
  try {
    const result = await authStore.acceptInvitationByTokenAndRefresh(token)
    if (result.ok) {
      MessagePlugin.success(t('inviteRegister.joined'))
    } else {
      MessagePlugin.warning(t('inviteRegister.invalidBody'))
    }
  } catch {
    MessagePlugin.warning(t('inviteRegister.invalidBody'))
  } finally {
    loading.value = false
    await nextTick()
    router.replace('/platform/research')
  }
}

// Handle login
const handleLogin = async () => {
  if (loading.value || oidcLoading.value) return
  loading.value = true
  try {
    const valid = await formRef.value?.validate()
    if (valid !== true) return

    const response = await login({
      email: formData.email,
      password: formData.password,
    })

    if (response.success) {
      if (inviteToken.value) {
        // 从邀请链接登录：持久化会话后兑换 token 并进入对应空间。
        await persistLoginResponse(response, true)
        await acceptAndEnter(inviteToken.value)
        return
      }
      await persistLoginResponse(response)
      notifyLoginSuccess(response, t, tm, formatRole, roleIcon)
    } else {
      MessagePlugin.error(response.message || t('auth.loginError'))
    }
  } catch (error: any) {
    console.error('登录错误:', error)
    MessagePlugin.error(error.message || t('auth.loginErrorRetry'))
  } finally {
    loading.value = false
  }
}

// Handle registration. Dispatches based on whether the user arrived
// with a share-link token: with token -> register-by-invite (auto-
// login on success); without -> the normal self-service register
// (drops back to the login form for the user to sign in).
const handleRegister = async () => {
  if (!registrationEnabled.value || loading.value || oidcLoading.value) return
  loading.value = true
  try {
    const valid = await registerFormRef.value?.validate()
    if (valid !== true) return

    if (inviteToken.value) {
      const response = await registerByInvite({
        token: inviteToken.value,
        username: registerData.username,
        email: registerData.email,
        password: registerData.password,
      })
      if (!response.success) {
        MessagePlugin.error(response.message || t('auth.registerFailed'))
        return
      }
      MessagePlugin.success(t('auth.registerSuccess'))
      // register-by-invite returns the same shape as login (token +
      // active_tenant + memberships), so reuse the login persistence
      // path — same store writes, same redirect target.
      await persistLoginResponse(response)
      return
    }

    const response = await register({
      username: registerData.username,
      email: registerData.email,
      password: registerData.password
    })

    if (response.success) {
      MessagePlugin.success(t('auth.registerSuccess'))

      // Switch to login mode and fill in email
      isRegisterMode.value = false
      formData.email = registerData.email

      // Clear register form
      Object.keys(registerData).forEach(key => {
        (registerData as any)[key] = ''
      })
    } else {
      MessagePlugin.error(response.message || t('auth.registerFailed'))
    }
  } catch (error: any) {
    console.error('注册错误:', error)
    MessagePlugin.error(error.message || t('auth.registerError'))
  } finally {
    loading.value = false
  }
}

// Check if already logged in; for lite edition, attempt transparent auto-setup
onMounted(async () => {
  // Share-link landing: ?token=xxx switches the form into invite-
  // register mode before any other auto-flow (logged-in redirect /
  // auto-setup / OIDC) gets a chance to redirect. Resolution failure
  // surfaces inline; the user can still log in normally if they
  // already have an account. We check this BEFORE the isLoggedIn
  // redirect so an existing session doesn't bounce the user to
  // /platform (and possibly back to /login if the session is stale),
  // dropping the invite token along the way.
  const tokenFromQuery = String(route.query.token || '').trim()
  if (tokenFromQuery) {
    inviteToken.value = tokenFromQuery
    inviteLookupLoading.value = true
    // 1. 先校验 token：无效/过期则停在登录页报错，不进注册模式。
    try {
      const resp = await getInvitationByToken(tokenFromQuery)
      if (resp.success && resp.data) {
        inviteLookup.value = resp.data
      } else {
        inviteLookupError.value = resp.message || t('inviteRegister.invalidBody')
        loadOIDCConfig()
        loadAuthConfig()
        return
      }
    } catch {
      inviteLookupError.value = t('inviteRegister.invalidBody')
      loadOIDCConfig()
      loadAuthConfig()
      return
    } finally {
      inviteLookupLoading.value = false
    }

    // 2. 已登录则直接兑换 token 进入空间（三种模式通用）。
    if (authStore.isLoggedIn && (await authStore.refreshFromAuthMe())) {
      await acceptAndEnter(tokenFromQuery)
      return
    }

    // 3. Only enabled registration modes may create an invited account.
    await loadAuthConfig()
    isRegisterMode.value = registrationEnabled.value
    loadOIDCConfig()
    return
  }

  if (authStore.isLoggedIn) {
    router.replace('/platform/research')
    return
  }

  localStorage.removeItem('weknora_auto_setup_failed')
  try {
    const response = await autoSetup()
    if (response.success) {
      authStore.setLiteMode(true)
      await persistLoginResponse(response)
      return
    }
  } catch {
    // Auto-setup may be unavailable outside the native Lite shell.
  }

  loadOIDCConfig()
  loadAuthConfig()
})
</script>

<style lang="less" scoped>
.login-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(380px, 1fr);
  width: 100%;
  min-height: 100vh;
  min-height: 100dvh;
  position: relative;
  background: var(--td-bg-color-page);
  color: var(--td-text-color-primary);
}

.header-logo {
  position: absolute;
  top: 30px;
  left: 5%;
  display: inline-flex;
  z-index: var(--z-sticky);
  .logo-image { width: 118px; height: auto; }
}

:global(html[theme-mode="dark"]) .header-logo .logo-image { filter: brightness(0) invert(1); opacity: 0.9; }

.header-links {
  position: absolute;
  top: 24px;
  right: 4%;
  display: flex;
  align-items: center;
  gap: 8px;
  z-index: var(--z-dropdown);
}

.header-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 36px;
  padding: 7px 10px;
  border: 1px solid transparent;
  border-radius: var(--app-radius-md);
  background: transparent;
  color: var(--td-text-color-secondary);
  font: inherit;
  font-size: var(--app-text-md);
  text-decoration: none;
  cursor: pointer;
  transition: background var(--app-motion-fast), color var(--app-motion-fast);
  &:hover { background: var(--app-surface-muted); color: var(--td-text-color-primary); }
}

.language-switch { position: relative; }
.language-dropdown {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  min-width: 168px;
  padding: 5px;
  border: 1px solid var(--app-border-subtle);
  border-radius: var(--app-radius-lg);
  background: var(--app-surface-raised);
  box-shadow: var(--app-shadow-md);
}
.language-option {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border: 0;
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: var(--app-text-md);
  text-align: left;
  cursor: pointer;
  .lang-label { flex: 1; }
  &:hover { background: var(--app-surface-muted); }
  &.active { background: var(--td-brand-color-light); color: var(--td-text-color-brand); }
}

.showcase-section {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 128px 48px 80px;
  border-right: 1px solid var(--app-border-subtle);
  background: var(--app-surface-muted);
}
.showcase-content { width: 100%; max-width: 400px; }
.showcase-mark {
  display: grid;
  place-items: center;
  width: 58px;
  height: 58px;
  margin-bottom: 28px;
  border: 1px solid var(--app-border-subtle);
  border-radius: var(--app-radius-xl);
  background: var(--app-surface-raised);
  color: var(--td-text-color-brand);
  box-shadow: var(--app-shadow-sm);
}
.showcase-subtitle {
  margin: 0;
  font-size: var(--app-text-4xl);
  font-weight: 600;
  line-height: 1.5;
  letter-spacing: -0.025em;
}
.showcase-description {
  margin: 14px 0 36px;
  max-width: 360px;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-lg);
  line-height: 1.8;
}
.showcase-features { border-top: 1px solid var(--app-border-subtle); }
.showcase-feature {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: 22px 0;
  border-bottom: 1px solid var(--app-border-subtle);
  > .t-icon { margin-top: 3px; color: var(--td-text-color-brand); flex-shrink: 0; }
  h2 { margin: 0; font-size: var(--app-text-base); font-weight: 600; line-height: 1.6; }
  p { margin: 5px 0 0; color: var(--td-text-color-secondary); font-size: var(--app-text-md); line-height: 1.65; }
}

.form-section {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 112px 40px 72px;
  min-width: 0;
}
.form-panel { width: 100%; max-width: 420px; }
.form-card {
  padding: 36px;
  border: 1px solid var(--app-border-subtle);
  border-radius: var(--app-radius-xl);
  background: var(--app-surface-raised);
  box-shadow: var(--app-shadow-md);
}
.form-header { margin-bottom: 28px; text-align: left; }
.form-title { margin: 0 0 10px; font-size: var(--app-text-4xl); font-weight: 600; line-height: 1.4; }
.form-welcome, .form-subtitle { margin: 0; color: var(--td-text-color-secondary); font-size: var(--app-text-base); line-height: 1.65; }
.form-content {
  :deep(.t-form-item__label) { margin-bottom: 8px; color: var(--td-text-color-primary); font-size: var(--app-text-base); font-weight: 500; }
  :deep(.t-form-item) { margin-bottom: 20px; }
  :deep(.t-input) { border-radius: var(--app-radius-md); background: var(--app-surface-raised); }
  :deep(.t-input:focus-within) { box-shadow: 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 12%, transparent); }
  :deep(.t-input__inner) { font-size: var(--app-text-base); }
  :deep(.t-form-item__control) { width: 100%; }
}
.submit-button, .register-cta__button, .oidc-button {
  height: 44px;
  border-radius: var(--app-radius-md);
  font-size: var(--app-text-base);
  font-weight: 500;
}
.submit-button { margin: 4px 0 0; }
.register-cta { margin-top: 24px; }
.register-cta__divider, .oidc-divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0 0 14px;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  line-height: 1.5;
  &::before, &::after { content: ''; flex: 1; border-top: 1px solid var(--app-border-subtle); }
}
.oidc-divider { margin-top: 24px; }
.form-footer {
  margin-top: 24px;
  text-align: center;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
  .link-button { margin-left: 4px; color: var(--td-text-color-brand); text-decoration: none; font-weight: 500; }
  .link-button:hover { text-decoration: underline; }
}
.invite-banner {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 24px;
  padding: 12px 14px;
  border: 1px solid var(--app-border-subtle);
  border-radius: var(--app-radius-md);
  background: var(--app-surface-muted);
  font-size: var(--app-text-md);
  line-height: 1.6;
  &__icon { margin-top: 2px; flex-shrink: 0; color: var(--td-text-color-brand); }
  &__title { font-weight: 600; }
  &__hint { color: var(--td-text-color-secondary); }
  &--error { background: var(--td-error-color-light); border-color: var(--td-error-color-3); color: var(--td-error-color); }
}
button:focus-visible, a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }

@media (max-width: 960px) {
  .showcase-section { padding: 112px 32px 60px; }
  .form-section { padding: 112px 24px 60px; }
  .form-card { padding: 28px; }
  .header-links > .header-link .link-text { display: none; }
}
@media (max-width: 720px) {
  .login-layout { grid-template-columns: minmax(0, 1fr); }
  .header-logo { top: 26px; left: 24px; .logo-image { width: 104px; } }
  .header-links { top: 20px; right: 16px; gap: 2px; }
  .showcase-section { display: none; }
  .form-section { padding: 104px 24px 40px; }
  .form-panel { max-width: 420px; }
}
@media (max-width: 380px) {
  .form-section { padding-inline: 16px; }
  .form-card { padding: 24px 20px; }
  .header-links > .header-link { display: none; }
}
@media (prefers-reduced-motion: reduce) {
  .header-link { transition: none; }
}
</style>
