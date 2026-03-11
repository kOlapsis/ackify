// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export interface AppConfig {
  smtpEnabled: boolean
  storageEnabled: boolean
  onlyAdminCanCreate: boolean
  organisationDomain: string
  oauthEnabled: boolean
  magicLinkEnabled: boolean
}

export const useConfigStore = defineStore('config', () => {
  const config = ref<AppConfig | null>(null)
  const loading = ref(false)
  const initialized = ref(false)
  const error = ref<string | null>(null)

  const smtpEnabled = computed(() => config.value?.smtpEnabled || false)
  const storageEnabled = computed(() => config.value?.storageEnabled || false)
  const onlyAdminCanCreate = computed(() => config.value?.onlyAdminCanCreate || false)
  const organisationDomain = computed(() => config.value?.organisationDomain || '')
  const oauthEnabled = computed(() => config.value?.oauthEnabled || false)
  const magicLinkEnabled = computed(() => config.value?.magicLinkEnabled || false)

  async function loadConfig() {
    if (initialized.value) return

    loading.value = true
    error.value = null

    try {
      const response = await fetch('/api/v1/config')
      if (!response.ok) {
        throw new Error('Failed to load configuration')
      }
      const result = await response.json()
      config.value = result.data || result
      initialized.value = true
    } catch (err: any) {
      error.value = err.message || 'Unknown error'
      console.error('Failed to load app config:', err)
    } finally {
      loading.value = false
    }
  }

  function reset() {
    config.value = null
    initialized.value = false
    error.value = null
  }

  return {
    config,
    loading,
    initialized,
    error,
    smtpEnabled,
    storageEnabled,
    onlyAdminCanCreate,
    organisationDomain,
    oauthEnabled,
    magicLinkEnabled,
    loadConfig,
    reset,
  }
})
