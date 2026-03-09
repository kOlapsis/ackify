<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Github } from 'lucide-vue-next'
import AppLogo from '@/components/AppLogo.vue'

const { t } = useI18n()
</script>

<template>
  <footer class="mt-auto border-t border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
    <div class="mx-auto max-w-6xl px-4 py-6 sm:px-6">
      <div class="flex flex-col items-center justify-between gap-4 sm:flex-row sm:gap-6">
        <!-- Brand & Description -->
        <div class="flex items-center gap-3 text-center sm:text-left">
          <AppLogo size="sm" :show-text="false" />
          <span class="text-sm text-slate-500 dark:text-slate-400 leading-tight max-w-md">
            {{ t('footer.description') }}
          </span>
        </div>

        <!-- Links & Copyright -->
        <div class="flex flex-wrap items-center justify-center gap-4 text-sm sm:flex-nowrap">
          <a
            href="https://github.com/kolapsis/ackify"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1.5 text-slate-500 dark:text-slate-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
            :aria-label="t('footer.resources.documentation')"
          >
            <Github :size="16" />
            <span>GitHub</span>
          </a>

          <span class="text-slate-300 dark:text-slate-600">|</span>

          <span class="text-slate-500 dark:text-slate-400 whitespace-nowrap">
            &copy; {{ new Date().getFullYear() }} {{ t('footer.license') }}
          </span>
        </div>
      </div>
    </div>
  </footer>
</template>
