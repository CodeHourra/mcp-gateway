import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { computed, ref, watch, nextTick } from 'vue'

function editor(request) {
  const { descriptor } = parse(readFileSync(new URL('../src/FullConfigEditor.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('Editor.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const source = ast.statements.filter(node => !ts.isImportDeclaration(node)).map(node => node.getText(ast)).join('\n')
  const context = vm.createContext({ computed, ref, watch, nextTick, request,
    defineProps: () => ({ active: false, busy: false }), defineEmits: () => () => {}, errorMessage: String,
  })
  vm.runInContext(ts.transpile(source + '\nglobalThis.editor = { load, check, save, content, original, preview, confirmRemoved, error, message, loading };'), context)
  return context.editor
}

test('full JSON editor invalidates changed previews and requires removal confirmation', async () => {
  const calls = []
  const state = editor(async (method, params) => {
    calls.push({ method, params })
    if (method === 'getFullMCPConfig') return { editorId: 'revision', content: '{"mcpServers":[{"name":"a"}]}' }
    if (method === 'previewFullMCPConfig') return { id: 'plan', removed: 1 }
    return { backupId: 'backup' }
  })
  await state.load()
  state.content.value = '{"mcpServers":[]}'
  await nextTick(); await state.check()
  await state.save()
  assert.equal(calls.filter(call => call.method === 'saveFullMCPConfig').length, 0)
  state.confirmRemoved.value = true
  state.content.value = '{"mcpServers":[{"name":"b"}]}'
  await nextTick()
  assert.equal(state.preview.value, undefined)
  assert.equal(state.confirmRemoved.value, false)
  await state.check(); state.confirmRemoved.value = true
  await state.save(); await nextTick()
  assert.equal(calls.filter(call => call.method === 'saveFullMCPConfig').length, 1)
  assert.equal(state.content.value, state.original.value)
  assert.match(state.message.value, /backup/)
})

test('failed JSON validation keeps the draft and releases the loading state', async () => {
  const state = editor(async method => {
    if (method === 'getFullMCPConfig') return { editorId: 'revision', content: '{}' }
    throw new Error('JSON 格式无效')
  })
  await state.load()
  state.content.value = '{bad json'
  await nextTick(); await state.check()
  assert.equal(state.content.value, '{bad json')
  assert.match(state.error.value, /JSON 格式无效/)
  assert.equal(state.preview.value, undefined)
  assert.equal(state.loading.value, false)
})
