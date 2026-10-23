#!/usr/bin/env python3
"""Bounded local Langfuse pause drill; always unpause on normal signal/exit.

Uses only the isolated capacity fixture and records no credentials or content.
The application is never paused. Run after a healthy idle preflight.
"""
import argparse
import datetime as dt
import json
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--run-label', required=True)
    parser.add_argument('--trial', default='outage')
    args = parser.parse_args()
    fixture = args.output / args.run_label / 'fixtures.json'
    if not fixture.is_file(): raise SystemExit('Prepare the isolated workload first')
    target = 'research-langfuse-web'
    state = json.loads(subprocess.check_output(['docker', 'inspect', target]))[0]['State']
    if not state['Running'] or state['Paused'] or state.get('Health', {}).get('Status') != 'healthy':
        raise SystemExit('Langfuse must start healthy and unpaused')
    report = {'started_at': dt.datetime.now(dt.timezone.utc).isoformat(), 'pause_seconds': 40,
              'target': target, 'application_paused': False}
    output = args.output / args.run_label / (args.trial + '-drill.json')
    lock = threading.Lock()
    paused = False

    def resume():
        nonlocal paused
        with lock:
            if paused:
                subprocess.run(['docker', 'unpause', target], check=True, stdout=subprocess.DEVNULL, timeout=15)
                paused = False
                report['resumed_at'] = dt.datetime.now(dt.timezone.utc).isoformat()

    def interrupted(signum, frame): raise SystemExit(128 + signum)
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    timer = None
    try:
        subprocess.run(['docker', 'pause', target], check=True, stdout=subprocess.DEVNULL, timeout=15)
        paused = True
        report['paused_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        timer = threading.Timer(40, resume)
        timer.start()
        command = [sys.executable, str(ROOT / 'scripts/research-capacity-test.py'), 'stage',
                   '--output', str(args.output), '--run-label', args.run_label, '--trial', args.trial,
                   '--kind', 'mixed', '--level', '2', '--operations', '3', '--max-writing-calls', '128',
                   '--max-embedding-calls', '10000']
        with output.with_suffix('.log').open('w') as log:
            result = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=300)
        report['workload_exit_code'] = result.returncode
        timer.join(timeout=45)
    finally:
        if timer: timer.cancel()
        resume()
        report['finished_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'drill': str(output), 'unpaused': not paused}))


if __name__ == '__main__': main()
