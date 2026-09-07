<script setup lang="ts" generic="T extends string | number">
import { useId } from 'vue'

defineProps<{ label: string; options: readonly { value: T; label: string }[]; disabled?: boolean; hideLabel?: boolean }>()
const model = defineModel<T>({ required: true })
const name = useId()
</script>

<template>
  <fieldset class="choice-group" :disabled="disabled">
    <legend :class="{ 'sr-only': hideLabel }">{{ label }}</legend>
    <div class="choice-options">
      <label v-for="option in options" :key="option.value" class="choice-option">
        <input v-model="model" class="sr-only" type="radio" :name="name" :value="option.value">
        <span>{{ option.label }}</span>
      </label>
    </div>
  </fieldset>
</template>
