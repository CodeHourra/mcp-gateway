import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'

test('startup polling waits for the current request and stops after unmount', async () => {
  const { descriptor } = parse(readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('App.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const fn = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === 'pollSnapshot')
  let finish
  const scheduled = []
  const context = vm.createContext({ unmounted: false, busy: { value: false }, window: { gateway: {} },
    snapshot: { value: { gateway: { status: 'starting' } } }, poll: undefined,
    refresh: () => new Promise(resolve => { finish = resolve }),
    setTimeout: (callback, delay) => { scheduled.push(delay); return 1 },
  })
  vm.runInContext(ts.transpile(fn.getText(ast)), context)
  const pending = context.pollSnapshot()
  assert.equal(scheduled.length, 0)
  finish(); await pending
  assert.deepEqual(scheduled, [500])
  context.snapshot.value.gateway.status = 'running'
  const next = context.pollSnapshot(); finish(); await next
  assert.deepEqual(scheduled, [500, 5000])
  const last = context.pollSnapshot(); context.unmounted = true; finish(); await last
  assert.deepEqual(scheduled, [500, 5000])
})
