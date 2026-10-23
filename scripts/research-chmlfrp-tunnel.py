#!/usr/bin/env python3
"""Run the Mac's two platform-only ChmlFrp tunnels as a login LaunchAgent."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import sys
import time
import urllib.request

LABEL = 'local.research.chmlfrp'
RUNTIME = Path.home() / 'Library/Application Support/ResearchChmlFrp'
PLIST = Path.home() / 'Library/LaunchAgents' / (LABEL + '.plist')
PUBLIC_URL = 'https://team-aiorigin.owo.lat'
SOURCE_CLIENT = 'ChmlFrp-0.70.1_261019_darwin_arm64/frpc'


def save_status(state, **extra):
    data = dict(state=state, url=PUBLIC_URL, provider='ChmlFrp', node='中国香港 VIP',
                updated_at=dt.datetime.now(dt.timezone.utc).isoformat(), **extra)
    target = RUNTIME / 'status.json'
    temporary = target.with_suffix('.tmp')
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2))
    temporary.chmod(0o600)
    temporary.replace(target)


def healthy(url):
    try:
        with urllib.request.urlopen(url + '/api/v1/auth/config', timeout=10) as response:
            payload = json.load(response)
        return payload.get('registration_mode') in ('invite_register', 'invite_only')
    except Exception:
        return False


def running():
    return subprocess.run(['launchctl', 'print', f'gui/{os.getuid()}/{LABEL}'],
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def run():
    os.umask(0o077)
    log_path = RUNTIME / 'frpc.log'
    if log_path.exists() and log_path.stat().st_size > 5 * 1024 * 1024:
        log_path.replace(RUNTIME / 'frpc.previous.log')
    stopping = False
    with log_path.open('a') as log:
        child = subprocess.Popen([str(RUNTIME / 'frpc'), '-c', str(RUNTIME / 'frpc.toml')],
                                 stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                                 cwd=str(RUNTIME))
        awake = subprocess.Popen(['/usr/bin/caffeinate', '-i', '-w', str(child.pid)])

        def terminate(_signum, _frame):
            nonlocal stopping
            stopping = True
            if child.poll() is None:
                child.terminate()

        signal.signal(signal.SIGTERM, terminate)
        signal.signal(signal.SIGINT, terminate)
        save_status('connecting', pid=child.pid)
        try:
            next_check = 0
            while child.poll() is None and not stopping:
                if time.monotonic() >= next_check:
                    reachable = healthy(PUBLIC_URL)
                    save_status('connected' if reachable else 'reconnecting', pid=child.pid,
                                public_https_verified=reachable)
                    next_check = time.monotonic() + 30
                time.sleep(1)
            return child.wait(timeout=10)
        finally:
            if child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
            awake.terminate()
            save_status('stopped' if stopping else 'disconnected')


def start():
    if running():
        status()
        return
    if not healthy('http://127.0.0.1:8091'):
        raise RuntimeError('请先启动平台及限制公开注册的公网网关。')
    root = Path(__file__).resolve().parents[1]
    source = root / '.research-private/chmlfrp'
    if not (source / 'frpc.toml').is_file():
        raise RuntimeError('缺少私密配置 .research-private/chmlfrp/frpc.toml')
    RUNTIME.mkdir(parents=True, exist_ok=True, mode=0o700)
    RUNTIME.chmod(0o700)
    for original, destination in ((source / SOURCE_CLIENT, RUNTIME / 'frpc'),
                                  (source / 'frpc.toml', RUNTIME / 'frpc.toml'),
                                  (Path(__file__).resolve(), RUNTIME / 'tunnel.py')):
        shutil.copy2(original, destination)
        destination.chmod(0o700 if destination.name == 'frpc' else 0o600)
    check = subprocess.run([str(RUNTIME / 'frpc'), 'verify', '-c', str(RUNTIME / 'frpc.toml')],
                           capture_output=True)
    if check.returncode:
        raise RuntimeError('Frpc 配置校验失败；未启动公网隧道。')
    PLIST.parent.mkdir(parents=True, exist_ok=True)
    with PLIST.open('wb') as handle:
        plistlib.dump(dict(Label=LABEL,
            ProgramArguments=['/usr/bin/python3', str(RUNTIME / 'tunnel.py'), 'run'],
            WorkingDirectory=str(RUNTIME), RunAtLoad=True, KeepAlive=True,
            ThrottleInterval=30, Umask=0o077,
            StandardOutPath=str(RUNTIME / 'launch.log'),
            StandardErrorPath=str(RUNTIME / 'launch-error.log')), handle)
    PLIST.chmod(0o600)
    subprocess.run(['launchctl', 'bootstrap', f'gui/{os.getuid()}', str(PLIST)], check=True)
    print('ChmlFrp 隧道已启动：' + PUBLIC_URL)
    print('首次启用需等待证书签发；双击“查看公网地址.command”检查实际连通状态。')


def stop():
    if running():
        subprocess.run(['launchctl', 'bootout', f'gui/{os.getuid()}/{LABEL}'], check=True)
        # bootout may return before launchd removes the old job. A following
        # start must not mistake that terminating job for an active tunnel.
        deadline = time.monotonic() + 20
        while running():
            if time.monotonic() >= deadline:
                raise RuntimeError('隧道仍在退出，请稍后再启动。')
            time.sleep(0.25)
    PLIST.unlink(missing_ok=True)
    if RUNTIME.exists():
        save_status('stopped')
    print('ChmlFrp 公网入口已关闭；本机平台、文件和证书保留。')


def status():
    print('固定访问地址：' + PUBLIC_URL)
    if not running():
        print('状态：隧道未启动。')
        return
    print('状态：' + ('HTTPS 访问正常' if healthy(PUBLIC_URL) else '正在重连或公网暂不可达'))
    print('Mac 需保持开机、联网、用户已登录，且 Docker/Colima 和平台正在运行。')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('start', 'stop', 'status', 'run'))
    args = parser.parse_args()
    try:
        result = globals()[args.action]()
        sys.exit(result if isinstance(result, int) else 0)
    except Exception as error:
        print('操作未完成：' + str(error), file=sys.stderr)
        sys.exit(1)
