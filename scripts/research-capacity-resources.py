#!/usr/bin/env python3
"""Read-only, low-frequency resource evidence for the local capacity test.

No requests to models, load generation, queue mutations, config changes, or
restarts. Credentials are read in memory from ignored env files and passed to
redis-cli through stdin, never argv/logs. Container stats/state: every 2 seconds;
host/VM/cgroup/queue gauges: every 10 seconds by default. Missing data stays
missing. Resource samples cannot establish a supported team size or request SLO.

  python3 scripts/research-capacity-resources.py sample --duration 600 --output artifacts/research/capacity/resources.jsonl
  python3 scripts/research-capacity-resources.py baseline --output artifacts/research/capacity/baseline.jsonl
  python3 scripts/research-capacity-resources.py summarize artifacts/research/capacity/resources.jsonl

SIGINT/SIGTERM finish the current bounded read and write a summary. Background
operation is ordinary shell backgrounding; this script never manages services.
"""
from __future__ import annotations

import argparse
import atexit
import concurrent.futures
import ctypes
import datetime as dt
import json
import math
import os
from pathlib import Path
import re
import resource
import signal
import statistics
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[1]
SAFE = re.compile(r"^[A-Za-z0-9_.-]{1,100}$")
ASYNQ_QUEUES = ['default', 'chat_attachment', 'postprocess', 'summary', 'multimodal', 'graph', 'question', 'memory', 'sync', 'low', 'wiki']
INSPECT_FORMAT = '''{"id":"{{.Id}}","name":"{{.Name}}","pid":{{.State.Pid}},"running":{{.State.Running}},"oom_killed":{{.State.OOMKilled}},"restart_count":{{.RestartCount}},"health":"{{with (index .State "Health")}}{{.Status}}{{else}}none{{end}}","memory_limit_bytes":{{.HostConfig.Memory}},"memory_reservation_bytes":{{.HostConfig.MemoryReservation}},"nano_cpus":{{.HostConfig.NanoCpus}},"cpu_quota":{{.HostConfig.CpuQuota}},"cpu_period":{{.HostConfig.CpuPeriod}},"cpu_shares":{{.HostConfig.CpuShares}},"pids_limit":{{if .HostConfig.PidsLimit}}{{.HostConfig.PidsLimit}}{{else}}0{{end}},"project":"{{index .Config.Labels "com.docker.compose.project"}}"}'''

# One VM process for all containers; /proc/<pid>/cgroup avoids assuming Docker's
# cgroup driver/layout. Only counters are returned, never process argv or env.
VM_SCRIPT = r'''
import json,sys,pathlib,os
names=json.loads(sys.argv[1])
def pairs(path,scale=1):
 out={}
 try:
  for line in pathlib.Path(path).read_text().splitlines():
   p=line.replace(':','').split()
   if len(p)>=2 and p[1].isdigit(): out[p[0]]=int(p[1])*scale
 except (OSError,ValueError): pass
 return out
def pressure(path):
 out={}
 try:
  for line in pathlib.Path(path).read_text().splitlines():
   p=line.split(); out[p[0]]={k:float(v) for k,v in (s.split('=') for s in p[1:])}
 except (OSError,ValueError): pass
 return out
def disk(path):
 try:
  x=os.statvfs(path); return {'total_bytes':x.f_blocks*x.f_frsize,'available_bytes':x.f_bavail*x.f_frsize,'free_bytes':x.f_bfree*x.f_frsize}
 except OSError: return None
out={'memory':pairs('/proc/meminfo',1024),'loadavg':[float(x) for x in pathlib.Path('/proc/loadavg').read_text().split()[:3]],'cpu_ticks':[int(x) for x in pathlib.Path('/proc/stat').read_text().splitlines()[0].split()[1:]],'pressure':{n:pressure('/proc/pressure/'+n) for n in ('cpu','memory','io')},'disk':{'root':disk('/'),'docker':disk('/var/lib/docker')},'cgroups':{}}
for name,pid in names.items():
 try:
  c=next(x.split(':',2)[2] for x in pathlib.Path('/proc/%d/cgroup'%pid).read_text().splitlines() if x.startswith('0:'))
  base=pathlib.Path('/sys/fs/cgroup')/c.lstrip('/')
  row={'cpu':pairs(base/'cpu.stat'),'memory_events':pairs(base/'memory.events'),'pressure':{n:pressure(base/(n+'.pressure')) for n in ('cpu','memory','io')}}
  for field in ('memory.current','memory.peak','memory.max','memory.swap.current','pids.current','pids.max'):
   try:
    value=(base/field).read_text().strip(); row[field.replace('.','_')]=int(value) if value.isdigit() else value
   except OSError: pass
  out['cgroups'][name]=row
 except (OSError,ValueError,StopIteration): out['cgroups'][name]={'unavailable':True}
print(json.dumps(out))
'''

