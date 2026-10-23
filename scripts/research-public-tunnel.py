#!/usr/bin/env python3
"""Manage a macOS login-session HTTPS trial tunnel without moving platform data."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import signal
import subprocess
import sys
import urllib.request

LABEL = 'local.research.public-trial'
RUNTIME = Path.home() / 'Library/Application Support/ResearchPublicTrial'
PLIST = Path.home() / 'Library/LaunchAgents' / (LABEL + '.plist')
STATUS = RUNTIME / 'status.json'
URL = RUNTIME / 'current-url.txt'


def save_status(state, url=None):
    data = {'state': state, 'updated_at': dt.datetime.now(dt.timezone.utc).isoformat(),
            'provider': 'localhost.run', 'url': url, 'temporary': True}
    temporary = STATUS.with_suffix('.tmp')
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2))
    temporary.replace(STATUS)
    if url:
        URL.write_text(url + '\n')


def run():
    # launchd restarts this process after network loss. Keep one SSH session,
    # verify its host key, and keep the computer awake only while it runs.
    save_status('connecting')
    child = subprocess.Popen([
        '/usr/bin/ssh', '-T', '-F', '/dev/null', '-o', 'IdentityAgent=none',
        '-o', 'IdentitiesOnly=yes', '-o', 'PreferredAuthentications=none',
        '-o', 'StrictHostKeyChecking=yes', '-o', 'UserKnownHostsFile="' + str(RUNTIME / 'known_hosts') + '"',
        '-o', 'ExitOnForwardFailure=yes', '-o', 'ServerAliveInterval=30',
        '-o', 'ServerAliveCountMax=3', '-o', 'ConnectTimeout=15',
        '-R', '80:127.0.0.1:8091', 'nokey@localhost.run', '--',
        '--inject-http-proxy-headers', '--output', 'json'],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    awake = subprocess.Popen(['/usr/bin/caffeinate', '-i', '-w', str(child.pid)])

    def terminate(_signum, _frame):
        child.terminate()

    signal.signal(signal.SIGTERM, terminate)
    signal.signal(signal.SIGINT, terminate)
    current = None
    diagnostic = ''
    try:
        for line in child.stdout:
            try:
                event = json.loads(line)
            except (ValueError, TypeError):
                if line.strip():
                    diagnostic = line.strip()[:300]
                continue
            if event.get('type') != 'v1.tcpip_forward.register.accepted':
                continue
            match = re.search(r'https://[a-z0-9-]+\.(?:lhr\.life|lhr\.rocks|localhost\.run)(?=[\s/]|$)', event.get('data', ''))
            if match:
                current = match.group(0)
                save_status('connected', current)
                print('当前公网试用地址：' + current, flush=True)
        code = child.wait()
        if code:
            print(f'隧道连接退出（{code}）：{diagnostic}', file=sys.stderr, flush=True)
        return code
    finally:
        if child.poll() is None:
            child.terminate()
        awake.terminate()
        save_status('disconnected', current)


def start():
    request = urllib.request.Request('http://127.0.0.1:8091/api/v1/auth/config',
                                     headers={'X-Forwarded-Proto': 'https'})
    with urllib.request.urlopen(request, timeout=10) as response:
        config = json.load(response)
    if config.get('registration_mode') not in ('invite_register', 'invite_only'):
        raise RuntimeError('公网网关未就绪：必须先限制公开注册')
    domain = 'gui/' + str(os.getuid())
    existing = subprocess.run(['launchctl', 'print', domain + '/' + LABEL], capture_output=True)
    if existing.returncode == 0:
        status()
        return
    RUNTIME.mkdir(parents=True, exist_ok=True, mode=0o700)
    RUNTIME.chmod(0o700)
    root = Path(__file__).resolve().parents[1]
    known_hosts = root / '.research-private/public-trial/known_hosts'
    if not known_hosts.exists():
        raise RuntimeError('缺少已验证的隧道主机密钥记录')
    shutil.copy2(known_hosts, RUNTIME / 'known_hosts')
    installed = RUNTIME / 'tunnel.py'
    shutil.copy2(Path(__file__).resolve(), installed)
    PLIST.parent.mkdir(parents=True, exist_ok=True)
    with PLIST.open('wb') as handle:
        plistlib.dump({'Label': LABEL,
            'ProgramArguments': ['/usr/bin/python3', str(installed), 'run'],
            'RunAtLoad': True, 'KeepAlive': True, 'ThrottleInterval': 30,
            'StandardOutPath': str(RUNTIME / 'launch.log'),
            'StandardErrorPath': str(RUNTIME / 'launch-error.log')}, handle)
    PLIST.chmod(0o600)
    subprocess.run(['launchctl', 'bootstrap', domain, str(PLIST)], check=True)
    print('公网隧道已启动；稍后双击“查看公网地址.command”。Mac 需保持开机、联网且用户已登录。')


def stop():
    domain = 'gui/' + str(os.getuid())
    existing = subprocess.run(['launchctl', 'print', domain + '/' + LABEL], capture_output=True)
    if existing.returncode == 0:
        subprocess.run(['launchctl', 'bootout', domain + '/' + LABEL], check=True)
    PLIST.unlink(missing_ok=True)
    if RUNTIME.exists():
        save_status('stopped')
    print('公网入口已关闭，本机平台与文件保留。')


def status():
    if not STATUS.exists():
        print('公网隧道尚未启动。')
        return
    data = json.loads(STATUS.read_text())
    print('状态：' + data['state'])
    if data.get('url'):
        print('访问地址：' + data['url'])
    print('免费试用地址会变化；转发给同事前请以此处地址为准。')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('start', 'stop', 'status', 'run'))
    arguments = parser.parse_args()
    try:
        result = globals()[arguments.action]()
        sys.exit(result if isinstance(result, int) else 0)
    except Exception as error:
        print('操作未完成：' + str(error), file=sys.stderr)
        sys.exit(1)
