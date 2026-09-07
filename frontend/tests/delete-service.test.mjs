import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'

test('delete waits for confirmation and keeps the original service ID', async () => {
  const { descriptor } = parse(readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8'))
  const ast = ts.createSourceFile('App.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const fn = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === 'deleteService')
  assert.ok(fn)
  let confirmation
  const calls = []
  const context = vm.createContext({
    askConfirmation: (...args) => { confirmation = args },
    mutate: (...args) => { calls.push(args); return Promise.resolve() },
  })
  vm.runInContext(ts.transpile(fn.getText(ast)), context)
  const service = { id: 'first', name: 'First service' }
  context.deleteService(service)
  assert.equal(calls.length, 0)
  assert.match(confirmation[1], /First service/)
  service.id = 'second'
  await confirmation[3]()
  assert.equal(calls.length, 1)
  assert.equal(calls[0][0], 'deleteService')
  assert.equal(calls[0][1].serviceId, 'first')
  const header = descriptor.template.content.split('<div class="inspect-header">')[1].split('<div class="tab-bar"')[0]
  assert.match(header, /:disabled="busy \|\| !connected" @click="deleteService\(selected\)">删除服务/)
})