# Aggregate gauges only. Pending IDs are used inside Redis solely to read their
# timestamps; IDs, payload, task results and document text never leave Redis.
QUEUE_SCRIPT = r'''
local kind=ARGV[1]
local now=tonumber(redis.call('TIME')[1])
local out={kind=kind,queues={},discovery_complete=true}
local names={}
if kind=='asynq' then names=cjson.decode(ARGV[2])
else
 local cursor='0'; local seen={}; local loops=0
 repeat
  local scan=redis.call('SCAN',cursor,'MATCH','bull:*:meta','COUNT',100)
  cursor=scan[1]; loops=loops+1
  for _,key in ipairs(scan[2]) do
   local name=string.match(key,'^bull:([%w_-]+):meta$')
   if name and not seen[name] then table.insert(names,name); seen[name]=true end
  end
 until cursor=='0' or loops>=10
 out.discovery_complete=(cursor=='0')
end
local function number(cmd,key) return tonumber(redis.call(cmd,key)) or 0 end
local function age(timestamp,scale)
 if timestamp and tonumber(timestamp) then return math.max(0,now-tonumber(timestamp)/scale) end
 return cjson.null
end
for _,name in ipairs(names) do
 local q={}; local prefix
 if kind=='asynq' then
  prefix='asynq:{'..name..'}:'
  q.pending=number('LLEN',prefix..'pending'); q.active=number('LLEN',prefix..'active')
  for _,s in ipairs({'scheduled','retry','archived','completed'}) do q[s]=number('ZCARD',prefix..s) end
  q.processed_total=number('GET',prefix..'processed'); q.failed_total=number('GET',prefix..'failed')
  q.paused=number('EXISTS',prefix..'paused')
  local id=redis.call('LINDEX',prefix..'pending',-1)
  q.oldest_pending_age_seconds=id and age(redis.call('HGET',prefix..'t:'..id,'pending_since'),1e9) or 0
 else
  prefix='bull:'..name..':'
  for _,s in ipairs({'wait','active','paused'}) do q[s]=number('LLEN',prefix..s) end
  for _,s in ipairs({'delayed','prioritized','waiting-children','failed','completed'}) do q[s]=number('ZCARD',prefix..s) end
  local id=redis.call('LINDEX',prefix..'wait',-1)
  q.oldest_wait_age_seconds=id and age(redis.call('HGET',prefix..id,'timestamp'),1000) or 0
  local active=redis.call('LINDEX',prefix..'active',-1)
  q.oldest_active_runtime_seconds=active and age(redis.call('HGET',prefix..active,'processedOn'),1000) or 0
 end
 out.queues[name]=q
end
local info=redis.call('INFO','memory')..redis.call('INFO','stats')..redis.call('INFO','clients')
out.redis={}
for _,field in ipairs({'used_memory','used_memory_peak','maxmemory','evicted_keys','rejected_connections','total_error_replies','blocked_clients','connected_clients'}) do
 out.redis[field]=tonumber(string.match(info,field..':(%d+)')) or 0
end
return cjson.encode(out)
'''


def iso():
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec='milliseconds')


def command(argv, *, data=None, timeout=5):
    """Suppress raw errors: subprocess stderr can contain secret-bearing URLs."""
    try:
        p = subprocess.run(argv, input=data, text=True, stdout=subprocess.PIPE,
                           stderr=subprocess.DEVNULL, timeout=timeout, check=False)
        if p.returncode:
            raise RuntimeError('command_exit_%d' % p.returncode)
        return p.stdout
    except subprocess.TimeoutExpired:
        raise RuntimeError('command_timeout') from None
    except OSError:
        raise RuntimeError('command_unavailable') from None


