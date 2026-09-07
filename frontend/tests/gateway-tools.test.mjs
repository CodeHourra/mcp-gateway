import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { reactive, ref, watch, nextTick } from 'vue'

function component(file, scope, exposed) {
  const { descriptor } = parse(readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8'))
  const ast = ts.createSourceFile(file, descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const source = ast.statements.filter(node => !ts.isImportDeclaration(node)).map(node => node.getText(ast)).join('\n')
  const context = vm.createContext({ reactive, ref, watch, onUnmounted() {}, errorMessage: String, ...scope })
  vm.runInContext(ts.transpile(source + `\nglobalThis.state = { ${exposed} };`), context)
  return context.state
}

test('gateway catalog ignores stale responses and waits for explicit expansion', async () => {
  const props = reactive({ address: 'http://127.0.0.1:17840/mcp/call', connected: true, busy: false })
  const calls = []
  const state = component('GatewayTools.vue', { defineProps: () => props, request: (method, args) => new Promise(resolve => calls.push({ method, args, resolve })) }, 'load, toggle, tools, expanded, loading, error')
  assert.equal(calls.length, 0)
  state.toggle({ target: { open: true } })
  assert.equal(calls.length, 1)
  props.address = 'http://127.0.0.1:17840/mcp/all'
  await nextTick()
  assert.equal(calls.length, 2)
  calls[1].resolve({ address: props.address, tools: [{ id: 'new' }] })
  await nextTick(); await nextTick()
  calls[0].resolve({ address: 'http://127.0.0.1:17840/mcp/call', tools: [{ id: 'stale' }] })
  await nextTick(); await nextTick()
  assert.equal(state.tools.value[0].id, 'new')
  state.toggle({ target: { open: false } })
  assert.equal(state.tools.value[0].id, 'new')
})

test('shared tool UI routes gateway calls correctly and validates object arguments', async () => {
  const calls = [], events = []
  const props = { tools: [], gatewayAddress: 'http://127.0.0.1:17840/mcp/call', busy: false }
  const state = component('ToolsList.vue', {
    defineProps: () => props, defineEmits: () => (...event) => events.push(event),
    parseArguments: text => { const args = JSON.parse(text); if (!args || Array.isArray(args) || typeof args !== 'object') throw Error('工具参数必须是 JSON 对象'); return args },
    request: async (method, args) => { calls.push({ method, args }); return { content: [{ type: 'text', text: 'result' }], isError: true } },
  }, 'call, argumentsByTool, results')
  const tool = { id: 'retrieve_tools', name: 'retrieve_tools', enabled: true }
  state.argumentsByTool[tool.id] = '[]'
  await state.call(tool)
  assert.equal(calls.length, 0)
  assert.match(state.results[tool.id].error, /JSON 对象/)
  state.argumentsByTool[tool.id] = '{"query":"test"}'
  await state.call(tool)
  assert.equal(calls[0].method, 'callGatewayTool')
  assert.equal(calls[0].args.address, props.gatewayAddress)
  assert.equal(calls[0].args.name, tool.name)
  assert.equal(state.results[tool.id].value.isError, true)
  assert.deepEqual(events.at(-1), ['calling', false])
  props.gatewayAddress = undefined
  await state.call(tool)
  assert.equal(calls[1].method, 'callTool')
  props.busy = true
  await state.call(tool)
  assert.equal(calls.length, 2)
})
