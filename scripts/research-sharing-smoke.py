#!/usr/bin/env python3
"""Exercise real upload, cross-workspace sharing, replacement and revocation.

Uses existing synthetic QA accounts; creates a fresh, isolated project per run.
Never prints credentials, rewrites existing documents, or changes existing shares.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import hashlib
import importlib.util
import json
from pathlib import Path
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', default='http://localhost:8090')
    parser.add_argument('--owner', choices=('owner', 'ui'), default='ui', help='Synthetic account with a configured local embedding model')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location('capture', ROOT / 'scripts/research-live-acceptance.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    suite = module.Acceptance()
    suite.state['actors']['owner'] = dict(suite.state['actors'][args.owner])
    suite.base = args.base_url.rstrip('/')
    suite.qa = {}
    # Keep this run's IDs separate from earlier acceptance fixtures.
    args.output.mkdir(parents=True, exist_ok=False)
    suite.save_state = lambda: (args.output / 'fixtures.json').write_text(
        json.dumps(suite.qa, ensure_ascii=False, indent=2))
    suite.report['base_url'] = suite.base
    suite.report['scope'] = 'Real TXT/DOCX/PDF uploads and two independent workspaces; synthetic documents'
    run_id = uuid.uuid4().hex[:10]

    def raw(actor, endpoint, allowed=(200,)):
        headers = {}
        if actor:
            headers = {'Authorization': 'Bearer ' + suite.state['actors'][actor]['token'],
                       'X-Tenant-ID': str(suite.tenant(actor))}
        request = urllib.request.Request(suite.base + '/api/v1' + endpoint, headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=45) as response:
                code, body = response.status, response.read()
        except urllib.error.HTTPError as error:
            code, body = error.code, error.read()
        if code not in allowed:
            raise module.APIError('GET', endpoint, code)
        return body

    def login():
        for name in ('owner', 'colleague'):
            actor = suite.state['actors'][name]
            assert actor['email'].endswith('@example.invalid'), 'Only synthetic QA accounts may be used'
            request = urllib.request.Request(suite.base + '/api/v1/auth/login',
                data=json.dumps({key: actor[key] for key in ('email', 'password')}).encode(),
                headers={'Content-Type': 'application/json'}, method='POST')
            with urllib.request.urlopen(request, timeout=30) as response:
                payload = json.load(response)
            actor.update({key: payload[key] for key in ('token', 'active_tenant')})
        assert suite.tenant('owner') != suite.tenant('colleague')
        return {'independent_workspaces': 2}

    def setup():
        _, models = suite.request('owner', 'GET', '/models')
        if isinstance(models, dict):
            models = models.get('models', [])
        model = next((item for item in models if item.get('type') == 'Embedding' and item.get('source') == 'local'), None)
        assert model, 'The selected QA owner needs an existing local embedding model'
        for key, label in (('source', '私有来源'), ('target', '团队共享')):
            _, kb = suite.request('owner', 'POST', '/knowledge-bases', {
                'name': f'上线共享验收 · {label} · {run_id}', 'type': 'document',
                'description': '独立合成验收资料，不包含业务文件',
                'embedding_model_id': model['id'],
                'indexing_strategy': {'vector_enabled': True, 'keyword_enabled': True,
                                      'wiki_enabled': False, 'graph_enabled': False}})
            suite.qa[key] = kb['id']
            suite.save_state()
        _, org = suite.request('owner', 'POST', '/organizations', {
            'name': '上线共享验收 · ' + run_id, 'description': 'Isolated synthetic sharing smoke test'})
        suite.qa['organization'] = org['id']
        suite.save_state()
        suite.request('colleague', 'POST', '/organizations/join', {'invite_code': org['invite_code']})
        return {'organization': org['id'], 'source': suite.qa['source'], 'target': suite.qa['target']}

    contents = {'txt': f'第一版团队共享测试\n编号 {run_id}\n同步内容：SHARE-V1\n'.encode()}
    for extension in ('docx', 'pdf'):
        contents[extension] = (ROOT / f'artifacts/research/research-draft.{extension}').read_bytes()
    fields = {'process_config': json.dumps({'summary_enabled': False, 'graph_enabled': False,
                                           'question_generation_config': {'enabled': False}})}

    def upload_files():
        suite.qa['files'] = {}
        for extension, content in contents.items():
            item = suite.upload('owner', 'POST', f"/knowledge-bases/{suite.qa['source']}/knowledge/file",
                                content, f'共享验收-{run_id}.{extension}', fields)
            suite.qa['files'][extension] = item['id']
            suite.save_state()
            suite.wait_native_document(item['id'])
            _, chunks = suite.request('owner', 'GET', '/chunks/' + item['id'])
            if isinstance(chunks, dict):
                chunks = chunks.get('chunks', [])
            assert chunks, 'Uploaded file produced no native chunks'
            text = '\n'.join(chunk.get('content', '') for chunk in chunks)
            assert ('SHARE-V1' if extension == 'txt' else '人工结论') in text, 'Parsed content missing'
            assert hashlib.sha256(raw('owner', '/knowledge/' + item['id'] + '/preview')).digest() == hashlib.sha256(content).digest()
            suite.request('colleague', 'GET', '/knowledge/' + item['id'], allowed=(403, 404))
            raw(None, '/knowledge/' + item['id'] + '/preview', (401, 403))
        return {'formats': list(contents), 'private_before_share': True, 'unauthenticated_denied': True}

    def share_files():
        source, target, org = (suite.qa[k] for k in ('source', 'target', 'organization'))
        body = {'target_kb_id': target, 'organization_id': org,
                'knowledge_ids': list(suite.qa['files'].values()),
                'selected_tenant_ids': [suite.tenant('owner')], 'idempotency_key': uuid.uuid4().hex}
        suite.request('owner', 'POST', f'/research/capture/{source}/share', body, allowed=(409,))
        body.update(selected_tenant_ids=[suite.tenant('owner'), suite.tenant('colleague')], idempotency_key=uuid.uuid4().hex)
        _, job = suite.request('owner', 'POST', f'/research/capture/{source}/share', body)
        for _ in range(90):
            _, job = suite.request('owner', 'GET', f"/research/capture/{source}/jobs/{job['id']}")
            if job['status'] in ('completed', 'partial', 'failed'):
                break
            time.sleep(2)
        assert job['status'] == 'completed', 'Share/move did not complete'
        for extension, identifier in suite.qa['files'].items():
            _, item = suite.request('colleague', 'GET', '/knowledge/' + identifier)
            assert item['id'] == identifier and item['knowledge_base_id'] == target
            assert raw('colleague', '/knowledge/' + identifier + '/preview') == contents[extension]
            _, chunks = suite.request('colleague', 'GET', '/chunks/' + identifier)
            assert chunks, 'Shared parsed content not readable'
        return {'same_knowledge_ids': True, 'identical_original_previews': 3, 'partial_audience_rejected': True}

    def replace_and_read():
        target, identifier = suite.qa['target'], suite.qa['files']['txt']
        v2 = f'第二版团队共享测试\n编号 {run_id}\n同步内容：SHARE-V2\n'.encode()
        item = suite.upload('owner', 'PUT', f'/research/capture/{target}/sources/{identifier}/file',
                            v2, f'共享验收-{run_id}.txt')
        assert item['id'] == identifier
        suite.wait_native_document(identifier)
        started = time.monotonic()
        def read_current(_):
            _, item = suite.request('colleague', 'GET', '/knowledge/' + identifier)
            _, chunks = suite.request('colleague', 'GET', '/chunks/' + identifier)
            if isinstance(chunks, dict):
                chunks = chunks.get('chunks', [])
            assert 'SHARE-V2' in '\n'.join(chunk.get('content', '') for chunk in chunks)
            assert item['file_hash'] == hashlib.md5(v2).hexdigest()
            assert raw('colleague', '/knowledge/' + identifier + '/preview') == v2
        with ThreadPoolExecutor(max_workers=3) as executor:
            list(executor.map(read_current, range(6)))
        _, versions = suite.request('colleague', 'GET', f'/research/capture/{target}/sources/{identifier}')
        expected = {hashlib.md5(contents['txt']).hexdigest(): contents['txt'], hashlib.md5(v2).hexdigest(): v2}
        assert set(expected).issubset({v['file_hash'] for v in versions}), 'Original history incomplete'
        for version in versions:
            if version['file_hash'] in expected:
                endpoint = f"/knowledgebase/{target}/research/sources/{identifier}/versions/{version['id']}/assets/original"
                assert raw('owner', endpoint) == expected[version['file_hash']], 'Historical original changed'
        suite.qa['versions'] = [v['id'] for v in versions]
        suite.save_state()
        return {'concurrent_reads': 6, 'same_id_after_replacement': True, 'versions_preserved': len(versions),
                'read_batch_seconds': round(time.monotonic() - started, 3)}

    def viewer_permissions():
        target, identifier = suite.qa['target'], suite.qa['files']['txt']
        suite.upload('colleague', 'PUT', f'/research/capture/{target}/sources/{identifier}/file',
                     b'must not overwrite', 'denied.txt', allowed=(403,))
        raw('colleague', f'/knowledge/{identifier}/download', (403,))
        version = suite.qa['versions'][0]
        raw('colleague', f'/knowledgebase/{target}/research/sources/{identifier}/versions/{version}/assets/original', (403,))
        return {'viewer_can_preview': True, 'viewer_cannot_overwrite_or_download_original': True}

    def revoke():
        target, identifier, org = suite.qa['target'], suite.qa['files']['txt'], suite.qa['organization']
        _, shares = suite.request('owner', 'GET', f'/knowledge-bases/{target}/shares')
        if isinstance(shares, dict):
            shares = shares.get('shares', [])
        share = next(item for item in shares if item['organization_id'] == org)
        suite.request('owner', 'DELETE', f"/knowledge-bases/{target}/shares/{share['id']}")
        try:
            for endpoint in (f'/knowledge/{identifier}', f'/chunks/{identifier}',
                             f'/knowledge/{identifier}/preview', f'/research/capture/{target}/sources/{identifier}',
                             f"/knowledgebase/{target}/research/sources/{identifier}/versions/{suite.qa['versions'][0]}/assets/markdown"):
                raw('colleague', endpoint, (403, 404))
        finally:
            suite.request('owner', 'POST', f'/knowledge-bases/{target}/shares', {'organization_id': org, 'permission': 'viewer'})
        suite.request('colleague', 'GET', '/knowledge/' + identifier)
        return {'revocation_denies_document_chunks_preview_history_assets': True, 'restored_test_share': True}

    try:
        for name, fn in [('independent_login', login), ('isolated_project', setup),
                         ('upload_parse_and_private_access', upload_files), ('share_same_files', share_files),
                         ('replacement_and_concurrent_read', replace_and_read),
                         ('viewer_permissions', viewer_permissions), ('revocation', revoke)]:
            if not suite.check(name, fn):
                break
    finally:
        suite.report['finished_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        suite.report['result'] = 'passed' if len(suite.report['checks']) == 7 and all(
            item['status'] == 'passed' for item in suite.report['checks']) else 'failed'
        (args.output / 'report.json').write_text(json.dumps(suite.report, ensure_ascii=False, indent=2))
        print(json.dumps({'result': suite.report['result'], 'report': str(args.output / 'report.json')}))
    if suite.report['result'] != 'passed':
        raise SystemExit(1)


if __name__ == '__main__':
    main()
