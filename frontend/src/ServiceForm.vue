<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { errorMessage, type AuthType, type ServiceConfig } from './api'
import ChoiceGroup from './ChoiceGroup.vue'

const props = defineProps<{ config: ServiceConfig; busy: boolean }>()
const emit = defineEmits<{ save: [config: ServiceConfig]; cancel: [] }>()
const form = reactive<ServiceConfig>(JSON.parse(JSON.stringify(props.config)))
const args = ref(JSON.stringify(form.args, null, 2))
const scopes = ref(form.auth.scopes.join(' '))
const error = ref('')
const transportOptions = [{ value: 'stdio', label: 'stdio · 本地命令' }, { value: 'http', label: 'Streamable HTTP' }, { value: 'sse', label: 'SSE · 旧版兼容' }] as const
const authOptions = computed<{ value: AuthType; label: string }[]>(() => form.transport === 'stdio'
  ? [{ value: 'none', label: '无认证' }, { value: 'env', label: '环境变量' }]
  : [{ value: 'none', label: '无认证' }, { value: 'bearer', label: 'Bearer Token' }, { value: 'api_key', label: 'API Key' }, { value: 'headers', label: '自定义 Header' }, { value: 'oauth', label: 'OAuth' }])
watch(() => form.transport, () => { if (!authOptions.value.some(item => item.value === form.auth.type)) form.auth.type = 'none' })

function submit() {
  error.value = ''
  try {
    const parsed: unknown = JSON.parse(args.value)
    if (!Array.isArray(parsed) || !parsed.every(arg => typeof arg === 'string')) throw new Error('命令参数必须是 JSON 字符串数组，例如 ["-y", "@example/mcp"]。')
    if (form.transport !== 'stdio') {
      const url = new URL(form.url)
      if (!['http:', 'https:'].includes(url.protocol)) throw new Error('服务地址必须使用 http:// 或 https://。')
    }
    for (const [label, pairs] of [['Header', form.auth.headers], ['环境变量', form.auth.env]] as const) {
      const keys = pairs.map(pair => pair.key.trim())
      if (keys.some(key => !key)) throw new Error(`${label}名称不能为空。`)
      if (new Set(keys.map(key => label === 'Header' ? key.toLowerCase() : key)).size !== keys.length) throw new Error(`${label}名称不能重复。`)
    }
    emit('save', JSON.parse(JSON.stringify({ ...form, args: parsed, auth: { ...form.auth, scopes: scopes.value.split(/\s+/).filter(Boolean) } })))
  } catch (cause) { error.value = errorMessage(cause) }
}
</script>

<template>
  <form class="config-form" @submit.prevent="submit">
    <div class="split-fields">
      <label class="field">服务名称<input v-model="form.name" required maxlength="64" pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" autocomplete="off" placeholder="例如：work-docs"><small>1–64 个字母、数字、点、下划线或连字符</small></label>
      <ChoiceGroup v-model="form.transport" class="field" label="传输方式" :options="transportOptions" />
    </div>
    <template v-if="form.transport === 'stdio'">
      <label class="field">启动命令<input v-model="form.command" required spellcheck="false" autocomplete="off" placeholder="例如：npx 或可执行文件的完整路径"></label>
      <label class="field">命令参数 <small>JSON 字符串数组，不经 shell 解析</small><textarea v-model="args" class="code-input" rows="3" spellcheck="false" required /></label>
      <label class="field">工作目录 <small>可选</small><input v-model="form.cwd" spellcheck="false" placeholder="/Users/…/project"></label>
    </template>
    <label v-else class="field">服务地址<input v-model="form.url" type="url" required spellcheck="false" autocomplete="url" placeholder="https://example.com/mcp"></label>

    <section class="form-section">
      <h3>连接与认证</h3>
      <ChoiceGroup v-model="form.auth.type" class="field" label="认证方式" :options="authOptions" />
      <p v-if="form.auth.type === 'none'" class="muted">此服务不附加认证凭证。</p>
      <template v-if="form.auth.type === 'bearer' || form.auth.type === 'api_key'">
        <label v-if="form.auth.type === 'api_key'" class="field">Header 名称<input v-model="form.auth.headerName" required placeholder="X-API-Key" autocomplete="off"></label>
        <label class="field">{{ form.auth.type === 'bearer' ? 'Bearer Token' : 'API Key' }}<input v-model="form.auth.token" type="password" :required="!form.auth.tokenStored" autocomplete="new-password" :placeholder="form.auth.tokenStored ? '已保存凭证；留空保持原值' : '输入凭证'"></label>
      </template>
      <template v-if="form.auth.type === 'headers'">
        <div v-for="(pair, index) in form.auth.headers" :key="index" class="pair-row">
          <label class="field">Header 名称<input v-model="pair.key" required autocomplete="off"></label>
          <label class="field">值<input v-model="pair.value" type="password" :required="!pair.stored" autocomplete="new-password" :placeholder="pair.stored ? '已保存；留空保持' : 'Header 值'"></label>
          <button type="button" class="icon-button" :aria-label="`移除第 ${index + 1} 个 Header`" @click="form.auth.headers.splice(index, 1)">−</button>
        </div>
        <button type="button" class="control" @click="form.auth.headers.push({ key: '', value: '' })">＋ 添加 Header</button>
      </template>
      <template v-if="form.transport === 'stdio' && form.auth.type === 'env'">
        <div v-for="(pair, index) in form.auth.env" :key="index" class="pair-row">
          <label class="field">变量名称<input v-model="pair.key" required pattern="[A-Za-z_][A-Za-z0-9_]*" autocomplete="off" placeholder="API_TOKEN"></label>
          <label class="field">值<input v-model="pair.value" type="password" :required="!pair.stored" autocomplete="new-password" :placeholder="pair.stored ? '已保存；留空保持' : '变量值'"></label>
          <button type="button" class="icon-button" :aria-label="`移除第 ${index + 1} 个环境变量`" @click="form.auth.env.splice(index, 1)">−</button>
        </div>
        <button type="button" class="control" @click="form.auth.env.push({ key: '', value: '' })">＋ 添加环境变量</button>
      </template>
      <template v-if="form.auth.type === 'oauth'">
        <p class="callout">保存后，在此服务的“连接与认证”中打开浏览器登录。授权能力由提供方决定。</p>
        <label class="field">Client ID <small>提供方要求时填写</small><input v-model="form.auth.clientId" autocomplete="off"></label>
        <label class="field">Scopes <small>用空格分隔，可选</small><input v-model="scopes" autocomplete="off" spellcheck="false" placeholder="read write"></label>
      </template>
      <p v-if="form.auth.type !== 'none'" class="note">凭证仅用于当前服务。已保存的敏感值不会在表单中回显。</p>
    </section>
    <label class="check-row"><input v-model="form.enabled" type="checkbox">启用此服务</label>
    <p v-if="error" role="alert" class="error-text">{{ error }}</p>
    <div class="actions form-actions"><button type="button" class="control" :disabled="busy" @click="emit('cancel')">取消</button><button class="control primary" :disabled="busy">{{ busy ? '正在保存…' : '保存服务' }}</button></div>
  </form>
</template>
