<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePageTitle } from '@/composables/usePageTitle'
import { ShieldCheck, Loader2, AlertCircle } from 'lucide-vue-next'
import AppLogo from '@/components/AppLogo.vue'

const { t } = useI18n()
usePageTitle('auth.reminder.title')

const route = useRoute()

const loading = ref(false)
const errorMessage = ref('')

const token = computed(() => (route.query.token as string) || '')
const docID = computed(() => (route.query.doc as string) || '')

// Fallback destination when the token is missing or already consumed: the recipient
// still reaches the right document (signing there requires a fresh, valid session).
const docFallback = computed(() => (docID.value ? `/?doc=${encodeURIComponent(docID.value)}` : '/'))

onMounted(() => {
  if (!token.value) {
    errorMessage.value = t('auth.reminder.error.message')
  }
})

async function confirm() {
  if (!token.value) {
    errorMessage.value = t('auth.reminder.error.message')
    return
  }

  loading.value = true
  errorMessage.value = ''

  try {
    const response = await fetch('/api/v1/auth/reminder-link/verify', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ token: token.value }),
    })

    if (!response.ok) {
      throw new Error(t('auth.reminder.error.message'))
    }

    const data = await response.json()
    // Full navigation so the freshly-created reminder session is picked up on load.
    window.location.href = data.redirectUrl || docFallback.value
  } catch (error: any) {
    errorMessage.value = error.message || t('auth.reminder.error.message')
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-[calc(100vh-8rem)] flex items-center justify-center px-4 sm:px-6 py-12">
    <div class="max-w-md w-full space-y-8">
      <!-- Header with logo -->
      <div class="text-center">
        <div class="flex justify-center mb-6">
          <AppLogo size="lg" :show-version="false" />
        </div>
        <h1 class="text-2xl sm:text-3xl font-bold text-slate-900 dark:text-slate-100">
          {{ t('auth.reminder.title') }}
        </h1>
        <p class="mt-2 text-sm text-slate-500 dark:text-slate-400">
          {{ t('auth.reminder.subtitle') }}
        </p>
      </div>

      <!-- Error Alert -->
      <div v-if="errorMessage" class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
        <div class="flex items-start">
          <AlertCircle :size="20" class="mr-3 mt-0.5 text-red-600 dark:text-red-400 flex-shrink-0" />
          <div class="flex-1">
            <h3 class="font-medium text-red-900 dark:text-red-200">{{ t('auth.reminder.error.title') }}</h3>
            <p class="mt-1 text-sm text-red-700 dark:text-red-300">{{ errorMessage }}</p>
            <a :href="docFallback" class="mt-3 inline-block text-sm font-medium text-blue-600 dark:text-blue-400 hover:underline">
              {{ t('auth.reminder.error.continue') }}
            </a>
          </div>
        </div>
      </div>

      <!-- Confirmation Card -->
      <div v-if="!errorMessage" class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
        <div class="flex items-center gap-3 mb-4">
          <div class="w-10 h-10 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center">
            <ShieldCheck :size="20" class="text-blue-600 dark:text-blue-400" />
          </div>
          <div>
            <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('auth.reminder.card.title') }}</h2>
            <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('auth.reminder.card.description') }}</p>
          </div>
        </div>
        <button
          @click="confirm"
          :disabled="loading"
          class="w-full trust-gradient text-white font-medium rounded-lg px-4 py-3 text-sm hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center"
        >
          <Loader2 v-if="loading" class="w-4 h-4 animate-spin mr-2" />
          {{ loading ? t('auth.reminder.loading') : t('auth.reminder.button') }}
        </button>
      </div>

      <!-- Privacy note -->
      <p class="text-center text-xs text-slate-500 dark:text-slate-400">
        {{ t('auth.choice.privacy') }}
      </p>
    </div>
  </div>
</template>
