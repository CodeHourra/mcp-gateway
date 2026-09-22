import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { nextTick, reactive, ref, watch } from 'vue'

function load() {
  const { descriptor } = parse(readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('App.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const declarations = ['selectedId', 'expandedServices', 'tab', 'toolSearch']
  const functions = ['toggleTools', 'toggleConfig', 'closeConfig']
  const source = ast.statements.filter(node =>
    ts.isVariableStatement(node) && node.declarationList.declarations.some(item => declarations.includes(item.name.getText(ast))) ||
    ts.isFunctionDeclaration(node) && functions.includes(node.name?.text) ||
    ts.isExpressionStatement(node) && node.getText(ast).startsWith('watch(selectedId,')
  ).map(node => node.getText(ast)).join('\n')
  const context = vm.createContext({ nextTick, reactive, ref, watch })
  vm.runInContext(ts.transpile(source + '\nglobalThis.state = { selectedId, expandedServices, tab, toolSearch };'), context)
  return { state: context.state, context, template: descriptor.template.content }
}

test('tools and configuration panels expand in place and stay mutually exclusive per service', async () => {
  const { state, context } = load()
  assert.equal(state.expandedServices.size, 0)
  assert.equal(state.selectedId.value, '')
  context.toggleTools('alpha')
  context.toggleTools('beta')
  assert.equal(state.expandedServices.size, 2)
  // The group the trigger belongs to owns the panel that opens and receives focus.
  let focused = 0
  const panel = { focus() { focused++ }, scrollIntoView() {} }
  const trigger = { focus() { focused++ }, closest: () => ({ querySelector: () => panel }) }
  await context.toggleConfig('alpha', { currentTarget: trigger })
  // Opening configuration closes the tools panel of the same service only.
  assert.equal(state.expandedServices.has('alpha'), false)
  assert.equal(state.expandedServices.has('beta'), true)
  assert.equal(state.selectedId.value, 'alpha')
  assert.equal(state.tab.value, 'connection')
  assert.equal(focused, 1)
  // Reopening tools for that service closes its configuration instead of showing both.
  context.toggleTools('alpha')
  assert.equal(state.expandedServices.has('alpha'), true)
  assert.equal(state.expandedServices.has('beta'), true)
  assert.equal(state.selectedId.value, '')
  await context.toggleConfig('alpha', { currentTarget: trigger })
  assert.equal(state.expandedServices.has('alpha'), false)
  // Clicking the active trigger again collapses and returns focus to it.
  context.toggleConfig('alpha', { currentTarget: trigger })
  assert.equal(state.selectedId.value, '')
  assert.equal(focused, 3)
})

test('template renders both panels inline inside the service group', () => {
  const { template } = load()
  for (const marker of [
    /v-show="expandedServices\.has\(server\.id\)"/,
    /@click="toggleTools\(server\.id\)"/,
    /class="control service-config-button"[^>]+@click="toggleConfig\(server\.id, \$event\)"/,
    /v-if="selectedId === server\.id"[^>]*class="service-config-panel"/
  ]) assert.match(template, marker)
  // The detail panel is no longer a list-wide sibling of the service list.
  assert.doesNotMatch(template, /<aside/)
  assert.doesNotMatch(template, /class="inspector"/)
  assert.doesNotMatch(template, /master-detail/)
})
