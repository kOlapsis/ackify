<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePageTitle } from '@/composables/usePageTitle'
import {
  getSettings,
  updateSection,
  testConnection,
  resetFromENV,
  isSecretMasked,
  getOIDCProviderURLs,
  type SettingsResponse,
  type GeneralConfig,
  type OIDCConfig,
  type SMTPConfig,
  type StorageConfig,
  type ConfigSection
} from '@/services/settings'
import { extractError } from '@/services/http'
import {
  Settings,
  Shield,
  Mail,
  HardDrive,
  Loader2,
  Save,
  RefreshCw,
  TestTube2,
  AlertCircle,
  CheckCircle,
  ChevronRight,
  Link
} from 'lucide-vue-next'

const { t } = useI18n()
usePageTitle('admin.settings.title')

// State
const settings = ref<SettingsResponse | null>(null)
const loading = ref(true)
const saving = ref(false)
const testing = ref<string | null>(null)
const error = ref('')
const success = ref('')
const activeSection = ref<ConfigSection>('general')
const showResetConfirm = ref(false)

// Edit states for each section
const editGeneral = ref<GeneralConfig>({ organisation: '', organisation_domain: '', only_admin_can_create: false, allowed_domains: [] })
const allowedDomainsText = ref('')
const editOIDC = ref<OIDCConfig>({
  enabled: false, provider: '', client_id: '', client_secret: '',
  auth_url: '', token_url: '', userinfo_url: '', logout_url: '',
  scopes: [], allowed_domain: '', auto_login: false
})
const editMagicLink = ref<{ enabled: boolean }>({ enabled: false })
const editSMTP = ref<SMTPConfig>({
  host: '', port: 587, username: '', password: '',
  tls: false, starttls: true, insecure_skip_verify: false,
  timeout: '10s', from: '', from_name: '', subject_prefix: ''
})
const editStorage = ref<StorageConfig>({
  type: '', max_size_mb: 50, local_path: '/data/documents',
  s3_endpoint: '', s3_bucket: '', s3_access_key: '', s3_secret_key: '',
  s3_region: 'us-east-1', s3_use_ssl: true
})

// Section navigation
const sections = computed(() => [
  { id: 'general' as ConfigSection, icon: Settings, label: t('admin.settings.sections.general') },
  { id: 'oidc' as ConfigSection, icon: Shield, label: t('admin.settings.sections.oidc') },
  { id: 'magiclink' as ConfigSection, icon: Link, label: t('admin.settings.sections.magiclink') },
  { id: 'smtp' as ConfigSection, icon: Mail, label: t('admin.settings.sections.smtp') },
  { id: 'storage' as ConfigSection, icon: HardDrive, label: t('admin.settings.sections.storage') }
])

// Load settings
async function loadSettings() {
  try {
    loading.value = true
    error.value = ''
    const response = await getSettings()
    settings.value = response.data
    // Initialize edit states
    editGeneral.value = { ...response.data.general, allowed_domains: response.data.general.allowed_domains || [] }
    allowedDomainsText.value = (response.data.general.allowed_domains || []).join('\n')
    editOIDC.value = { ...response.data.oidc }
    editMagicLink.value = { enabled: response.data.magiclink.enabled }
    editSMTP.value = { ...response.data.smtp }
    editStorage.value = { ...response.data.storage }
  } catch (err) {
    error.value = extractError(err)
  } finally {
    loading.value = false
  }
}

// Save section
async function saveSection(section: ConfigSection) {
  try {
    saving.value = true
    error.value = ''
    success.value = ''

    let config: any
    switch (section) {
      case 'general':
        editGeneral.value.allowed_domains = allowedDomainsText.value
          .split('\n')
          .map(d => d.trim())
          .filter(d => d !== '')
        config = editGeneral.value
        break
      case 'oidc': config = editOIDC.value; break
      case 'magiclink': config = editMagicLink.value; break
      case 'smtp': config = editSMTP.value; break
      case 'storage': config = editStorage.value; break
    }

    await updateSection(section, config)
    success.value = t('admin.settings.saveSuccess')
    await loadSettings()
    setTimeout(() => success.value = '', 3000)
  } catch (err) {
    error.value = extractError(err)
  } finally {
    saving.value = false
  }
}

