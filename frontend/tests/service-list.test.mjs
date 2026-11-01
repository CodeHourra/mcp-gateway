import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { computed, nextTick, reactive, ref, watch } from 'vue'

function load() {
  const { descriptor } = parse(readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('App.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const declarations = ['expandedServices', 'segmentByService']
  const functions = ['needsAttention', 'defaultSegment', 'segmentOf', 'setSegment', 'toggleService', 'collapseAll', 'headerAction', 'moveServiceFocus', 'saveService']
  const source = ast.statements.filter(node =>
    ts.isVariableStatement(node) && node.declarationList.declarations.some(item => declarations.includes(item.name.getText(ast))) ||
    ts.isFunctionDeclaration(node) && functions.includes(node.name?.text)
  ).map(node => node.getText(ast)).join('\n')
  const context = vm.createContext({ computed, nextTick, reactive, ref, watch })
  vm.runInContext(ts.transpile(source + '\nglobalThis.state = { expandedServices, segmentByService, needsAttention, defaultSegment, segmentOf, setSegment, toggleService, collapseAll, headerAction, moveServiceFocus, saveService };'), context)
  return { state: context.state, context, template: descriptor.template.content }
}

const service = (id, status = 'ready', auth = { type: 'none' }) => ({ id, name: id, status, auth })

test('each service card keeps its own disclosure and all cards can collapse together', () => {
  const { state } = load()
  assert.equal(state.expandedServices.size, 0)
  state.toggleService(service('alpha'))
  state.toggleService(service('beta'))
  assert.equal(state.expandedServices.size, 2)
  assert.equal(state.expandedServices.has('alpha'), true)
  assert.equal(state.expandedServices.has('beta'), true)
  state.toggleService(service('alpha'))
  assert.equal(state.expandedServices.has('alpha'), false)
  assert.equal(state.expandedServices.has('beta'), true)
  state.collapseAll()
  assert.equal(state.expandedServices.size, 0)
})

test('the first expand seeds the segment from service status and remembers user choices', () => {
  const { state } = load()
  assert.equal(state.defaultSegment(service('a', 'error')), 'connection')
  assert.equal(state.defaultSegment(service('b', 'auth_required')), 'connection')
  assert.equal(state.defaultSegment(service('c', 'ready')), 'tools')
  assert.equal(state.defaultSegment(service('d', 'disabled')), 'tools')
  assert.equal(state.needsAttention(service('a', 'error')), true)
  assert.equal(state.needsAttention(service('b', 'auth_required')), true)
  assert.equal(state.needsAttention(service('a', 'ready')), false)

  const alpha = service('alpha', 'error')
  state.toggleService(alpha)
  assert.equal(state.segmentOf('alpha'), 'connection')
  state.setSegment('alpha', 'activity')
  state.toggleService(service('beta', 'ready'))
  assert.equal(state.segmentOf('alpha'), 'activity')
  assert.equal(state.segmentOf('beta'), 'tools')
  // Collapsing and re-expanding keeps the explicit choice, even after the status changed.
  state.collapseAll()
  alpha.status = 'ready'
  state.toggleService(alpha)
  assert.equal(state.segmentOf('alpha'), 'activity')
})

test('header actions match the status and never call undefined work', () => {
  const { state } = load()
  assert.deepEqual({ ...state.headerAction(service('a', 'auth_required', { type: 'oauth' })) }, { kind: 'login', label: '登录' })
  assert.deepEqual({ ...state.headerAction(service('b', 'auth_required', { type: 'bearer' })) }, { kind: 'edit', label: '编辑配置' })
  assert.deepEqual({ ...state.headerAction(service('c', 'error')) }, { kind: 'reconnect', label: '重新连接' })
  for (const status of ['ready', 'connecting', 'disabled', 'starting']) assert.equal(state.headerAction(service('d', status)), undefined)
})

test('saving a service expands its card on the connection segment', async () => {
  const { state, context } = load()
  const saved = { id: 'gamma', name: 'gamma' }
  Object.assign(context, { run: action => action(), request: async () => saved, refresh: async () => {}, closeEditor() {}, document: { getElementById: () => null }, nextTick })
  context.saveService({ name: 'gamma' })
  await nextTick(); await new Promise(resolve => setTimeout(resolve, 0))
  assert.equal(state.expandedServices.has('gamma'), true)
  assert.equal(state.segmentOf('gamma'), 'connection')
})

test('keyboard navigation moves between service row headers', () => {
  const { state } = load()
  let focused = -1
  const rows = [0, 1, 2].map(index => ({ index, focus() { focused = index } }))
  const container = { querySelectorAll: () => rows }
  rows.forEach(row => { row.closest = () => container })
  const press = (key, index) => state.moveServiceFocus({ key, currentTarget: rows[index], preventDefault() {} })
  press('ArrowDown', 0)
  assert.equal(focused, 1)
  press('ArrowUp', 1)
  assert.equal(focused, 0)
  press('End', 0)
  assert.equal(focused, 2)
  press('Home', 2)
  assert.equal(focused, 0)
  press('Tab', 0)
  assert.equal(focused, 0)
})

test('the template has one disclosure per card and no leftover panels', () => {
  const { template } = load()
  for (const marker of [
    /class="service-row"[^>]+@click="toggleService\(server\)"/,
    /role="switch"/,
    /v-show="expandedServices\.has\(server\.id\)"/,
    /class="service-body"/,
    /class="service-segments"/,
    /全部收起/,
  ]) assert.match(template, marker)
  for (const marker of [
    /service-config-panel/, /service-config-button/, /class="tab-bar"/, /class="inspect-/, /service-tools-panel/,
    /service-expand-label/, /<aside/, /class="inspector"/, /master-detail/, /class="theme-picker"/,
  ]) assert.doesNotMatch(template, marker)
})
