// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises, VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SignButton from '@/components/SignButton.vue'
import { useAuthStore } from '@/stores/auth'
import { useSignatureStore } from '@/stores/signatures'
import { createI18n } from 'vue-i18n'

vi.mock('@/services/http')

const pushMock = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock })
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      signButton: {
        signing: 'Signing...',
        confirmAction: 'Sign Document',
        mustLogin: 'Sign in to confirm',
        confirmed: 'Signed',
        on: 'on',
        error: {
          missingDocId: 'Missing document ID',
          authFailed: 'Authentication failed'
        }
      }
    }
  }
})

describe('SignButton Component', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    pushMock.mockReset()
    delete (window as any).location
    ;(window as any).location = {
      href: '',
      pathname: '/document/123',
      search: '?test=true'
    }
  })

  const mountComponent = (props = {}): VueWrapper => {
    return mount(SignButton, {
      props,
      global: {
        plugins: [i18n]
      }
    })
  }

  describe('Business logic - Signature state', () => {
    it('should detect when current user has signed', async () => {
      const authStore = useAuthStore()
      authStore.initialized = true
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        signatures: [
          {
            userEmail: 'test@example.com',
            signedAt: '2024-01-15T10:00:00Z'
          }
        ]
      })

      await flushPromises()

      // Should show signed status (no button visible)
      expect(wrapper.find('button').exists()).toBe(false)
      // Should display signed confirmation (check i18n key or translated text)
      expect(wrapper.text()).toMatch(/Signed|signButton\.confirmed/)
    })

    it('should not show signed status when different user has signed', async () => {
      const authStore = useAuthStore()
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        signatures: [
          {
            userEmail: 'other@example.com',
            signedAt: '2024-01-15T10:00:00Z'
          }
        ]
      })

      await flushPromises()

      // Should show button since user hasn't signed
      expect(wrapper.find('button').exists()).toBe(true)
    })

    it('should detect signature change after successful sign action', async () => {
      const authStore = useAuthStore()
      const signatureStore = useSignatureStore()

      authStore.initialized = true
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      vi.spyOn(signatureStore, 'createSignature').mockResolvedValueOnce({
        id: 1,
        docId: 'doc-123',
        userSub: 'user-123',
        userEmail: 'test@example.com',
        signedAt: '2024-01-15T10:00:00Z',
        payloadHash: 'hash123',
        signature: 'sig123',
        nonce: 'nonce123',
        createdAt: '2024-01-15T10:00:00Z'
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        signatures: []
      })

      await flushPromises()
      expect(wrapper.find('button').exists()).toBe(true)

      // Sign the document
      await wrapper.find('button').trigger('click')
      await flushPromises()

      // After signing, button should disappear (isSigned becomes true)
      expect(wrapper.find('button').exists()).toBe(false)
    })
  })

  describe('Business logic - Signature creation', () => {
    it('should create signature when user is authenticated', async () => {
      const authStore = useAuthStore()
      const signatureStore = useSignatureStore()

      authStore.initialized = true
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      const createSignatureSpy = vi.spyOn(signatureStore, 'createSignature').mockResolvedValueOnce({
        id: 1,
        docId: 'doc-123',
        userSub: 'user-123',
        userEmail: 'test@example.com',
        userName: 'Test User',
        signedAt: '2024-01-15T10:00:00Z',
        payloadHash: 'hash123',
        signature: 'sig123',
        nonce: 'nonce123',
        createdAt: '2024-01-15T10:00:00Z'
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        referer: 'https://example.com',
        signatures: []
      })

      await wrapper.find('button').trigger('click')
      await flushPromises()

      expect(createSignatureSpy).toHaveBeenCalledWith({
        docId: 'doc-123',
        referer: 'https://example.com'
      })
    })

    it('should emit signed event on successful signature', async () => {
      const authStore = useAuthStore()
      const signatureStore = useSignatureStore()

      authStore.initialized = true
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      vi.spyOn(signatureStore, 'createSignature').mockResolvedValueOnce({
        id: 1,
        docId: 'doc-123',
        userSub: 'user-123',
        userEmail: 'test@example.com',
        signedAt: '2024-01-15T10:00:00Z',
        payloadHash: 'hash123',
        signature: 'sig123',
        nonce: 'nonce123',
        createdAt: '2024-01-15T10:00:00Z'
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        signatures: []
      })

      await wrapper.find('button').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('signed')).toBeTruthy()
      expect(wrapper.emitted('signed')?.[0]).toEqual(['doc-123'])
    })

    it('should emit error event on signature creation failure', async () => {
      const authStore = useAuthStore()
      const signatureStore = useSignatureStore()

      authStore.initialized = true
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      vi.spyOn(signatureStore, 'createSignature').mockRejectedValueOnce({
        response: {
          data: {
            error: {
              message: 'You have already signed this document'
            }
          }
        }
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        signatures: []
      })

      await wrapper.find('button').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('error')).toBeTruthy()
      expect(wrapper.emitted('error')?.[0]).toEqual(['You have already signed this document'])
    })
  })

  describe('Business logic - Authentication requirement', () => {
    it('should redirect to the auth chooser page when not authenticated', async () => {
      const authStore = useAuthStore()
      authStore.initialized = true

      const wrapper = mountComponent({
        docId: 'doc-123',
        signatures: []
      })

      await wrapper.find('button').trigger('click')
      await flushPromises()

      expect(pushMock).toHaveBeenCalledWith({
        name: 'auth-choice',
        query: { redirect: '/document/123?test=true' }
      })
    })

    it('should stay clickable when not authenticated even if disabled prop is true', async () => {
      const authStore = useAuthStore()
      authStore.initialized = true

      const wrapper = mountComponent({
        docId: 'doc-123',
        disabled: true,
        signatures: []
      })

      const button = wrapper.find('button')
      expect(button.attributes('disabled')).toBeUndefined()

      await button.trigger('click')
      await flushPromises()

      expect(pushMock).toHaveBeenCalledTimes(1)
    })
  })

  describe('Button state', () => {
    it('should disable button when no docId provided', () => {
      const wrapper = mountComponent({
        signatures: []
      })

      const button = wrapper.find('button')
      expect(button.attributes('disabled')).toBeDefined()
    })

    it('should disable button when authenticated and disabled prop is true', () => {
      const authStore = useAuthStore()
      authStore.initialized = true
      authStore.setUser({
        id: 'user-123',
        email: 'test@example.com',
        name: 'Test User',
        isAdmin: false
      })

      const wrapper = mountComponent({
        docId: 'doc-123',
        disabled: true,
        signatures: []
      })

      const button = wrapper.find('button')
      expect(button.attributes('disabled')).toBeDefined()
    })
  })
})