// Test connection
async function testConnectionHandler(type: 'smtp' | 's3' | 'oidc') {
  try {
    testing.value = type
    error.value = ''
    success.value = ''

    let config: any
    switch (type) {
      case 'smtp': config = editSMTP.value; break
      case 's3': config = editStorage.value; break
      case 'oidc': config = editOIDC.value; break
    }

    await testConnection(type, config)
    success.value = t('admin.settings.testSuccess')
    setTimeout(() => success.value = '', 3000)
  } catch (err) {
    error.value = extractError(err)
  } finally {
    testing.value = null
  }
}

// Reset from ENV
async function handleReset() {
  try {
    saving.value = true
    error.value = ''
    await resetFromENV()
    success.value = t('admin.settings.resetSuccess')
    await loadSettings()
    showResetConfirm.value = false
    setTimeout(() => success.value = '', 3000)
  } catch (err) {
    error.value = extractError(err)
  } finally {
    saving.value = false
  }
}

// OIDC provider change handler
function onOIDCProviderChange() {
  const provider = editOIDC.value.provider
  if (provider && provider !== 'custom') {
    const urls = getOIDCProviderURLs(provider)
    editOIDC.value = { ...editOIDC.value, ...urls }
  }
}

// Check if password field has value (masked or real)
function hasSecretValue(value: string): boolean {
  return value !== '' && !isSecretMasked(value) || isSecretMasked(value)
}

onMounted(loadSettings)
</script>

