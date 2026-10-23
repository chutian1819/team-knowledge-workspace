#!/usr/bin/env node
// Static inventory only: a wired event is not evidence that its backend action works.
import { createRequire } from 'node:module'
import { readdirSync, readFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(join(root, 'frontend/package.json'))
const { parse } = require('@vue/compiler-sfc')
const { parse: parseTemplate } = require('@vue/compiler-dom')
const sourceRoot = join(root, 'frontend/src')
const controls = []
const files = []
function walk(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) walk(path)
    else if (entry.name.endsWith('.vue')) files.push(path)
  }
}
function textContent(node) {
  if (node.type === 2) return node.content
  if (node.type === 5) return `{{ ${node.content.content} }}`
  return (node.children || []).map(textContent).join(' ').replace(/\s+/g, ' ').trim()
}
walk(sourceRoot)
for (const path of files.sort()) {
  const { descriptor, errors } = parse(readFileSync(path, 'utf8'), { filename: path })
  if (errors.length) throw new Error(`Could not parse ${relative(root, path)}: ${errors[0]}`)
  if (!descriptor.template) continue
  const ast = parseTemplate(descriptor.template.content)
  const visit = node => {
    if (node.type === 1) {
      const events = node.props.filter(prop => prop.type === 7 && ['on', 'model'].includes(prop.name))
      const interactive = /^(button|a|input|select|textarea|t-(button|link|input|select|checkbox|radio|switch|dropdown|upload|pagination))$/.test(node.tag)
      if (events.length || interactive) {
        const bindings = Object.fromEntries(node.props.filter(prop => prop.type === 7)
          .map(prop => [`${prop.name}:${prop.arg?.content || ''}${prop.modifiers?.length ? `.${prop.modifiers.map(item => typeof item === 'string' ? item : item.content).join('.')}` : ''}`, prop.exp?.content || '']))
        const attributes = Object.fromEntries(node.props.filter(prop => prop.type === 6)
          .map(prop => [prop.name, prop.value?.content ?? true]))
        controls.push({
          file: relative(root, path), line: descriptor.template.loc.start.line + node.loc.start.line - 1,
          tag: node.tag, label: textContent(node).slice(0, 160), attributes, bindings,
        })
      }
    }
    for (const child of node.children || []) visit(child)
  }
  visit(ast)
}
const byArea = {}
for (const control of controls) {
  const match = /frontend\/src\/(views\/[^/]+|components|stores)/.exec(control.file)
  const area = match?.[1] || 'other'
  byArea[area] = (byArea[area] || 0) + 1
}
const output = resolve(root, process.argv[2] || 'artifacts/research/ui-action-inventory.json')
mkdirSync(dirname(output), { recursive: true })
writeFileSync(output, JSON.stringify({
  generated_at: new Date().toISOString(), method: 'Vue template AST: event/model bindings and native/TDesign interactive controls; dynamic repeated items counted once per template. This is an inventory, not a successful browser test.',
  vue_files: files.length, controls: controls.length, by_area: byArea, items: controls,
}, null, 2) + '\n')
console.log(JSON.stringify({ output: relative(root, output), vue_files: files.length, controls: controls.length, by_area: byArea }, null, 2))
