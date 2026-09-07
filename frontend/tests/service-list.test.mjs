import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { nextTick, reactive, ref, watch } from 'vue'

test('service tools start collapsed and expand independently from configuration details', async () => {
  const { descriptor } = parse(readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('App.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const declarations = ['selectedId', 'expandedServices', 'inspector', 'selectedTrigger', 'toolSearch', 'tab']
  const functions = ['toggleServiceTools', 'selectService', 'closeService']
  const source = ast.statements.filter(node =>
    ts.isVariableStatement(node) && node.declarationList.declarations.some(item => declarations.includes(item.name.getText(ast))) ||
    ts.isFunctionDeclaration(node) && functions.includes(node.name?.text) ||
    ts.isExpressionStatement(node) && node.getText(ast).startsWith('watch(selectedId,')
  ).map(node => node.getText(ast)).join('\n')
  const context = vm.createContext({ nextTick, reactive, ref, watch })
  vm.runInContext(ts.transpile(source + '\nglobalThis.state = { selectedId, expandedServices, inspector, tab };'), context)
  const { state } = context
  assert.equal(state.expandedServices.size, 0)
  assert.equal(state.selectedId.value, '')
  context.toggleServiceTools('alpha')
  context.toggleServiceTools('beta')
  assert.equal(state.expandedServices.size, 2)
  assert.equal(state.selectedId.value, '')
  let focused = 0
  state.inspector.value = { focus() { focused++ }, scrollIntoView() {} }
  let returnedFocus = 0
  await context.selectService('alpha', { currentTarget: { focus() { returnedFocus++ } } })
  assert.equal(state.tab.value, 'connection')
  assert.equal(focused, 1)
  context.toggleServiceTools('alpha')
  assert.equal(state.expandedServices.has('alpha'), false)
  assert.equal(state.expandedServices.has('beta'), true)
  assert.equal(state.selectedId.value, 'alpha')
  context.closeService()
  assert.equal(returnedFocus, 1)
  assert.equal(state.selectedId.value, '')
  assert.equal(state.expandedServices.has('beta'), true)
  // Hiding, rather than unmounting, preserves entered arguments and call results.
  assert.match(descriptor.template.content, /v-show="expandedServices.has\(server.id\)"/)
  assert.match(descriptor.template.content, /@click="toggleServiceTools\(server.id\)"/)
  assert.match(descriptor.template.content, /class="control service-config-button"[^>]+@click="selectService\(server.id, \$event\)"/)
})