<template>
  <div class="max-w-6xl mx-auto px-4 sm:px-6 py-6 sm:py-8">
    <!-- Breadcrumb -->
    <nav class="flex items-center gap-2 text-sm mb-6">
      <router-link to="/admin" class="text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 transition-colors">
        {{ t('admin.title') }}
      </router-link>
      <ChevronRight :size="16" class="text-slate-300 dark:text-slate-600" />
      <span class="text-slate-900 dark:text-slate-100 font-medium">
        {{ t('admin.settings.title') }}
      </span>
    </nav>

    <!-- Header -->
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-6 sm:mb-8">
      <div class="flex items-start gap-4">
        <div class="w-12 h-12 sm:w-14 sm:h-14 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center flex-shrink-0">
          <Settings class="w-6 h-6 sm:w-7 sm:h-7 text-blue-600 dark:text-blue-400" />
        </div>
        <div>
          <h1 class="text-xl sm:text-2xl font-bold text-slate-900 dark:text-white">
            {{ t('admin.settings.title') }}
          </h1>
          <p class="text-sm text-slate-500 dark:text-slate-400 mt-1">
            {{ t('admin.settings.subtitle') }}
          </p>
        </div>
      </div>
      <button
        @click="showResetConfirm = true"
        class="w-full sm:w-auto inline-flex items-center justify-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 font-medium rounded-lg px-4 py-2.5 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors"
      >
        <RefreshCw :size="18" />
        {{ t('admin.settings.actions.reset') }}
      </button>
    </div>

    <!-- Alerts -->
    <div v-if="error" class="mb-6 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
      <div class="flex items-start gap-3">
        <AlertCircle :size="20" class="text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />
        <p class="text-red-700 dark:text-red-400 text-sm">{{ error }}</p>
      </div>
    </div>

    <div v-if="success" class="mb-6 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-xl p-4">
      <div class="flex items-start gap-3">
        <CheckCircle :size="20" class="text-emerald-600 dark:text-emerald-400 flex-shrink-0 mt-0.5" />
        <p class="text-emerald-700 dark:text-emerald-400 text-sm">{{ success }}</p>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex items-center justify-center gap-3 py-24">
      <Loader2 :size="32" class="animate-spin text-blue-600" />
      <span class="text-slate-500">{{ t('common.loading') }}</span>
    </div>

    <!-- Main Content -->
    <div v-else class="flex flex-col lg:flex-row gap-6">
      <!-- Sidebar Navigation -->
      <nav class="lg:w-64 flex-shrink-0">
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-2">
          <ul class="space-y-1">
            <li v-for="section in sections" :key="section.id">
              <button
                @click="activeSection = section.id"
                :class="[
                  'w-full flex items-center gap-3 px-4 py-3 rounded-lg text-left transition-colors text-sm font-medium',
                  activeSection === section.id
                    ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
                    : 'text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-700'
                ]"
              >
                <component :is="section.icon" :size="20" />
                {{ section.label }}
              </button>
            </li>
          </ul>
        </div>
      </nav>

      <!-- Content Area -->
      <div class="flex-1 bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700">
        <!-- General Section -->
        <div v-if="activeSection === 'general'" class="p-6">
          <h2 class="text-lg font-semibold text-slate-900 dark:text-white mb-6">
            {{ t('admin.settings.sections.general') }}
          </h2>
          <div class="space-y-6">
            <div>
              <label for="organisation" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                {{ t('admin.settings.general.organisation') }}
              </label>
              <input
                id="organisation"
                data-testid="organisation"
                v-model="editGeneral.organisation"
                type="text"
                class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              />
            </div>
            <div>
              <label for="organisation_domain" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                {{ t('admin.settings.general.organisationDomain') }}
              </label>
              <input
                id="organisation_domain"
                data-testid="organisation_domain"
                v-model="editGeneral.organisation_domain"
                type="text"
                :placeholder="t('admin.settings.general.organisationDomainPlaceholder')"
                class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              />
              <p class="mt-1.5 text-xs text-slate-500 dark:text-slate-400">
                {{ t('admin.settings.general.organisationDomainHelper') }}
              </p>
            </div>
            <div class="flex items-center gap-3">
              <input
                v-model="editGeneral.only_admin_can_create"
                type="checkbox"
                id="only_admin_can_create"
                data-testid="only_admin_can_create"
                class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500"
              />
              <label for="only_admin_can_create" class="text-sm text-slate-700 dark:text-slate-300">
                {{ t('admin.settings.general.onlyAdminCanCreate') }}
              </label>
            </div>
            <div>
              <label for="allowed_domains" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                {{ t('admin.settings.general.allowedDomains') }}
              </label>
              <textarea
                id="allowed_domains"
                data-testid="allowed_domains"
                v-model="allowedDomainsText"
                rows="4"
                :placeholder="t('admin.settings.general.allowedDomainsPlaceholder')"
                class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent font-mono text-sm"
              ></textarea>
              <p class="mt-1.5 text-xs text-slate-500 dark:text-slate-400">
                {{ t('admin.settings.general.allowedDomainsHelper') }}
              </p>
            </div>
          </div>
          <div class="mt-8 flex justify-end">
            <button
              @click="saveSection('general')"
              :disabled="saving"
              class="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg px-6 py-2.5 transition-colors"
            >
              <Loader2 v-if="saving" :size="18" class="animate-spin" />
              <Save v-else :size="18" />
              {{ t('common.save') }}
            </button>
          </div>
        </div>

        <!-- OIDC Section -->
        <div v-if="activeSection === 'oidc'" class="p-6">
          <h2 class="text-lg font-semibold text-slate-900 dark:text-white mb-6">
            {{ t('admin.settings.sections.oidc') }}
          </h2>
          <div class="space-y-6">
            <div class="flex items-center gap-3">
              <input
                v-model="editOIDC.enabled"
                type="checkbox"
                id="oidc_enabled"
                data-testid="oidc_enabled"
                class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500"
              />
              <label for="oidc_enabled" class="text-sm font-medium text-slate-700 dark:text-slate-300">
                {{ t('admin.settings.oidc.enabled') }}
              </label>
            </div>

            <div v-if="editOIDC.enabled" class="space-y-6">
              <div>
                <label for="oidc_provider" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                  {{ t('admin.settings.oidc.provider') }}
                </label>
                <select
                  id="oidc_provider"
                  data-testid="oidc_provider"
                  v-model="editOIDC.provider"
                  @change="onOIDCProviderChange"
                  class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500"
                >
                  <option value="">{{ t('admin.settings.oidc.providerPlaceholder') }}</option>
                  <option value="google">{{ t('admin.settings.oidc.providers.google') }}</option>
                  <option value="github">{{ t('admin.settings.oidc.providers.github') }}</option>
                  <option value="gitlab">{{ t('admin.settings.oidc.providers.gitlab') }}</option>
                  <option value="custom">{{ t('admin.settings.oidc.providers.custom') }}</option>
                </select>
              </div>

              <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label for="oidc_client_id" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                    {{ t('admin.settings.oidc.clientId') }}
                  </label>
                  <input id="oidc_client_id" data-testid="oidc_client_id" v-model="editOIDC.client_id" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label for="oidc_client_secret" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                    {{ t('admin.settings.oidc.clientSecret') }}
                  </label>
                  <input id="oidc_client_secret" data-testid="oidc_client_secret" v-model="editOIDC.client_secret" type="password" :placeholder="hasSecretValue(editOIDC.client_secret) ? '********' : ''" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
              </div>

              <div v-if="editOIDC.provider === 'custom'" class="space-y-4">
                <div>
                  <label for="oidc_auth_url" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.oidc.authUrl') }}</label>
                  <input id="oidc_auth_url" data-testid="oidc_auth_url" v-model="editOIDC.auth_url" type="url" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label for="oidc_token_url" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.oidc.tokenUrl') }}</label>
                  <input id="oidc_token_url" data-testid="oidc_token_url" v-model="editOIDC.token_url" type="url" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label for="oidc_userinfo_url" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.oidc.userinfoUrl') }}</label>
                  <input id="oidc_userinfo_url" data-testid="oidc_userinfo_url" v-model="editOIDC.userinfo_url" type="url" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
              </div>

              <div>
                <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
                  {{ t('admin.settings.oidc.allowedDomain') }}
                </label>
                <input v-model="editOIDC.allowed_domain" type="text" placeholder="@company.com" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>

              <div class="flex items-center gap-3">
                <input v-model="editOIDC.auto_login" type="checkbox" id="oidc_auto_login" data-testid="oidc_auto_login" class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500" />
                <label for="oidc_auto_login" class="text-sm text-slate-700 dark:text-slate-300">{{ t('admin.settings.oidc.autoLogin') }}</label>
              </div>
            </div>
          </div>
          <div class="mt-8 flex flex-wrap gap-3 justify-end">
            <button
              v-if="editOIDC.enabled"
              @click="testConnectionHandler('oidc')"
              :disabled="testing === 'oidc'"
              class="inline-flex items-center gap-2 bg-slate-100 dark:bg-slate-700 hover:bg-slate-200 dark:hover:bg-slate-600 disabled:opacity-50 text-slate-700 dark:text-slate-300 font-medium rounded-lg px-4 py-2.5 transition-colors"
            >
              <Loader2 v-if="testing === 'oidc'" :size="18" class="animate-spin" />
              <TestTube2 v-else :size="18" />
              {{ t('admin.settings.oidc.testConnection') }}
            </button>
            <button @click="saveSection('oidc')" :disabled="saving" class="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg px-6 py-2.5 transition-colors">
              <Loader2 v-if="saving" :size="18" class="animate-spin" />
              <Save v-else :size="18" />
              {{ t('common.save') }}
            </button>
          </div>
        </div>

        <!-- MagicLink Section -->
        <div v-if="activeSection === 'magiclink'" class="p-6">
          <h2 class="text-lg font-semibold text-slate-900 dark:text-white mb-6">{{ t('admin.settings.sections.magiclink') }}</h2>
          <div class="space-y-6">
            <div class="flex items-center gap-3">
              <input v-model="editMagicLink.enabled" type="checkbox" id="magiclink_enabled" data-testid="magiclink_enabled" class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500" />
              <label for="magiclink_enabled" class="text-sm font-medium text-slate-700 dark:text-slate-300">{{ t('admin.settings.magiclink.enabled') }}</label>
            </div>
            <p v-if="editMagicLink.enabled && !editSMTP.host" class="text-amber-600 dark:text-amber-400 text-sm">{{ t('admin.settings.validation.magiclinkRequiresSmtp') }}</p>
          </div>
          <div class="mt-8 flex justify-end">
            <button @click="saveSection('magiclink')" :disabled="saving" class="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg px-6 py-2.5 transition-colors">
              <Loader2 v-if="saving" :size="18" class="animate-spin" />
              <Save v-else :size="18" />
              {{ t('common.save') }}
            </button>
          </div>
        </div>

        <!-- SMTP Section -->
        <div v-if="activeSection === 'smtp'" class="p-6">
          <h2 class="text-lg font-semibold text-slate-900 dark:text-white mb-6">{{ t('admin.settings.sections.smtp') }}</h2>
          <div class="space-y-6">
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label for="smtp_host" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.smtp.host') }}</label>
                <input id="smtp_host" data-testid="smtp_host" v-model="editSMTP.host" type="text" placeholder="smtp.example.com" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>
              <div>
                <label for="smtp_port" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.smtp.port') }}</label>
                <input id="smtp_port" data-testid="smtp_port" v-model.number="editSMTP.port" type="number" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label for="smtp_username" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.smtp.username') }}</label>
                <input id="smtp_username" data-testid="smtp_username" v-model="editSMTP.username" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>
              <div>
                <label for="smtp_password" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.smtp.password') }}</label>
                <input id="smtp_password" data-testid="smtp_password" v-model="editSMTP.password" type="password" :placeholder="hasSecretValue(editSMTP.password) ? '********' : ''" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label for="smtp_from" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.smtp.from') }}</label>
                <input id="smtp_from" data-testid="smtp_from" v-model="editSMTP.from" type="email" placeholder="noreply@example.com" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>
              <div>
                <label for="smtp_from_name" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.smtp.fromName') }}</label>
                <input id="smtp_from_name" data-testid="smtp_from_name" v-model="editSMTP.from_name" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
              </div>
            </div>
            <div class="flex flex-wrap gap-6">
              <div class="flex items-center gap-3">
                <input v-model="editSMTP.tls" type="checkbox" id="smtp_tls" data-testid="smtp_tls" class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500" />
                <label for="smtp_tls" class="text-sm text-slate-700 dark:text-slate-300">{{ t('admin.settings.smtp.tls') }}</label>
              </div>
              <div class="flex items-center gap-3">
                <input v-model="editSMTP.starttls" type="checkbox" id="smtp_starttls" data-testid="smtp_starttls" class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500" />
                <label for="smtp_starttls" class="text-sm text-slate-700 dark:text-slate-300">{{ t('admin.settings.smtp.starttls') }}</label>
              </div>
            </div>
          </div>
          <div class="mt-8 flex flex-wrap gap-3 justify-end">
            <button @click="testConnectionHandler('smtp')" :disabled="testing === 'smtp' || !editSMTP.host" class="inline-flex items-center gap-2 bg-slate-100 dark:bg-slate-700 hover:bg-slate-200 dark:hover:bg-slate-600 disabled:opacity-50 text-slate-700 dark:text-slate-300 font-medium rounded-lg px-4 py-2.5 transition-colors">
              <Loader2 v-if="testing === 'smtp'" :size="18" class="animate-spin" />
              <TestTube2 v-else :size="18" />
              {{ t('admin.settings.smtp.testConnection') }}
            </button>
            <button @click="saveSection('smtp')" :disabled="saving" class="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg px-6 py-2.5 transition-colors">
              <Loader2 v-if="saving" :size="18" class="animate-spin" />
              <Save v-else :size="18" />
              {{ t('common.save') }}
            </button>
          </div>
        </div>

        <!-- Storage Section -->
        <div v-if="activeSection === 'storage'" class="p-6">
          <h2 class="text-lg font-semibold text-slate-900 dark:text-white mb-6">{{ t('admin.settings.sections.storage') }}</h2>
          <div class="space-y-6">
            <div>
              <label for="storage_type" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.type') }}</label>
              <select id="storage_type" data-testid="storage_type" v-model="editStorage.type" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500">
                <option value="">{{ t('admin.settings.storage.types.none') }}</option>
                <option value="local">{{ t('admin.settings.storage.types.local') }}</option>
                <option value="s3">{{ t('admin.settings.storage.types.s3') }}</option>
              </select>
            </div>
            <div>
              <label for="storage_max_size_mb" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.maxSizeMb') }}</label>
              <input id="storage_max_size_mb" data-testid="storage_max_size_mb" v-model.number="editStorage.max_size_mb" type="number" min="1" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
            </div>
            <div v-if="editStorage.type === 'local'">
              <label for="storage_local_path" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.localPath') }}</label>
              <input id="storage_local_path" data-testid="storage_local_path" v-model="editStorage.local_path" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
            </div>
            <div v-if="editStorage.type === 's3'" class="space-y-4">
              <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label for="storage_s3_endpoint" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.s3Endpoint') }}</label>
                  <input id="storage_s3_endpoint" data-testid="storage_s3_endpoint" v-model="editStorage.s3_endpoint" type="text" placeholder="https://s3.amazonaws.com" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label for="storage_s3_bucket" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.s3Bucket') }} *</label>
                  <input id="storage_s3_bucket" data-testid="storage_s3_bucket" v-model="editStorage.s3_bucket" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
              </div>
              <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label for="storage_s3_access_key" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.s3AccessKey') }}</label>
                  <input id="storage_s3_access_key" data-testid="storage_s3_access_key" v-model="editStorage.s3_access_key" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label for="storage_s3_secret_key" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.s3SecretKey') }}</label>
                  <input id="storage_s3_secret_key" data-testid="storage_s3_secret_key" v-model="editStorage.s3_secret_key" type="password" :placeholder="hasSecretValue(editStorage.s3_secret_key || '') ? '********' : ''" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
              </div>
              <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label for="storage_s3_region" class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">{{ t('admin.settings.storage.s3Region') }}</label>
                  <input id="storage_s3_region" data-testid="storage_s3_region" v-model="editStorage.s3_region" type="text" class="w-full px-4 py-2.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500" />
                </div>
                <div class="flex items-center gap-3 pt-8">
                  <input v-model="editStorage.s3_use_ssl" type="checkbox" id="s3_use_ssl" data-testid="s3_use_ssl" class="w-5 h-5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500" />
                  <label for="s3_use_ssl" class="text-sm text-slate-700 dark:text-slate-300">{{ t('admin.settings.storage.s3UseSsl') }}</label>
                </div>
              </div>
            </div>
          </div>
          <div class="mt-8 flex flex-wrap gap-3 justify-end">
            <button v-if="editStorage.type === 's3'" @click="testConnectionHandler('s3')" :disabled="testing === 's3' || !editStorage.s3_bucket" class="inline-flex items-center gap-2 bg-slate-100 dark:bg-slate-700 hover:bg-slate-200 dark:hover:bg-slate-600 disabled:opacity-50 text-slate-700 dark:text-slate-300 font-medium rounded-lg px-4 py-2.5 transition-colors">
              <Loader2 v-if="testing === 's3'" :size="18" class="animate-spin" />
              <TestTube2 v-else :size="18" />
              {{ t('admin.settings.storage.testConnection') }}
            </button>
            <button @click="saveSection('storage')" :disabled="saving" class="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg px-6 py-2.5 transition-colors">
              <Loader2 v-if="saving" :size="18" class="animate-spin" />
              <Save v-else :size="18" />
              {{ t('common.save') }}
            </button>
          </div>
        </div>

      </div>
    </div>

    <!-- Reset Confirmation Modal -->
    <Teleport to="body">
      <div v-if="showResetConfirm" class="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div class="fixed inset-0 bg-black/50" @click="showResetConfirm = false"></div>
        <div class="relative bg-white dark:bg-slate-800 rounded-xl shadow-xl max-w-md w-full p-6">
          <h3 class="text-lg font-semibold text-slate-900 dark:text-white mb-2">{{ t('admin.settings.resetConfirm.title') }}</h3>
          <p class="text-slate-600 dark:text-slate-400 mb-6">{{ t('admin.settings.resetConfirm.message') }}</p>
          <div class="flex justify-end gap-3">
            <button @click="showResetConfirm = false" class="px-4 py-2 text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg transition-colors">
              {{ t('common.cancel') }}
            </button>
            <button @click="handleReset" :disabled="saving" class="px-4 py-2 bg-amber-600 hover:bg-amber-700 text-white rounded-lg transition-colors disabled:opacity-50">
              <Loader2 v-if="saving" :size="18" class="animate-spin inline mr-2" />
              {{ t('admin.settings.resetConfirm.confirm') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
