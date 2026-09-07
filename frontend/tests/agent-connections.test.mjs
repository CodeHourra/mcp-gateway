import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { computed, reactive } from 'vue'

test('agent sections distinguish configured, disabled, stale and unrecognized configurations', () => {
  const { descriptor } = parse(readFileSync(new URL('../src/AgentConnections.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('Agents.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const source = ast.statements.filter(node => !ts.isImportDeclaration(node)).map(node => node.getText(ast)).join('\n')
  const props = reactive({ agents: [
    { id: 'ready', status: 'configured' }, { id: 'disabled', status: 'configured', disabled: true },
    { id: 'stale', status: 'needs_update' }, { id: 'conflict', status: 'conflict' },
    { id: 'unknown', status: 'unavailable' }, { id: 'new', status: 'not_configured' },
  ] })
  const context = vm.createContext({ computed, defineProps: () => props, defineEmits: () => () => {} })
  vm.runInContext(ts.transpile(source + '\nglobalThis.state = { groups, label };'), context)
  const { groups, label } = context.state
  assert.deepEqual(Array.from(groups.value, group => Array.from(group.agents, agent => agent.id)), [['ready'], ['disabled', 'stale', 'conflict', 'unknown'], ['new']])
  assert.match(label(props.agents[1]), /已禁用/)
  assert.match(label(props.agents[2]), /已接入.*需更新/)
  assert.match(label(props.agents[4]), /无法判断/)
  props.agents[5].status = 'configured'
  assert.equal(groups.value[0].agents.length, 2)
  assert.equal(groups.value[2].agents.length, 0)
})