def guarded(fn, *args):
    try:
        return fn(*args)
    except Exception as exc:
        # Never stringify an arbitrary exception/response/argv.
        return {'unavailable': True, 'error_type': type(exc).__name__}


def env_file(path):
    out = {}
    if not path.exists():
        return out
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith('#') or '=' not in line:
            continue
        key, value = line.removeprefix('export ').split('=', 1)
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in '\"\'':
            value = value[1:-1]
        out[key.strip()] = value
    return out


def byte_value(value):
    match = re.fullmatch(r'\s*([\d.]+)\s*([KMGTPE]?i?B)\s*', value)
    if not match:
        raise ValueError('invalid byte unit')
    number, unit = match.groups()
    exponent = 'BKMGTPE'.index(unit[0]) if unit != 'B' else 0
    return round(float(number) * (1024 if 'i' in unit else 1000) ** exponent)


def group(name):
    if name.startswith('context-workspace-'):
        return 'platform'
    if name.startswith('research-embedding'):
        return 'embedding'
    if name.startswith('research-langfuse-'):
        return 'langfuse'
    return 'other'


def inspect_containers():
    ids = command(['docker', 'ps', '-aq']).split()
    if not ids:
        return []
    lines = command(['docker', 'inspect', '--format', INSPECT_FORMAT, *ids]).splitlines()
    rows = []
    for line in lines:
        x = json.loads(line)
        x['name'] = x['name'].lstrip('/')
        if not SAFE.fullmatch(x['name']):
            continue
        x['group'] = group(x['name'])
        x['cpu_limit_cores'] = x['nano_cpus']/1e9 if x['nano_cpus'] else (x['cpu_quota']/x['cpu_period'] if x['cpu_period'] and x['cpu_quota'] > 0 else None)
        rows.append(x)
    return rows


def parse_stats(line):
    # Docker emits cursor-home/erase ANSI sequences even with JSON --format
    # when streaming to a pipe. They are framing, not JSON content.
    x = json.loads(re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]', '', line).strip())
    if not SAFE.fullmatch(x.get('Name', '')):
        raise ValueError('invalid container name')
    usage, limit = x['MemUsage'].split('/')
    return {'name': x['Name'], 'group': group(x['Name']),
            'cpu_percent_one_core': float(x['CPUPerc'].rstrip('%')),
            'memory_working_set_bytes': byte_value(usage),
            'memory_effective_limit_bytes': byte_value(limit),
            'pids': int(x['PIDs']) if x['PIDs'].isdigit() else None}


