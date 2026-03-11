// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import http, { resetCsrfToken } from '@/services/http'
import { useConfigStore } from '@/stores/config'

export interface User {
  id: string
  email: string
  name: string
  picture?: string
  isAdmin: boolean
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const loading = ref(false)
  const initialized = ref(false)
  const configStore = useConfigStore()

  const isAuthenticated = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.isAdmin ?? false)

  const canCreateDocuments = computed(() => {
    if (isAdmin.value) return true
    if (configStore.onlyAdminCanCreate) return false
    const domain = configStore.organisationDomain
    if (domain && user.value?.email) {
      return user.value.email.toLowerCase().endsWith('@' + domain.toLowerCase())
    }
    return true
  })

  async function checkAuth() {
    if (initialized.value) return

    loading.value = true
    try {
      const response = await http.get('/users/me')
      user.value = response.data.data
    } catch (error) {
      user.value = null
    } finally {
      loading.value = false
      initialized.value = true
    }
  }

  async function fetchCurrentUser() {
    try {
      const response = await http.get('/users/me')
      user.value = response.data.data
    } catch (error) {
      console.error('Failed to fetch user info:', error)
    }
  }

  async function startOAuthLogin(redirectTo?: string) {
    try {
      console.log('Starting OAuth login...', { redirectTo })
      const response = await http.post('/auth/start', { redirectTo })
      console.log('OAuth response:', response.data)

      if (response.data.data?.redirectUrl) {
        console.log('Redirecting to:', response.data.data.redirectUrl)
        window.location.href = response.data.data.redirectUrl
      } else if (response.data.redirectUrl) {
        console.log('Redirecting to:', response.data.redirectUrl)
        window.location.href = response.data.redirectUrl
      } else {
        console.error('No redirect URL in response:', response.data)
      }
    } catch (error) {
      console.error('OAuth login error:', error)
      throw error
    }
  }

  async function logout() {
    try {
      const response = await http.get('/auth/logout')
      user.value = null
      resetCsrfToken()

      if (response.data.redirectUrl) {
        window.location.href = response.data.redirectUrl
      } else {
        window.location.href = '/'
      }
    } catch (error) {
      console.error('Logout failed:', error)
      user.value = null
      window.location.href = '/'
    }
  }

  function setUser(userData: User) {
    user.value = userData
  }

  return {
    user,
    loading,
    initialized,
    isAuthenticated,
    isAdmin,
    canCreateDocuments,
    checkAuth,
    fetchCurrentUser,
    startOAuthLogin,
    logout,
    setUser,
  }
})