import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import test from 'node:test'

// Run each parser regression in an isolated process: a future downgrade must
// fail within the timeout instead of hanging the runner or polluting its realm.
function parseDiagram(source: string, inspect: string): any {
  const script = `
    import mermaid from 'mermaid';
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' });
    let diagram, error;
    try {
      diagram = await mermaid.mermaidAPI.getDiagramFromText(${JSON.stringify(source)});
    } catch (failure) {
      error = failure.message;
    }
    console.log(JSON.stringify(${inspect}));
  `
  return JSON.parse(execFileSync(process.execPath, ['--input-type=module', '-e', script], {
    cwd: process.cwd(),
    encoding: 'utf8',
    timeout: 5_000,
  }).trim())
}

test('Mermaid rejects a prototype parent before mutating shared defaults', () => {
  // https://github.com/mermaid-js/mermaid/security/advisories/GHSA-3rrr-jr9j-h3q3
  const result = parseDiagram(`architecture-beta
    group mermaidPrototypePollutionMarker(cloud)[Marker]
    service a(server)[A] in __proto__
    service b(server)[B] in mermaidPrototypePollutionMarker
    a:R -- L:b`, `({
      rejected: Boolean(error),
      polluted: Object.hasOwn(Object.prototype, 'mermaidPrototypePollutionMarker')
    })`)
  assert.equal(result.rejected, true)
  assert.equal(result.polluted, false)
})

test('Mermaid finishes an XY diagram with equal X-axis bounds', () => {
  // https://github.com/mermaid-js/mermaid/security/advisories/GHSA-2v8p-3f2j-5mp7
  const result = parseDiagram('xychart-beta\n x-axis 1 --> 1\n line [1, 2]',
    '({ error, plots: diagram?.db.getXYChartData().plots })')
  assert.equal(result.error, undefined)
  assert.deepEqual(result.plots[0].data, [['1', 1], ['1', 2]])
})

test('Mermaid clamps untrusted radar ticks to a bounded count', () => {
  // https://github.com/mermaid-js/mermaid/security/advisories/GHSA-rhh3-jpg6-66xh
  const result = parseDiagram('radar-beta\n axis a,b\n curve c {1,1}\n ticks 1000000000',
    '({ error, ticks: diagram?.db.getOptions().ticks })')
  assert.equal(result.error, undefined)
  assert.ok(result.ticks > 0 && result.ticks <= 32)
})