class StatsStream:
    """Reuse Docker's streaming connection, avoiding one expensive startup/2s."""
    def __init__(self):
        self.ids = (); self.process = None; self.thread = None
        self.rows = {}; self.lock = threading.Lock(); self.ready = threading.Event()

    def close(self):
        if self.process:
            self.process.terminate()
            try: self.process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.process.kill(); self.process.wait(timeout=2)
            if self.thread: self.thread.join(timeout=2)
            self.process = None

    def ensure(self, states):
        ids = tuple(sorted(x['id'] for x in states if x['running']))
        if ids == self.ids and self.process and self.process.poll() is None:
            return
        self.close(); self.ids = ids
        with self.lock: self.rows = {}
        self.ready.clear()
        if not ids: return
        self.process = subprocess.Popen(['docker','stats','--no-trunc','--format','{{json .}}',*ids],
                                         text=True,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
        def read():
            for line in self.process.stdout:
                try: row = parse_stats(line)
                except (ValueError,KeyError): continue
                with self.lock:
                    self.rows[row['name']] = (time.monotonic(), row)
                    if len(self.rows) == len(ids): self.ready.set()
        self.thread=threading.Thread(target=read,daemon=True); self.thread.start()

    def snapshot(self):
        if not self.ids: return []
        self.ready.wait(timeout=4)
        if not self.ready.is_set(): raise RuntimeError('stats_not_ready')
        now=time.monotonic()
        with self.lock:
            result=[{**row,'sample_age_seconds':now-at} for at,row in self.rows.values()]
        if any(row['sample_age_seconds']>5 for row in result): raise RuntimeError('stale_stats')
        return result


def disk(path):
    s = os.statvfs(path)
    return {'total_bytes': s.f_blocks*s.f_frsize, 'available_bytes': s.f_bavail*s.f_frsize,
            'free_bytes': s.f_bfree*s.f_frsize}


def host_metrics():
    vm = command(['vm_stat'])
    page_size = int(re.search(r'page size of (\d+) bytes', vm)[1])
    pages = {k.strip(): int(v)*page_size for k, v in re.findall(r'^([^:\n]+):\s*(\d+)\.', vm, re.M)}
    pressure = command(['memory_pressure', '-Q'])
    percent = re.search(r'System-wide memory free percentage:\s*(\d+)%', pressure)
    swap = command(['sysctl', '-n', 'vm.swapusage'])
    swaps = {k: byte_value(v+'iB') for k, v in re.findall(r'(total|used|free)\s*=\s*([\d.]+[KMGT])', swap)}
    # Mach CPU ticks are O(1); `top` scans every host process and distorted the
    # idle baseline. Their delta is calculated only between actual samples.
    lib=ctypes.CDLL('/usr/lib/libSystem.B.dylib')
    ticks=(ctypes.c_uint*4)(); count=ctypes.c_uint(4)
    lib.mach_host_self.restype=ctypes.c_uint
    status=lib.host_statistics(lib.mach_host_self(),3,ticks,ctypes.byref(count))
    return {'memory_pages_bytes': pages, 'memory_pressure_free_percent': int(percent[1]) if percent else None,
            'swap_bytes': swaps, 'cpu_ticks':list(ticks) if status==0 else None,
            'loadavg': list(os.getloadavg()), 'disk': disk(ROOT), 'captured_at': iso()}


def vm_metrics(containers):
    pids = {x['name']: x['pid'] for x in containers if x['running'] and x['pid'] > 0}
    row = json.loads(command(['colima', 'ssh', '--', 'python3', '-c', VM_SCRIPT, json.dumps(pids)], timeout=5))
    row['captured_at'] = iso()
    return row


def queue_metrics(kind, secrets, args):
    env = secrets['platform' if kind == 'asynq' else 'langfuse']
    password = env.get('REDIS_PASSWORD' if kind == 'asynq' else 'LF_REDIS_PASSWORD', '')
    username = env.get('REDIS_USERNAME', '') if kind == 'asynq' else ''
    database = env.get('REDIS_DB', '0') if kind == 'asynq' else '0'
    if '\n' in password or '\n' in username or not database.isdigit():
        raise ValueError('invalid redis configuration')
    container = args.platform_redis if kind == 'asynq' else args.langfuse_redis
    # read builtins consume secrets from stdin. Neither password nor username
    # is interpolated into the local command line or written to an artifact.
    shell = 'IFS= read -r REDISCLI_AUTH; export REDISCLI_AUTH; IFS= read -r redis_user; if [ -n "$redis_user" ]; then set -- "$@" --user "$redis_user"; fi; script=$(cat); exec redis-cli --json -n "$1" ${4:+$4} ${5:+"$5"} EVAL "$script" 0 "$2" "$3"'
    raw = command(['docker', 'exec', '-i', container, 'sh', '-c', shell, 'metrics', database, kind, json.dumps(ASYNQ_QUEUES)],
                  data=password+'\n'+username+'\n'+QUEUE_SCRIPT, timeout=4)
    row = json.loads(json.loads(raw))
    row['captured_at'] = iso()
    return row


def hardware():
    raw = command(['sysctl', '-n', 'hw.model', 'hw.ncpu', 'hw.physicalcpu', 'hw.memsize']).splitlines()
    c = json.loads(command(['colima', 'status', '--json']))
    return {'host': {'model': raw[0], 'logical_cpus': int(raw[1]), 'physical_cpus': int(raw[2]), 'memory_bytes': int(raw[3])},
            'vm': {k: c.get(k) for k in ('cpu', 'memory', 'disk', 'arch', 'driver')}}


def safe_configuration():
    """Allowed concurrency values only, never arbitrary env or command arrays."""
    raw = command(['docker', 'inspect', 'context-workspace-app', 'research-embedding'])
    result = {}
    for x in json.loads(raw):
        selected = {}
        for item in x['Config'].get('Env', []):
            k, _, v = item.partition('=')
            if (k.startswith('WEKNORA_ASYNQ_') or k in ('WEKNORA_WIKI_ASYNQ_CONCURRENCY', 'OLLAMA_NUM_PARALLEL', 'OLLAMA_MAX_LOADED_MODELS')) and v.isdigit():
                selected[k] = int(v)
        result[x['Name'].lstrip('/')] = selected
    return result


def counter_delta(previous, current):
    return max(0, current-previous) if current >= previous else None


def add_vm_deltas(previous, current, elapsed):
    if not previous or previous.get('unavailable') or current.get('unavailable') or elapsed <= 0:
        return
    a, b = previous.get('cpu_ticks', []), current.get('cpu_ticks', [])
    if len(a) >= 8 and len(b) >= 8:
        deltas = [y-x for x,y in zip(a[:8],b[:8])]
        total = sum(deltas)
        if total > 0 and min(deltas) >= 0:
            current['cpu_busy_percent'] = 100*(total-deltas[3]-deltas[4])/total
    for name, row in current.get('cgroups', {}).items():
        old = previous.get('cgroups', {}).get(name, {})
        if row.get('unavailable') or not old or old.get('unavailable'):
            continue
        cpu, before = row.get('cpu', {}), old.get('cpu', {})
        delta = {k: counter_delta(before[k], cpu[k]) for k in ('nr_periods','nr_throttled','throttled_usec','usage_usec') if k in cpu and k in before}
        row['interval'] = {'elapsed_seconds': elapsed, **delta}
        if delta.get('nr_periods'):
            row['interval']['throttled_period_percent'] = 100*delta['nr_throttled']/delta['nr_periods'] if delta.get('nr_throttled') is not None else None
        if delta.get('usage_usec') is not None:
            row['interval']['cpu_percent_one_core'] = 100*delta['usage_usec']/1e6/elapsed
        row['new_oom_kill_count'] = counter_delta(old.get('memory_events',{}).get('oom_kill',0),row.get('memory_events',{}).get('oom_kill',0))


def numbers_summary(values):
    clean = [v for v in values if isinstance(v, (float,int)) and math.isfinite(v)]
    if not clean:
        return {'samples': 0}
    return {'samples': len(clean), 'min': min(clean), 'average': statistics.mean(clean), 'peak': max(clean)}


def summarize(path):
    rows = [json.loads(line) for line in path.read_text().splitlines() if line.strip()]
    samples = [r for r in rows if r.get('kind') == 'sample']
    metadata = next((r for r in rows if r.get('kind') == 'metadata'), {})
    result = {'kind': 'summary', 'generated_at': iso(), 'sample_count': len(samples), 'hardware': metadata.get('hardware'),
              'definitions': {'cpu': 'Docker percent: 100% = one vCPU. VM busy percent has 100% = whole VM.',
                              'memory': 'Docker working set and cgroup resident counters differ; do not sum per-container peaks as simultaneous usage.',
                              'queues': 'Point-in-time oldest pending/wait age, not measured request p95. LF wait age excludes delayed/prioritized queues; counts still included.',
                              'oom': 'Initial OOMKilled flag may be historical. New cgroup oom_kill deltas/restart changes are reported separately.',
                              'limits': 'A 2s sample can miss short spikes. Background gauges use slow_interval. No supported team-size conclusion.'},
              'containers': {}, 'groups': {}, 'queues': {}, 'queue_redis': {}, 'headroom': {}, 'collector': {}, 'unavailable_samples': {}}
    names = sorted({x['name'] for r in samples for x in r.get('stats',[]) if isinstance(x,dict)})
    vm_samples = [r['vm'] for r in samples if isinstance(r.get('vm'),dict) and not r['vm'].get('unavailable')]
    for name in names:
        stats = [x for r in samples for x in r.get('stats',[]) if isinstance(x,dict) and x.get('name')==name]
        states = [x for r in samples for x in r.get('containers',[]) if isinstance(x,dict) and x.get('name')==name]
        cgroups = [r.get('cgroups',{}).get(name,{}) for r in vm_samples]
        result['containers'][name] = {
            'group':group(name), 'cpu_percent_one_core':numbers_summary([x['cpu_percent_one_core'] for x in stats]),
            'memory_working_set_bytes':numbers_summary([x['memory_working_set_bytes'] for x in stats]),
            'memory_current_bytes':numbers_summary([x.get('memory_current') for x in cgroups]),
            'throttled_period_percent':numbers_summary([x.get('interval',{}).get('throttled_period_percent') for x in cgroups]),
            'throttled_microseconds_in_intervals':sum(x.get('interval',{}).get('throttled_usec') or 0 for x in cgroups),
            'new_cgroup_oom_kills':sum(x.get('new_oom_kill_count') or 0 for x in cgroups),
            'initial_oom_killed':states[0]['oom_killed'] if states else None,
            'oom_flag_became_true':any(not a['oom_killed'] and b['oom_killed'] for a,b in zip(states,states[1:])),
            'restart_count_growth':sum(max(0,b['restart_count']-a['restart_count']) for a,b in zip(states,states[1:]) if a['id']==b['id']),
            'container_replacements':sum(a['id']!=b['id'] for a,b in zip(states,states[1:])),
            'unhealthy_samples':sum(x['health']=='unhealthy' for x in states),
            'latest_limits':{k:states[-1][k] for k in ('cpu_limit_cores','cpu_shares','memory_limit_bytes','memory_reservation_bytes')} if states else {}}
    for g in ('platform','embedding','langfuse','other','all'):
        valid = [r for r in samples if isinstance(r.get('stats'),list)]
        result['groups'][g] = {metric:numbers_summary([sum(x[metric] for x in r['stats'] if g=='all' or x['group']==g) for r in valid]) for metric in ('cpu_percent_one_core','memory_working_set_bytes')}
    for kind in ('asynq','langfuse'):
        valid = [r[kind] for r in samples if isinstance(r.get(kind),dict) and not r[kind].get('unavailable')]
        qnames = sorted({n for r in valid for n in r.get('queues',{})})
        result['queues'][kind] = {name:{field:numbers_summary([r.get('queues',{}).get(name,{}).get(field) for r in valid]) for field in sorted({f for r in valid for f in r.get('queues',{}).get(name,{})})} for name in qnames}
        result['queues'][kind+'_discovery_complete'] = bool(valid) and all(r.get('discovery_complete') for r in valid)
        result['queue_redis'][kind] = {}
        for field in ('used_memory','used_memory_peak','maxmemory','evicted_keys','rejected_connections','total_error_replies','blocked_clients','connected_clients'):
            values=[r.get('redis',{}).get(field) for r in valid]
            result['queue_redis'][kind][field]=numbers_summary(values)
            if field in ('evicted_keys','rejected_connections','total_error_replies') and values and all(isinstance(v,int) for v in values):
                result['queue_redis'][kind][field]['observed_growth']=sum(max(0,b-a) for a,b in zip(values,values[1:]))
                result['queue_redis'][kind][field]['counter_resets']=sum(b<a for a,b in zip(values,values[1:]))
    hosts = [r['host'] for r in samples if isinstance(r.get('host'),dict) and not r['host'].get('unavailable')]
    result['headroom'] = {'vm_mem_available_bytes':numbers_summary([r.get('memory',{}).get('MemAvailable') for r in vm_samples]),
                          'vm_docker_disk_available_bytes':numbers_summary([(r.get('disk',{}).get('docker') or {}).get('available_bytes') for r in vm_samples]),
                          'vm_cpu_busy_percent':numbers_summary([r.get('cpu_busy_percent') for r in vm_samples]),
                          'host_memory_pressure_free_percent':numbers_summary([r.get('memory_pressure_free_percent') for r in hosts]),
                          'host_cpu_busy_percent':numbers_summary([r.get('cpu_busy_percent') for r in hosts]),
                          'host_swap_used_bytes':numbers_summary([r.get('swap_bytes',{}).get('used') for r in hosts]),
                          'host_disk_available_bytes':numbers_summary([r.get('disk',{}).get('available_bytes') for r in hosts])}
    result['collector'] = {field:numbers_summary([r.get('collector',{}).get(field) for r in samples]) for field in ('elapsed_seconds','self_cpu_seconds','child_cpu_seconds','interval_overrun_seconds')}
    result['collector']['run_totals'] = next((r for r in reversed(rows) if r.get('kind')=='collector_totals'), None)
    for field in ('stats','containers','host','vm','asynq','langfuse'):
        result['unavailable_samples'][field] = sum(isinstance(r.get(field),dict) and r[field].get('unavailable',False) for r in samples)
    return result


def write_json(path, value):
    path.parent.mkdir(parents=True,exist_ok=True)
    with path.open('w') as f:
        os.chmod(path,0o600)
        json.dump(value,f,ensure_ascii=False,indent=2)
        f.write('\n')


def sample(args):
    if args.output.exists():
        raise ValueError('output already exists; choose a fresh path')
    collector_start=time.monotonic()
    secrets = {'platform':env_file(args.env_file), 'langfuse':env_file(args.observability_env)}
    usage_start=resource.getrusage(resource.RUSAGE_SELF)
    children_start=resource.getrusage(resource.RUSAGE_CHILDREN)
    metadata = {'kind':'metadata','at':iso(),'interval_seconds':args.interval,'slow_interval_seconds':args.slow_interval,
                'hardware':guarded(hardware),'safe_configuration':guarded(safe_configuration),
                'scope':'read-only host, Colima, Docker stats/state/cgroups, aggregate Asynq/BullMQ gauges; no model/load/business mutation'}
    args.output.parent.mkdir(parents=True,exist_ok=True)
    stopping = False
    def stop(*_):
        nonlocal stopping
        stopping = True
    signal.signal(signal.SIGTERM,stop)
    signal.signal(signal.SIGINT,stop)
    start = time.monotonic(); last_slow = -math.inf; prior_vm = None; prior_vm_time = None; prior_host=None; count = 0
    last_states = []
    stream=StatsStream()
    atexit.register(stream.close)
    with args.output.open('x') as f, concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        os.chmod(args.output,0o600)
        f.write(json.dumps(metadata,ensure_ascii=False)+'\n'); f.flush()
        while not stopping and (args.duration==0 or time.monotonic()-start<args.duration):
            began = time.monotonic(); count += 1
            ru = resource.getrusage(resource.RUSAGE_SELF); children = resource.getrusage(resource.RUSAGE_CHILDREN)
            slow = began-last_slow>=args.slow_interval
            jobs = {'containers':pool.submit(guarded,inspect_containers)}
            if slow:
                jobs['host']=pool.submit(guarded,host_metrics)
                jobs['asynq']=pool.submit(guarded,queue_metrics,'asynq',secrets,args)
                jobs['langfuse']=pool.submit(guarded,queue_metrics,'langfuse',secrets,args)
            result = {'kind':'sample','at':iso(),'elapsed_from_start_seconds':began-start}
            result['containers']=jobs.pop('containers').result()
            if isinstance(result['containers'],list): last_states=result['containers']
            stream.ensure(last_states)
            jobs['stats']=pool.submit(guarded,stream.snapshot)
            if slow: jobs['vm']=pool.submit(guarded,vm_metrics,last_states)
            for key,future in jobs.items(): result[key]=future.result()
            if slow:
                now=time.monotonic()
                add_vm_deltas(prior_vm,result['vm'],now-prior_vm_time if prior_vm_time else 0)
                if not result['vm'].get('unavailable'): prior_vm=result['vm']; prior_vm_time=now
                host=result['host']
                if not host.get('unavailable') and host.get('cpu_ticks'):
                    if prior_host:
                        delta=[b-a for a,b in zip(prior_host['cpu_ticks'],host['cpu_ticks'])]
                        if min(delta)>=0 and sum(delta)>0: host['cpu_busy_percent']=100*(sum(delta)-delta[2])/sum(delta)
                    prior_host=host
                last_slow=began
            elapsed=time.monotonic()-began
            ru2=resource.getrusage(resource.RUSAGE_SELF); child2=resource.getrusage(resource.RUSAGE_CHILDREN)
            result['collector']={'elapsed_seconds':elapsed,'self_cpu_seconds':ru2.ru_utime+ru2.ru_stime-ru.ru_utime-ru.ru_stime,
                                 'child_cpu_seconds':child2.ru_utime+child2.ru_stime-children.ru_utime-children.ru_stime,
                                 'interval_overrun_seconds':max(0,elapsed-args.interval)}
            f.write(json.dumps(result,ensure_ascii=False)+'\n'); f.flush()
            if args.once: break
            deadline=began+args.interval
            while not stopping and time.monotonic()<deadline:
                time.sleep(min(.2,deadline-time.monotonic()))
    stream.close()
    usage_end=resource.getrusage(resource.RUSAGE_SELF)
    children_end=resource.getrusage(resource.RUSAGE_CHILDREN)
    wall=time.monotonic()-collector_start
    own_cpu=usage_end.ru_utime+usage_end.ru_stime-usage_start.ru_utime-usage_start.ru_stime
    child_cpu=children_end.ru_utime+children_end.ru_stime-children_start.ru_utime-children_start.ru_stime
    with args.output.open('a') as f:
        f.write(json.dumps({'kind':'collector_totals','wall_seconds':wall,'self_cpu_seconds':own_cpu,
                            'child_cpu_seconds_including_stats_stream':child_cpu,
                            'host_one_core_cpu_percent':100*(own_cpu+child_cpu)/wall,
                            'note':'Host collector/CLI CPU only; remote VM probe is reflected in VM totals. Startup is included.'})+'\n')
    report=summarize(args.output)
    target=args.output.with_suffix('.summary.json')
    write_json(target,report)
    gaps={k:v for k,v in report['unavailable_samples'].items() if v}
    print(json.dumps({'status':'completed_with_gaps' if gaps else 'completed','unavailable_samples':gaps,'samples':count,'output':str(args.output),'summary':str(target)},ensure_ascii=False))


def main():
    p=argparse.ArgumentParser(description=__doc__,formatter_class=argparse.RawDescriptionHelpFormatter)
    sub=p.add_subparsers(dest='mode',required=True)
    for mode in ('sample','baseline'):
        s=sub.add_parser(mode)
        s.add_argument('--output',type=Path,required=True)
        s.add_argument('--duration',type=float,default=0,help='Seconds; 0 runs until SIGINT/SIGTERM')
        s.add_argument('--interval',type=float,default=2)
        s.add_argument('--slow-interval',type=float,default=10)
        s.add_argument('--env-file',type=Path,default=ROOT/'.env')
        s.add_argument('--observability-env',type=Path,default=ROOT/'.research-observability.env')
        s.add_argument('--platform-redis',default='context-workspace-redis')
        s.add_argument('--langfuse-redis',default='research-langfuse-redis')
    s=sub.add_parser('summarize'); s.add_argument('input',type=Path); s.add_argument('--output',type=Path)
    args=p.parse_args()
    try:
        if args.mode=='summarize':
            report=summarize(args.input)
            target=args.output or args.input.with_suffix('.summary.json'); write_json(target,report)
            print(json.dumps({'summary':str(target)},ensure_ascii=False)); return
        args.once=args.mode=='baseline'
        if args.interval<2 or args.slow_interval<args.interval or args.duration<0:
            p.error('interval must be >=2s, slow-interval >= interval, duration >=0')
        if not SAFE.fullmatch(args.platform_redis) or not SAFE.fullmatch(args.langfuse_redis):
            p.error('invalid local container name')
        sample(args)
    except Exception as exc:
        print(json.dumps({'status':'failed','error_type':type(exc).__name__,'note':'Details suppressed to avoid accidental credential disclosure.'}),file=sys.stderr)
        sys.exit(1)


if __name__=='__main__':
    main()
