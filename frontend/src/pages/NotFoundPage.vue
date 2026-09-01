<script setup lang="ts">
import { useRouter } from 'vue-router'
import AuthCard from '@/components/AuthCard.vue'
import AppButton from '@/components/AppButton.vue'
import PolicyFooter from '@/components/PolicyFooter.vue'

const router = useRouter()
</script>

<template>
  <AuthCard num="404" :title="$t('notFound.title')" :subtitle="$t('notFound.message')">
    <AppButton @click="router.push('/account/login')">
      {{ $t('notFound.backToLogin') }}
    </AppButton>
  </AuthCard>

  <PolicyFooter />
</template>
