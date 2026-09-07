import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parseArguments, request } from './api.ts'

test('tool call accepts only JSON objects and an unavailable bridge never reports success', async () => {
  assert.deepEqual(parseArguments('{"limit": 3}'), { limit: 3 })
  for (const value of ['null', '[]', 'true', '1', '"text"', '{']) assert.throws(() => parseArguments(value))
  await assert.rejects(request('saveService', {}), /尚未连接/)
})
