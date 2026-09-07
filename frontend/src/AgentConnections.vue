<script setup lang="ts">
import { computed } from 'vue'
import type { Agent } from './api'

const props = defineProps<{ agents: Agent[]; busy: boolean; connected: boolean }>()
defineEmits<{ preview: [agent: Agent, disconnect?: boolean] }>()
const groups = computed(() => [
  { id: 'configured', title: '已配置接入', description: '配置与当前网关一致，可在客户端重新加载后使用。', agents: props.agents.filter(agent => agent.status === 'configured' && !agent.disabled) },
  { id: 'attention', title: '需处理', description: '检查下方原因，更新配置或处理客户端设置。', agents: props.agents.filter(agent => agent.disabled || !['configured', 'not_configured'].includes(agent.status)) },
  { id: 'pending', title: '待接入', description: '尚未在客户端配置中发现当前网关接入。', agents: props.agents.filter(agent => agent.status === 'not_configured' && !agent.disabled) },
])
function label(agent: Agent) {
  if (agent.disabled) return agent.status === 'needs_update' ? '已接入 · 已禁用 · 需更新' : '已接入 · 客户端已禁用'
  return ({ configured: '已配置接入', needs_update: '已接入 · 需更新', not_configured: '未接入', conflict: '配置冲突', unavailable: '无法判断接入状态' } as Record<string, string>)[agent.status] || '待核对配置'
}
</script>

<template>
  <div class="agent-overview" aria-label="Agent 配置状态统计">
    <div v-for="group in groups" :key="group.id" :class="group.id"><strong>{{ group.agents.length }}</strong><span>{{ group.title }}</span></div>
  </div>
  <section v-for="group in groups.filter(item => item.agents.length)" :key="group.id" class="agent-section" :aria-labelledby="`agents-${group.id}`">
    <div class="agent-section-heading"><h2 :id="`agents-${group.id}`">{{ group.title }} <span>{{ group.agents.length }}</span></h2><p>{{ group.description }}</p></div>
    <article v-for="agent in group.agents" :key="agent.id" class="agent-card" :class="group.id" :data-agent-id="agent.id">
      <div class="agent-card-top"><span class="service-symbol" aria-hidden="true">{{ agent.name.slice(0, 1) }}</span><h3>{{ agent.name }}</h3><span class="badge" :class="group.id === 'configured' ? 'good' : group.id === 'attention' ? 'warn' : 'neutral'">{{ label(agent) }}</span></div>
      <p class="agent-message">{{ agent.message || (group.id === 'configured' ? '已检测到当前网关的接入配置。' : '可生成配置预览，确认后写入客户端。') }}</p>
      <p v-if="agent.disabled" class="warning-text">该接入在客户端中被禁用，请在客户端启用；更新配置会保留禁用设置。</p>
      <div class="agent-card-bottom"><div class="agent-config-path"><span>配置文件</span><code>{{ agent.configPath || '未提供配置位置' }}</code></div><div class="actions"><button class="control" :class="group.id === 'pending' || agent.status === 'needs_update' ? 'primary' : ''" :disabled="busy || !connected || !agent.canConfigure" @click="$emit('preview', agent)">{{ agent.status === 'configured' ? '查看 / 更新配置' : agent.status === 'needs_update' ? '更新接入配置' : agent.canConfigure ? '配置接入' : '需手动处理' }}</button><button v-if="agent.canDisconnect" class="control danger" :disabled="busy" @click="$emit('preview', agent, true)">解除接入</button></div></div>
    </article>
  </section>
</template>

<style scoped>
.agent-overview{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin:22px 0 28px}.agent-overview>div{display:flex;align-items:baseline;gap:10px;padding:15px 18px;border:1px solid var(--line);border-radius:9px;background:var(--soft)}.agent-overview strong{font-size:24px;line-height:1.2}.agent-overview span{font-size:14px;color:var(--muted)}.agent-overview .configured strong{color:var(--good)}.agent-overview .attention strong{color:var(--warn)}.agent-section{margin-top:26px}.agent-section-heading{margin-bottom:12px}.agent-section-heading h2{font-size:16px}.agent-section-heading h2 span{color:var(--muted);font-weight:400;margin-left:6px}.agent-section-heading p{color:var(--muted);font-size:13px;margin-top:4px}.agent-card{border:1px solid var(--line);border-left:3px solid var(--line);border-radius:9px;padding:17px 18px;margin-top:10px;background:var(--bg)}.agent-card.configured{border-left-color:var(--good)}.agent-card.attention{border-left-color:var(--warn)}.agent-card-top{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.agent-card-top h3{flex:1;font-size:16px}.agent-card-top .service-symbol{width:28px;height:28px;font-size:14px}.agent-message{font-size:14px;color:var(--muted);overflow-wrap:anywhere;margin-top:12px}.agent-card .warning-text{font-size:13px}.agent-card-bottom{display:flex;gap:16px;align-items:flex-end;justify-content:space-between;margin-top:17px;padding-top:13px;border-top:1px solid var(--line)}.agent-config-path{min-width:0}.agent-config-path span{display:block;font-size:12px;color:var(--muted);margin-bottom:3px}.agent-config-path code{font-size:12px;color:var(--muted);overflow-wrap:anywhere}.agent-card-bottom .actions{flex-shrink:0}
@media(max-width:850px){.agent-card-bottom{align-items:flex-start;flex-direction:column}.agent-overview>div{padding:12px;gap:5px;flex-wrap:wrap}}@media(max-width:600px){.agent-overview{gap:7px}.agent-overview>div{flex-direction:column}.agent-overview strong{font-size:21px}.agent-overview span{font-size:13px}.agent-card{padding:14px}.agent-card-top .badge{margin-left:38px}.agent-card-top h3{flex-basis:calc(100% - 40px)}}
</style>
