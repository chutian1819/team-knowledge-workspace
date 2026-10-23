import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('resources', Path(__file__).with_name('research-capacity-resources.py'))
resources = importlib.util.module_from_spec(spec)
spec.loader.exec_module(resources)


class ResourceMetricsTest(unittest.TestCase):
    def test_stream_framing_and_byte_units(self):
        row = {'Name':'research-embedding', 'CPUPerc':'150.5%', 'MemUsage':'512MiB / 2GiB', 'PIDs':'12'}
        parsed = resources.parse_stats('\x1b[H'+json.dumps(row)+'\n')
        self.assertEqual(parsed['memory_working_set_bytes'],512*1024**2)
        self.assertEqual(parsed['memory_effective_limit_bytes'],2*1024**3)
        self.assertEqual(parsed['cpu_percent_one_core'],150.5)
        self.assertEqual(resources.byte_value('1.25GB'),1250000000)

    def test_counter_reset_is_not_a_positive_throttle_or_oom(self):
        before={'cpu_ticks':[10,0,10,10,0,0,0,0], 'cgroups':{'worker':{'cpu':{'usage_usec':1000,'nr_periods':10,'nr_throttled':2,'throttled_usec':100},'memory_events':{'oom_kill':2}}}}
        after={'cpu_ticks':[30,0,30,30,0,0,0,0], 'cgroups':{'worker':{'cpu':{'usage_usec':500,'nr_periods':2,'nr_throttled':0,'throttled_usec':0},'memory_events':{'oom_kill':0}}}}
        resources.add_vm_deltas(before,after,2)
        self.assertIsNone(after['cgroups']['worker']['interval']['usage_usec'])
        self.assertIsNone(after['cgroups']['worker']['new_oom_kill_count'])

    def test_partial_sample_does_not_turn_missing_stats_into_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp)/'samples.jsonl'
            path.write_text(json.dumps({'kind':'metadata'})+'\n'+json.dumps({'kind':'sample','stats':{'unavailable':True,'error_type':'RuntimeError'},'containers':{'unavailable':True}})+'\n')
            report=resources.summarize(path)
            self.assertEqual(report['groups']['all']['cpu_percent_one_core']['samples'],0)
            self.assertEqual(report['unavailable_samples']['stats'],1)
            self.assertEqual(report['containers'],{})

    def test_errors_do_not_leak_credentials(self):
        def fail():
            raise ValueError('redis://password-secret@localhost/0')
        result=resources.guarded(fail)
        self.assertEqual(result,{'unavailable':True,'error_type':'ValueError'})
        self.assertNotIn('password-secret',json.dumps(result))


if __name__ == '__main__':
    unittest.main()
