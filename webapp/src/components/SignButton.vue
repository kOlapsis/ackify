<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<template>
  <div class="w-full">
    <!-- Sign Button -->
    <button
      v-if="!isSigned"
      @click="handleSign"
      :disabled="isButtonDisabled"
      data-testid="sign-button"
      :class="[
        'w-full sm:w-auto inline-flex items-center justify-center gap-2 px-6 py-3 text-base font-medium rounded-lg transition-all min-h-[48px]',
        isButtonDisabled
          ? 'bg-slate-300 dark:bg-slate-700 text-slate-500 dark:text-slate-400 cursor-not-allowed'
          : 'trust-gradient text-white hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-slate-900 focus:ring-offset-2 dark:focus:ring-offset-slate-900'
      ]"
      type="button"
    >
      <!-- Spinner -->
      <svg
        v-if="loading"
        class="animate-spin h-5 w-5"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
      >
        <circle
          class="opacity-25"
          cx="12"
          cy="12"
          r="10"
          stroke="currentColor"
          stroke-width="4"
        />
        <path
          class="opacity-75"
          fill="currentColor"
          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
        />
      </svg>
      <!-- Pen Icon -->
      <svg
        v-else
        class="h-5 w-5"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"
        />
      </svg>
      <span>{{ buttonLabel }}</span>
    </button>

    <!-- Signed Status -->
    <div v-else data-testid="sign-success" class="bg-emerald-50 dark:bg-emerald-900/20 border-2 border-emerald-200 dark:border-emerald-800 rounded-xl p-4 sm:p-5">
      <div class="flex items-center justify-center gap-3">
        <div class="w-10 h-10 rounded-full verified-gradient flex items-center justify-center flex-shrink-0">
          <svg
            class="h-5 w-5 text-white"
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2.5"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M5 13l4 4L19 7"
            />
          </svg>
        </div>
        <div class="text-center sm:text-left">
          <p class="font-semibold text-emerald-700 dark:text-emerald-400">
            {{ $t('signButton.confirmed') }}
          </p>
          <p v-if="signedAt" class="text-sm text-emerald-600 dark:text-emerald-500 mt-0.5">
            {{ $t('signButton.on') }} {{ formatDate(signedAt) }}
          </p>
        </div>
      </div>
    </div>

    <!-- Error Message -->
    <div v-if="error" class="mt-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-4 py-3">
      <p class="text-red-600 dark:text-red-400 text-sm text-center">{{ error }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSignatureStore } from '@/stores/signatures'
import { useAuthStore } from '@/stores/auth'

interface Signature {
  userEmail: string
  signedAt: string
}

interface Props {
  docId?: string
  referer?: string
  disabled?: boolean
  signatures?: Signature[]
}

const props = defineProps<Props>()

const emit = defineEmits<{
  signed: [docId: string]
  error: [error: string]
}>()

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const signatureStore = useSignatureStore()
const loading = ref(false)
const error = ref<string | null>(null)
const isSigned = ref(false)
const signedAt = ref<string | null>(null)

const buttonLabel = computed(() => {
  if (loading.value) return t('signButton.signing')
  if (!authStore.isAuthenticated) return t('signButton.mustLogin')
  return t('signButton.confirmAction')
})

// Unauthenticated users should be able to click the button to start the auth
// flow even before the certify checkbox is ticked — they will tick it after login.
const isButtonDisabled = computed(() => {
  if (loading.value || !props.docId) return true
  if (!authStore.isAuthenticated) return false
  return !!props.disabled
})

// Check if current user has signed based on signatures list
async function checkIfSigned() {
  // Initialize auth store if not already done (for public pages)
  if (!authStore.initialized) {
    try {
      await authStore.checkAuth()
    } catch {
      // Ignore errors - user is not authenticated
    }
  }

  if (!props.signatures || !authStore.user?.email) {
    isSigned.value = false
    signedAt.value = null
    return
  }

  const userSignature = props.signatures.find(
    sig => sig.userEmail === authStore.user?.email
  )

  isSigned.value = !!userSignature
  signedAt.value = userSignature?.signedAt || null
}

// Watch for changes in signatures prop or auth state
watch(() => [props.signatures, authStore.user], checkIfSigned, { immediate: true, deep: true })

async function handleSign() {
  if (!props.docId) {
    error.value = t('signButton.error.missingDocId')
    return
  }

  // Check if user is authenticated
  if (!authStore.initialized) {
    try {
      await authStore.checkAuth()
    } catch {
      // Ignore errors - user is not authenticated
    }
  }

  // If not authenticated, send the user to the auth chooser so they can pick
  // any enabled method (OAuth and/or MagicLink) and come back to the document.
  if (!authStore.isAuthenticated) {
    const redirect = window.location.pathname + window.location.search
    await router.push({ name: 'auth-choice', query: { redirect } })
    return
  }

  loading.value = true
  error.value = null

  try {
    await signatureStore.createSignature({
      docId: props.docId,
      referer: props.referer,
    })

    isSigned.value = true
    signedAt.value = new Date().toISOString()
    emit('signed', props.docId)
  } catch (err: any) {
    const errorMessage =
      err.response?.data?.error?.message || 'Impossible de confirmer la lecture'
    error.value = errorMessage
    emit('error', errorMessage)
  } finally {
    loading.value = false
  }
}

function formatDate(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString('fr-FR', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>
