import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { nextTick, ref, watch } from 'vue'

function load() {
  const { descriptor } = parse(readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('App.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const keep = node => ts.isFunctionDeclaration(node) && ['run', 'armFeedbackTimer', 'pauseFeedbackTimer'].includes(node.name?.text)
    || ts.isVariableStatement(node) && node.declarationList.declarations.some(item => ['pendingAction', 'feedback', 'actionError', 'busy', 'feedbackTimer'].includes(item.name.getText(ast)))
    || ts.isExpressionStatement(node) && node.getText(ast).startsWith('watch(feedback,')
  const source = ast.statements.filter(keep).map(node => node.getText(ast)).join('\n')
  return { source, template: descriptor.template.content }
}

function context(source, timers = []) {
  const sandbox = vm.createContext({
    ref, watch, errorMessage: String,
    setTimeout: (callback, delay) => { timers.push({ callback, delay }); return timers.length },
    clearTimeout: id => { const timer = timers[id - 1]; if (timer) timer.cleared = true },
  })
  vm.runInContext(ts.transpile(source + '\nglobalThis.state = { run, armFeedbackTimer, pauseFeedbackTimer, pendingAction, feedback, actionError, busy };'), sandbox)
  return sandbox.state
}

test('keyed actions expose a per-button pending state and keep the global lock', async () => {
  const { source } = load()
  const state = context(source)
  let release
  const action = () => new Promise(resolve => { release = resolve })
  const pending = state.run(action, '', 'test:alpha')
  assert.equal(state.pendingAction.value, 'test:alpha')
  assert.equal(state.busy.value, true)
  release()
  await pending
  assert.equal(state.pendingAction.value, '')
  assert.equal(state.busy.value, false)

  let ran = 0
  state.busy.value = true
  await state.run(async () => { ran++ }, '', 'test:beta')
  assert.equal(ran, 0)
  assert.equal(state.pendingAction.value, '')
})

test('transient feedback auto-dismisses after six seconds and pauses on demand', async () => {
  const { source } = load()
  const timers = []
  const state = context(source, timers)
  state.feedback.value = '服务已删除。'
  await nextTick()
  assert.equal(timers.length, 1)
  assert.equal(timers[0].delay, 6000)
  timers[0].callback()
  assert.equal(state.feedback.value, '')

  state.feedback.value = '已请求重新连接。'
  await nextTick()
  assert.equal(timers.length, 2)
  state.pauseFeedbackTimer()
  assert.equal(timers[1].cleared, true)
  state.armFeedbackTimer()
  assert.equal(timers.length, 3)
  assert.equal(timers[2].cleared, undefined)

  state.feedback.value = ''
  await nextTick()
  assert.equal(timers.length, 3)
  assert.equal(timers[2].cleared, true)
})

test('the toast region keeps the message text in the first span', () => {
  const { template } = load()
  assert.match(template, /class="toast-region"/)
  assert.match(template, /class="toast feedback error-feedback" role="alert"><Icon name="alert" \/><span>\{\{ actionError \}\}<\/span>/)
  assert.match(template, /class="toast feedback" role="status"[^>]+@mouseenter="pauseFeedbackTimer"[^>]*><Icon name="check" \/><span>\{\{ feedback \}\}<\/span>/)
  assert.doesNotMatch(template, /class="feedback error-feedback" role="alert"><span>/)
})
