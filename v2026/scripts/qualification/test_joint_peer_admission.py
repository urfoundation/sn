"""Deterministic admission tests; no compiler, body, Docker or systemd process.

Real immutable local records and locks exercise the production adapter. Process,
manager, resource and joined-wait observations are explicit synthetic seams.
"""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import sys
import subprocess
import tempfile
import threading
import unittest
from unittest import mock

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))
import joint_peer_admission as admission


class JointAdmissionTests(unittest.TestCase):
    """Every refusal starts from an eligible two-owner original fixture."""
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.state = self.root / 'state'
        self.state.mkdir(mode=0o700)
        self.current = {'data': {'bytes': 20 * admission.GIB, 'inodes': 2000000},
                        'docker': {'bytes': admission.INITIAL_DOCKER, 'inodes': 2000000},
                        'memory_available': admission.INITIAL_MEMORY, 'time_ns': 123}
        self.members = {}
        self.observations = {}
        for index, role in enumerate(('compiler', 'short')):
            runner = self.root / (role + '.py')
            runner.write_text('# synthetic pinned runner\n')
            job = self.root / (role + '.job.json')
            adopt = self.root / (role + '.adoption.json')
            job.write_text(json.dumps({'adoption_path': str(adopt)}))
            unit = 'synthetic-' + role + '.service'
            group = '/user.slice/user-1000.slice/user@1000.service/app.slice/' + unit
            member = {'uid': 1000, 'manager': 'user', 'unit': unit, 'cgroup': group,
                      'memory_max': admission.LIMITS[role], 'python': '/synthetic/python',
                      'python_argv': '/synthetic/python', 'runner': admission.pin(runner),
                      'job': admission.pin(job), 'adoption_path': str(adopt),
                      'terminal_path': str(self.root / (role + '.terminal.json')),
                      'body_executables': ['/synthetic/body'], 'body_sha256': ['4' * 64],
                      'maximum_phases': 7 if role == 'compiler' else 49}
            member['argv'] = [member['python'], str(runner), str(job)]
            self.members[role] = member
            row = {'pid': os.getpid() + index, 'ppid': 1, 'start': str(100 + index), 'state': 'S',
                   'uid': 1000, 'euid': 1000, 'cgroup': group, 'executable': member['python'],
                   'argv': member['argv']}
            self.observations[role] = {'unit': unit, 'invocation': str(index + 1) * 32,
                                       'cgroup': group, 'main_pid': row['pid'], 'active': 'active',
                                       'empty': False, 'process': row,
                                       'load_state': 'loaded', 'manager_absent': False}
        plan_path = self.root / 'plan.json'
        plan = {'schema': 'urnetwork-qualification-joint-peer-v1', 'uid': 1000,
                'growth_bytes': admission.GROWTH, 'members': self.members,
                'state_directory': {'path': str(self.state), **admission.identity(self.state.stat())}}
        plan_path.write_text(json.dumps(plan))
        self.plan_pin = admission.pin(plan_path)
        for member in self.members.values():
            Path(member['adoption_path']).write_text(json.dumps({
                'joint_admission': self.plan_pin, 'job_sha256': member['job']['sha256'],
                'runner_sha256': member['runner']['sha256']}))
        self.read_workloads = admission.workload_rows
        self.observe_member_original = admission.observe_member
        patches = [mock.patch.object(admission, 'resources', side_effect=lambda: copy.deepcopy(self.current)),
                   mock.patch.object(admission, 'observe_member', side_effect=self.observe),
                   mock.patch.object(admission, 'workload_rows', side_effect=lambda *_: []),
                   mock.patch.object(admission.subprocess, 'check_output', return_value='')]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def observe(self, member):
        """Stable named fixture observations preserve separate actual generations."""
        role = next(role for role, value in self.members.items() if value['unit'] == member['unit'])
        return copy.deepcopy(self.observations[role])

    def owner(self, role='compiler'):
        """Exercise the real constructor and immutable state publication."""
        member = self.members[role]
        with mock.patch.object(admission.os, 'getpid', return_value=self.observations[role]['main_pid']):
            return admission.JointAdmission(Path(member['job']['path']), Path(member['runner']['path']),
                                            Path(member['adoption_path']), role)

    def terminal(self, owner, **changes):
        """Publish an eligible failed normal root with an actual-wait replay seam."""
        member = self.members['short']
        wait = {'path': str(self.root / 'synthetic-phase.process-result.json'), 'sha256': '5' * 64}
        final = copy.deepcopy(self.current)
        final.update(cgroup=member['cgroup'].lstrip('/'), memory_events='oom 0\noom_kill 0\n')
        terminal = {'job': member['job'], 'adoption': admission.pin(member['adoption_path']),
                    'failure': {}, 'resource_errors': [], 'final': final,
                    'phases': [{'result': {'process_result': wait}}], 'status': 'FAIL_COLLECTED_ALL_NORMAL_ROOTS',
                    'joint_admission': {'plan': self.plan_pin, 'initial': owner.initial,
                        'executor': owner.claims['short'], 'adoption': admission.pin(member['adoption_path']),
                        'all_started_joined': True, 'started': [{'label': 'synthetic-phase', 'wait': wait}]}}
        terminal.update(changes)
        admission.child.durable_json(Path(member['terminal_path']), terminal)
        self.observations['short'].update(main_pid=0, active='inactive', empty=True, process=None)
        return terminal

    def test_one_original_floor_is_shared_after_peer_growth(self):
        first = self.owner()
        initial_bytes = (self.state / 'initial.json').read_bytes()
        self.current['data']['bytes'] -= 5 * admission.GIB
        second = self.owner('short')
        self.assertEqual(first.initial, second.initial)
        self.assertEqual(first.floor, 10 * admission.GIB)
        self.assertEqual(first.floor, second.floor)
        self.assertEqual(initial_bytes, (self.state / 'initial.json').read_bytes())

    def test_absent_before_start_does_not_manufacture_a_generation(self):
        self.observations['short'].update(main_pid=0, active='inactive', empty=True,
                                          invocation='', cgroup='', process=None)
        owner = self.owner()
        self.assertEqual(owner.check(phase=True)['states']['short'], 'not-started')
        self.assertFalse((self.state / 'short.generation.json').exists())

    def test_user_manager_absence_requires_exact_not_found_observation(self):
        member = self.members['short']
        # setUp patches the manager seam for all other actual constructor tests.
        original = self.observe_member_original
        output = 'Id=' + member['unit'] + '\nLoadState=not-found\nActiveState=inactive\n'
        with mock.patch.object(admission.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, output, '')), \
                mock.patch.object(admission.Path, 'exists', return_value=False):
            result = original(member)
        self.assertEqual((result['main_pid'], result['empty'], result['invocation']), (0, True, ''))
        with mock.patch.object(admission.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'unobservable')):
            with self.assertRaisesRegex(admission.Refused, 'manager observation failed'):
                original(member)

    def test_missing_original_baseline_cannot_reset_growth(self):
        owner = self.owner()
        (self.state / 'initial.json').rename(self.root / 'original-initial.json')
        with self.assertRaisesRegex(admission.Refused, 'baseline disappeared'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'initial.json').exists())

    def test_changed_selected_job_bytes_are_refused_before_a_new_phase(self):
        owner = self.owner()
        Path(self.members['short']['job']['path']).write_text('{"different":true}')
        with self.assertRaisesRegex(admission.Refused, 'peer source pin changed'):
            owner.check(phase=True)

    def test_same_unit_system_namespace_is_refused(self):
        self.owner()
        actual = self.observations['short']
        actual['cgroup'] = actual['process']['cgroup'] = '/system.slice/' + self.members['short']['unit']
        with self.assertRaisesRegex(admission.Refused, 'USER namespace'):
            self.owner()

    def test_changed_invocation_and_reused_main_pid_are_refused(self):
        owner = self.owner()
        original = copy.deepcopy(self.observations['short'])
        claim_path = self.state / 'short.generation.json'
        claim = (claim_path.read_bytes(), claim_path.stat().st_ino)
        for field, message in [('invocation', 'joint invocation changed'),
                               ('start', 'joint MainPID generation changed')]:
            self.observations['short'] = copy.deepcopy(original)
            if field == 'invocation':
                self.observations['short']['invocation'] = '9' * 32
            else:
                self.observations['short']['process']['start'] = '999'
            for phase in (False, True):
                with self.assertRaisesRegex(admission.Refused, '^' + message + '$'):
                    owner.check(phase=phase)
                self.assertEqual((claim_path.read_bytes(), claim_path.stat().st_ino), claim)
                self.assertFalse((self.state / 'short.departure.json').exists())
            self.observations['short'] = copy.deepcopy(original)
            self.assertEqual(owner.check(phase=True)['states']['short'], 'live')

    def test_blank_invocation_cannot_hide_live_or_uncollected_generation(self):
        owner = self.owner()
        live = copy.deepcopy(self.observations['short'])
        claim_path = self.state / 'short.generation.json'
        claim = (claim_path.read_bytes(), claim_path.stat().st_ino)
        self.collected()
        collected = copy.deepcopy(self.observations['short'])
        cases = [
            (dict(live, invocation=''), 'joint departure is not an empty stopped generation'),
            (dict(collected, main_pid=live['main_pid']), 'joint departure is not an empty stopped generation'),
            (dict(collected, empty=False), 'joint departure is not an empty stopped generation'),
            (dict(collected, load_state='loaded', manager_absent=False), 'joint invocation changed'),
            (dict(collected, cgroup=live['cgroup']), 'joint invocation changed'),
        ]
        for observed, message in cases:
            self.observations['short'] = observed
            for phase in (False, True):
                with self.assertRaisesRegex(admission.Refused, '^' + message + '$'):
                    owner.check(phase=phase)
            self.assertEqual((claim_path.read_bytes(), claim_path.stat().st_ino), claim)
            self.assertFalse((self.state / 'short.departure.json').exists())
        self.observations['short'] = collected
        self.assertEqual(owner.check()['states']['short'], 'terminal-pending')
        with self.assertRaisesRegex(admission.Refused, 'not yet joined'):
            owner.check(phase=True)
        self.observations['short'] = live
        self.assertEqual(owner.check(phase=True)['states']['short'], 'live')
        self.assertEqual((claim_path.read_bytes(), claim_path.stat().st_ino), claim)

    def test_unrelated_workload_cannot_borrow_the_peer_slot(self):
        owner = self.owner()
        root = copy.deepcopy(self.observations['short']['process'])
        child = {**root, 'pid': root['pid'] + 10, 'ppid': root['pid'], 'start': '777',
                 'workload': 'body', 'protected_executable': True, 'executable_sha256': '4' * 64}
        for group in ('/system.slice/' + self.members['short']['unit'], self.members['short']['cgroup'] + '-other'):
            child['cgroup'] = group
            with self.assertRaisesRegex(admission.Refused, 'unrelated compiler or body'):
                admission.admit_census(self.members, owner.claims, [root, child])

    def test_protected_alias_and_reparented_orphan_keep_original_owner(self):
        owner = self.owner()
        root = copy.deepcopy(self.observations['short']['process'])
        child = {**root, 'pid': root['pid'] + 10, 'ppid': root['pid'], 'start': '777',
                 'workload': 'body', 'protected_executable': True, 'executable_sha256': '4' * 64,
                 'executable': '/memfd:synthetic-private-image (deleted)'}
        result = admission.admit_census(self.members, owner.claims, [root, child])
        self.assertEqual(result, [{'role': 'short', 'pid': child['pid'], 'start': '777'}])
        child['executable_sha256'] = '6' * 64
        with self.assertRaisesRegex(admission.Refused, 'body executable differs'):
            admission.admit_census(self.members, owner.claims, [root, child])

    def test_missing_peer_terminal_blocks_next_phase_without_releasing_claim(self):
        owner = self.owner()
        claim = (self.state / 'short.generation.json').read_bytes()
        self.observations['short'].update(main_pid=0, active='inactive', empty=True, process=None)
        self.assertEqual(owner.check()['states']['short'], 'terminal-pending')
        with self.assertRaisesRegex(admission.Refused, 'not yet joined'):
            owner.check(phase=True)
        self.assertEqual(claim, (self.state / 'short.generation.json').read_bytes())
        self.assertFalse((self.state / 'short.departure.json').exists())

    def test_ordinary_failure_joined_terminal_permits_peer_departure(self):
        owner = self.owner()
        terminal = self.terminal(owner)
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 101}) as replay:
            result = owner.check(phase=True)
        self.assertEqual(result['states']['short'], 'joined-terminal')
        replay.assert_called_once_with(terminal['joint_admission']['started'][0]['wait'],
                                       require_context_verified=False)
        self.assertTrue((self.state / 'short.departure.json').exists())

    def test_unjoined_wait_cannot_be_promoted_by_terminal_claim(self):
        owner = self.owner()
        self.terminal(owner)
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': False, 'exit': 0}):
            with self.assertRaisesRegex(admission.Refused, 'unjoined or invalid'):
                owner.check(phase=True)
        self.assertFalse((self.state / 'short.departure.json').exists())

    def test_terminal_cannot_omit_an_original_phase_wait(self):
        owner = self.owner()
        self.terminal(owner, phases=[])
        with self.assertRaisesRegex(admission.Refused, 'omitted or changed'):
            owner.check(phase=True)

    def test_failed_final_resource_observation_cannot_release_peer(self):
        owner = self.owner()
        final = copy.deepcopy(self.current)
        final['memory_available'] = admission.HEALTHY_MEMORY - 1
        final.update(cgroup=self.members['short']['cgroup'], memory_events='oom 0\n')
        self.terminal(owner, final=final)
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 0}):
            with self.assertRaisesRegex(admission.Refused, 'healthy resource floor'):
                owner.check(phase=True)
        self.assertFalse((self.state / 'short.departure.json').exists())

    def test_replaced_terminal_inode_cannot_rebind_departure(self):
        owner = self.owner()
        terminal = self.terminal(owner)
        path = Path(self.members['short']['terminal_path'])
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}):
            owner.check(phase=True)
            path.rename(self.root / 'original-terminal.json')
            admission.child.durable_json(path, terminal)
            with self.assertRaisesRegex(admission.Refused, 'original record differs'):
                owner.check(phase=True)

    def test_replaced_lock_inode_cannot_split_shared_custody(self):
        owner = self.owner()
        lock = self.state / 'admission.lock'
        lock.rename(self.root / 'original-lock')
        descriptor = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(descriptor)
        with self.assertRaisesRegex(admission.Refused, 'lock inode changed'):
            owner.check()

    def test_legitimate_lock_contention_retries_after_explicit_release(self):
        owner = self.owner()
        descriptor = os.open(self.state / 'admission.lock', os.O_RDWR)
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        blocked, release = threading.Event(), threading.Event()
        results, errors = [], []
        def pause(unused):
            blocked.set()
            if not release.wait(5):
                raise RuntimeError('synthetic contention release missing')
        def check():
            try:
                results.append(owner.check(phase=True))
            except BaseException as exc:
                errors.append(exc)
        with mock.patch.object(admission.time, 'sleep', side_effect=pause):
            worker = threading.Thread(target=check)
            worker.start()
            try:
                self.assertTrue(blocked.wait(5), 'worker did not reach actual held flock')
                fcntl.flock(descriptor, fcntl.LOCK_UN)
                release.set()
            finally:
                os.close(descriptor)
                release.set()
                worker.join(5)
        self.assertFalse(worker.is_alive())
        self.assertEqual(errors, [])
        self.assertEqual(len(results), 1)

    def test_phase_cannot_lower_or_reset_common_child_floor(self):
        owner = self.owner()
        context = mock.Mock()
        with self.assertRaisesRegex(admission.Refused, 'child floor was reset'):
            owner.run_phase(context, ['synthetic'], self.root, 'synthetic-phase', 1,
                            minimum_free=owner.floor - 1)
        context.run.assert_not_called()

    def test_runtime_final_resource_cut_uses_original_common_floor(self):
        owner = self.owner()
        self.current['data']['bytes'] = owner.floor - 1
        with self.assertRaisesRegex(admission.Refused, 'healthy resource floor'):
            owner.check()

    def test_process_exec_and_reparent_transitions_resnapshot_same_generation(self):
        def frame(ppid, start='123'):
            fields = ['0'] * 20
            fields[0], fields[1], fields[19] = 'S', str(ppid), start
            return '424242 (synthetic) ' + ' '.join(fields)
        cases = [([frame(7), frame(8), frame(8), frame(8)],
                  ['old', 'old', 'old', 'old'], [b'old\0'] * 4, 8, 'old'),
                 ([frame(7)] * 4, ['old', 'new', 'new', 'new'],
                  [b'old\0', b'new\0', b'new\0', b'new\0'], 7, 'new')]
        for frames, executables, arguments, expected_parent, expected_executable in cases:
            observed = iter(frames)
            def text(path, *unused, **kwargs):
                if path.name == 'stat':
                    return next(observed)
                if path.name == 'status':
                    return 'Uid:\t1000\t1000\t1000\t1000\n'
                if path.name == 'cgroup':
                    return '0::/synthetic.scope\n'
                raise AssertionError(str(path))
            with mock.patch.object(admission.Path, 'read_text', autospec=True, side_effect=text), \
                    mock.patch.object(admission.Path, 'read_bytes', side_effect=arguments), \
                    mock.patch.object(admission.os, 'readlink', side_effect=executables):
                row = admission.process(424242)
            self.assertEqual((row['ppid'], row['executable'], row['start']),
                             (expected_parent, expected_executable, '123'))

    def test_process_snapshot_never_retries_across_pid_reuse(self):
        def text(path, *unused, **kwargs):
            if path.name == 'stat':
                fields = ['0'] * 20
                fields[0], fields[1], fields[19] = 'S', '7', next(starts)
                return '424242 (synthetic) ' + ' '.join(fields)
            return 'Uid:\t1000\t1000\t1000\t1000\n' if path.name == 'status' else '0::/synthetic.scope\n'
        starts = iter(['123', '124'])
        with mock.patch.object(admission.Path, 'read_text', autospec=True, side_effect=text), \
                mock.patch.object(admission.Path, 'read_bytes', return_value=b'synthetic\0'), \
                mock.patch.object(admission.os, 'readlink', return_value='/synthetic/engine'):
            with self.assertRaisesRegex(admission.Refused, 'PID reused'):
                admission.process(424242)

    def test_process_exit_during_snapshot_is_not_live_workload(self):
        cases = [(['S', 'Z'], [b'synthetic\0']),
                 (['S', 'S', 'Z'], [b'synthetic\0', b'']),
                 (['S', 'S', 'Z'], [b'synthetic\0'] * 3)]
        for states, arguments in cases:
            observed = iter(states)
            def text(path, *unused, **kwargs):
                if path.name == 'stat':
                    fields = ['0'] * 20
                    fields[0], fields[1], fields[19] = next(observed), '7', '123'
                    return '424242 (synthetic) ' + ' '.join(fields)
                if path.name == 'status':
                    return 'Uid:\t1000\t1000\t1000\t1000\n'
                if path.name == 'cgroup':
                    return '0::/synthetic.scope\n'
                raise AssertionError(str(path))
            with mock.patch.object(admission.child, 'compiler_census', return_value={}), \
                    mock.patch.object(admission.Path, 'iterdir', return_value=iter([Path('/proc/424242')])), \
                    mock.patch.object(admission.Path, 'read_text', autospec=True, side_effect=text), \
                    mock.patch.object(admission.Path, 'read_bytes', side_effect=arguments), \
                    mock.patch.object(admission.os, 'readlink', return_value='/synthetic/body'):
                self.assertEqual(self.read_workloads(self.members, {}), [])

    def test_empty_live_process_resnapshots_without_accepting_unknown_or_reused_pid(self):
        cases = [(['123'] * 4, [b'', b'synthetic\0', b'synthetic\0'], None),
                 (['123'] * 6, [b''] * 3, 'unstable after bounded'),
                 (['123', '124'], [b''], 'PID reused')]
        for starts, arguments, refusal in cases:
            observed = iter(starts)
            def text(path, *unused, **kwargs):
                if path.name == 'stat':
                    fields = ['0'] * 20
                    fields[0], fields[1], fields[19] = 'S', '7', next(observed)
                    return '424242 (synthetic) ' + ' '.join(fields)
                if path.name == 'status':
                    return 'Uid:\t1000\t1000\t1000\t1000\n'
                if path.name == 'cgroup':
                    return '0::/synthetic.scope\n'
                raise AssertionError(str(path))
            with mock.patch.object(admission.Path, 'read_text', autospec=True, side_effect=text), \
                    mock.patch.object(admission.Path, 'read_bytes', side_effect=arguments), \
                    mock.patch.object(admission.os, 'readlink', return_value='/synthetic/body') as readlink:
                if refusal:
                    with self.assertRaisesRegex(admission.Refused, refusal):
                        admission.process(424242)
                    readlink.assert_not_called()
                else:
                    row = admission.process(424242)
                    self.assertEqual((row['pid'], row['start'], row['argv']),
                                     (424242, '123', ['synthetic']))

    def test_repeated_unstable_live_process_remains_refused(self):
        parents = iter([7, 8, 8, 9, 9, 10])
        def text(path, *unused, **kwargs):
            if path.name == 'stat':
                fields = ['0'] * 20
                fields[0], fields[1], fields[19] = 'S', str(next(parents)), '123'
                return '424242 (synthetic) ' + ' '.join(fields)
            return 'Uid:\t1000\t1000\t1000\t1000\n' if path.name == 'status' else '0::/synthetic.scope\n'
        with mock.patch.object(admission.Path, 'read_text', autospec=True, side_effect=text), \
                mock.patch.object(admission.Path, 'read_bytes', return_value=b'synthetic\0'), \
                mock.patch.object(admission.os, 'readlink', return_value='/synthetic/engine'):
            with self.assertRaisesRegex(admission.Refused, 'unstable after bounded'):
                admission.process(424242)

    def test_actual_sealed_memfd_bytes_bind_alias_and_unsealed_copy_refuses(self):
        owner = self.owner()
        # The descriptor contains synthetic bytes and is never executed.
        content = b'\x7fELFsynthetic-source-bytes'
        root = self.observations['short']['process']
        row = {**root, 'pid': root['pid'] + 10, 'ppid': root['pid'], 'start': '777',
               'executable': '/memfd:synthetic-alias (deleted)', 'argv': ['synthetic-alias']}
        self.members['short']['body_sha256'] = [hashlib.sha256(content).hexdigest()]
        for sealed in (True, False):
            descriptor = os.memfd_create('synthetic-alias', os.MFD_ALLOW_SEALING | os.MFD_CLOEXEC)
            try:
                os.write(descriptor, content)
                os.lseek(descriptor, 0, os.SEEK_SET)
                if sealed:
                    flags = fcntl.F_SEAL_SEAL | fcntl.F_SEAL_WRITE | fcntl.F_SEAL_GROW | fcntl.F_SEAL_SHRINK
                    fcntl.fcntl(descriptor, fcntl.F_ADD_SEALS, flags)
                with mock.patch.object(admission.child, 'compiler_census', return_value={}), \
                        mock.patch.object(admission.Path, 'iterdir', return_value=iter([Path('/proc') / str(row['pid'])])), \
                        mock.patch.object(admission.Path, 'read_bytes', return_value=b'synthetic-alias\0'), \
                        mock.patch.object(admission.Path, 'read_text', return_value='424242 (synthetic) S 7'), \
                        mock.patch.object(admission, 'process', return_value=copy.deepcopy(row)), \
                        mock.patch.object(admission.os, 'open', side_effect=lambda *unused: os.dup(descriptor)):
                    if sealed:
                        rows = admission._workload_snapshot(self.members, {})
                        self.assertEqual(rows[0]['executable_sha256'], hashlib.sha256(content).hexdigest())
                        self.assertEqual(len(admission.admit_census(self.members, owner.claims, [root, *rows])), 1)
                    else:
                        with self.assertRaisesRegex(admission.Refused, 'memfd is not sealed'):
                            admission._workload_snapshot(self.members, {})
            finally:
                os.close(descriptor)

    def test_parent_exit_census_resnapshots_without_changing_original_claim(self):
        owner = self.owner()
        claim = copy.deepcopy(owner.claims['short'])
        root = copy.deepcopy(self.observations['short']['process'])
        child = {**root, 'pid': root['pid'] + 10, 'ppid': root['pid'] + 20, 'start': '777',
                 'argv': ['synthetic-child'], 'workload': 'body',
                 'protected_executable': True, 'executable_sha256': '4' * 64}
        second = {**child, 'ppid': root['pid']}
        with mock.patch.object(admission, '_workload_snapshot', side_effect=[[root, child], [root, second]]) as snapshots:
            rows = self.read_workloads(self.members, {})
        self.assertEqual(snapshots.call_count, 2)
        self.assertEqual(len(admission.admit_census(self.members, owner.claims, rows)), 1)
        self.assertEqual(owner.claims['short'], claim)

    def collected(self):
        """Use the exact absent USER-manager shape, retaining the original claim."""
        self.observations['short'].update(main_pid=0, active='inactive', empty=True,
            process=None, invocation='', cgroup='', load_state='not-found', manager_absent=True)

    def test_first_collected_departure_requires_original_joined_terminal(self):
        owner = self.owner()
        original = (self.state / 'short.generation.json').read_bytes()
        self.terminal(owner)
        self.collected()
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': False, 'exit': 101}):
            with self.assertRaisesRegex(admission.Refused, 'unjoined or invalid'):
                owner.check(phase=True)
        self.assertFalse((self.state / 'short.departure.json').exists())
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 101}):
            self.assertEqual(owner.check(phase=True)['states']['short'], 'joined-terminal')
        record = json.loads((self.state / 'short.departure.json').read_text())
        observed = record['first_manager_observation']
        self.assertEqual(observed['original_invocation'], owner.claims['short']['invocation'])
        self.assertEqual(observed['observed_invocation'], '')
        self.assertTrue(observed['manager_absent'])
        self.assertEqual(original, (self.state / 'short.generation.json').read_bytes())

    def test_collected_peer_without_terminal_holds_only_next_phase(self):
        owner = self.owner()
        original = (self.state / 'short.generation.json').read_bytes()
        self.collected()
        self.assertEqual(owner.check()['states']['short'], 'terminal-pending')
        with self.assertRaisesRegex(admission.Refused, 'not yet joined'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'short.departure.json').exists())
        self.assertEqual(original, (self.state / 'short.generation.json').read_bytes())
        self.terminal(owner)
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}):
            self.assertEqual(owner.check(phase=True)['states']['short'], 'joined-terminal')

    def test_collection_after_departure_preserves_first_original_record(self):
        owner = self.owner()
        self.terminal(owner)
        path = self.state / 'short.departure.json'
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}):
            owner.check(phase=True)
            original, inode = path.read_bytes(), path.stat().st_ino
            for load_state, absent in [('not-found', True), ('loaded', False)]:
                self.collected()
                self.observations['short'].update(load_state=load_state, manager_absent=absent)
                self.assertEqual(owner.check(phase=True)['states']['short'], 'joined-terminal')
                self.assertEqual((path.read_bytes(), path.stat().st_ino), (original, inode))

    def test_collected_departure_refuses_changed_invocation_and_revival(self):
        owner = self.owner()
        live = copy.deepcopy(self.observations['short'])
        self.terminal(owner)
        self.collected()
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}):
            owner.check(phase=True)
        collected = copy.deepcopy(self.observations['short'])
        paths = [self.state / ('short.' + kind + '.json') for kind in ('generation', 'departure')]
        original_records = [(path.read_bytes(), path.stat().st_ino) for path in paths]
        cases = [(dict(collected, invocation='9' * 32), 'invocation changed'),
                 (dict(live, invocation='9' * 32), 'invocation changed'),
                 (live, 'departed generation became live')]
        reused = copy.deepcopy(live)
        reused['process']['start'] = '999'
        cases.append((reused, 'MainPID generation changed'))
        for observed, message in cases:
            self.observations['short'] = observed
            for phase in (False, True):
                with self.assertRaisesRegex(admission.Refused, message):
                    owner.check(phase=phase)
            self.assertEqual([(path.read_bytes(), path.stat().st_ino) for path in paths], original_records)
        self.observations['short'] = collected
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}):
            self.assertEqual(owner.check(phase=True)['states']['short'], 'joined-terminal')
        self.assertEqual([(path.read_bytes(), path.stat().st_ino) for path in paths], original_records)

    def test_deactivating_peer_defers_terminal_without_canceling_owner(self):
        owner = self.owner()
        self.terminal(owner)
        self.observations['short']['active'] = 'deactivating'
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}) as replay:
            self.assertEqual(owner.check()['states']['short'], 'terminal-pending')
            with self.assertRaisesRegex(admission.Refused, 'not yet joined'):
                owner.check(phase=True)
            replay.assert_not_called()
            self.observations['short']['active'] = 'inactive'
            self.assertEqual(owner.check(phase=True)['states']['short'], 'joined-terminal')

    def test_collected_departure_keeps_original_terminal_required(self):
        owner = self.owner()
        self.terminal(owner)
        self.collected()
        with mock.patch.object(admission.child, 'replay_process_result', return_value={'tree_joined': True, 'exit': 1}):
            owner.check(phase=True)
        Path(self.members['short']['terminal_path']).rename(self.root / 'original-terminal.json')
        with self.assertRaisesRegex(admission.Refused, 'original terminal disappeared'):
            owner.check()

    def failed_compiler_terminal(self, owner, wait=None, phase_changes=None,
                                 evidence_changes=None, **changes):
        """Retain the real compiler runner's failed phase and absent-wait shape."""
        member = self.members['compiler']
        failure = {'type': 'Refused', 'detail': 'synthetic resource observation interrupted'}
        phase = {'job': member['job'], 'status': 'FAIL_OR_INCOMPLETE', 'result': None,
                 'failure': failure, 'resource_errors': [failure]}
        phase.update(phase_changes or {})
        receipt = admission.child.durable_json(self.root / 'synthetic-compiler.receipt.json', phase)
        final = copy.deepcopy(self.current)
        final.update(cgroup=member['cgroup'], memory_events='oom 0\noom_kill 0\n')
        evidence = {'plan': self.plan_pin, 'initial': owner.initial,
                    'executor': owner.claims['compiler'], 'adoption': admission.pin(member['adoption_path']),
                    'all_started_joined': wait is not None, 'replay_error': None,
                    'started': [{'label': 'synthetic-compiler', 'wait': wait}]}
        evidence.update(evidence_changes or {})
        terminal = {'job': member['job'], 'adoption': admission.pin(member['adoption_path']),
                    'status': 'FAIL_OR_INCOMPLETE', 'failure': {'type': 'UnqualifiedContinuation'},
                    'final': final, 'phases': [{'receipt': receipt}], 'joint_admission': evidence}
        terminal.update(changes)
        admission.child.durable_json(Path(member['terminal_path']), terminal)
        self.observations['compiler'].update(main_pid=0, active='failed', empty=True, process=None)
        return terminal

    def retained_wait(self, label, code=-15):
        """Synthetic immutable wait bytes exercise the real replay decoder only."""
        stdout, stderr = b'synthetic original output\n', b'synthetic interrupted guard\n'
        (self.root / (label + '.stdout')).write_bytes(stdout)
        (self.root / (label + '.stderr')).write_bytes(stderr)
        return admission.child.durable_json(self.root / (label + '.process-result.json'), {
            'schema': 'urnetwork-qualification-process-wait-v1', 'label': label,
            'child_context': {'synthetic': True}, 'executable': {'synthetic': True},
            'result': {'argv': ['/synthetic/child'], 'pid': 424242, 'exit': code,
                'stdout_sha256': hashlib.sha256(stdout).hexdigest(),
                'stderr_sha256': hashlib.sha256(stderr).hexdigest(),
                'log_bytes': len(stdout) + len(stderr), 'tree_joined': True,
                'guard_failure': {'type': 'Refused', 'detail': 'synthetic interrupted guard'}}})

    def test_failed_compiler_without_wait_releases_only_stopped_original_slot(self):
        owner = self.owner('short')
        terminal = self.failed_compiler_terminal(owner)
        paths = [self.state / 'initial.json', self.state / 'compiler.generation.json',
                 Path(self.members['compiler']['terminal_path']), self.root / 'synthetic-compiler.receipt.json']
        original = [(path.read_bytes(), path.stat().st_ino) for path in paths]
        context = mock.Mock()
        context.run.return_value = {'synthetic': 'independent body admitted'}
        with mock.patch.object(admission.child, 'replay_process_result') as replay:
            self.assertEqual(owner.check(phase=True)['states']['compiler'], 'stopped-failed-terminal')
            result = owner.run_phase(context, ['/synthetic/independent-body'], self.root,
                                     'synthetic-independent', 1, minimum_free=owner.floor)
            replay.assert_not_called()
        self.assertEqual(result, context.run.return_value)
        context.run.assert_called_once_with(['/synthetic/independent-body'], self.root,
                                            'synthetic-independent', 1, minimum_free=owner.floor)
        record = json.loads((self.state / 'compiler.departure.json').read_text())
        self.assertEqual(record['release'], {'basis': 'stopped-empty-failed-generation',
            'all_started_joined': False, 'missing_wait_labels': ['synthetic-compiler'],
            'test_qualification': 'not-performed'})
        self.assertEqual(record['terminal']['sha256'], admission.pin(paths[2])['sha256'])
        self.assertEqual(terminal['joint_admission']['started'][0]['wait'], None)
        self.assertFalse((self.root / 'synthetic-compiler.process-result.json').exists())
        self.assertEqual([(path.read_bytes(), path.stat().st_ino) for path in paths], original)
        departure = (self.state / 'compiler.departure.json').read_bytes()
        self.observations['compiler'].update(invocation='', cgroup='', active='inactive',
                                             load_state='not-found', manager_absent=True)
        self.assertEqual(owner.check(phase=True)['states']['compiler'], 'stopped-failed-terminal')
        self.assertEqual((self.state / 'compiler.departure.json').read_bytes(), departure)

    def test_failed_peer_actual_signaled_exit_wait_does_not_require_context_postcheck(self):
        owner = self.owner('short')
        wait = self.retained_wait('synthetic-compiler')
        self.failed_compiler_terminal(owner, wait=wait)
        with self.assertRaisesRegex(admission.Refused, 'postcheck is incomplete'):
            admission.child.replay_process_result(wait)
        self.assertEqual(owner.check(phase=True)['states']['compiler'], 'joined-terminal')
        waited = admission.child.replay_process_result(wait, require_context_verified=False)
        self.assertEqual(waited['exit'], -15)
        self.assertFalse(waited['context_verified'])
        self.assertEqual(waited['guard_failure']['detail'], 'synthetic interrupted guard')
        release = json.loads((self.state / 'compiler.departure.json').read_text())['release']
        self.assertEqual(release['basis'], 'replayed-joined-waits')
        self.assertEqual(release['test_qualification'], 'not-performed')

    def test_failed_peer_corrupt_present_wait_cannot_be_treated_as_absent(self):
        owner = self.owner('short')
        wait = self.retained_wait('synthetic-compiler')
        self.failed_compiler_terminal(owner, wait=wait)
        (self.root / 'synthetic-compiler.stdout').write_bytes(b'changed synthetic original\n')
        with self.assertRaisesRegex(admission.Refused, 'retained process output differs'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'compiler.departure.json').exists())

    def test_failed_short_execution_wait_survives_incomplete_body_projection(self):
        owner = self.owner()
        wait = self.retained_wait('synthetic-phase')
        failure = {'type': 'Refused', 'detail': 'synthetic guard stopped before projection'}
        original = {'job': self.members['short']['job'], 'result': None, 'process_result': wait,
                    'failure': failure, 'resource_errors': [failure]}
        receipt = admission.child.durable_json(self.root / 'synthetic-phase.execution.json', original)
        evidence = {'plan': self.plan_pin, 'initial': owner.initial,
                    'executor': owner.claims['short'], 'adoption': admission.pin(self.members['short']['adoption_path']),
                    'all_started_joined': True, 'started': [{'label': 'synthetic-phase', 'wait': wait}]}
        self.terminal(owner, status='FAIL_OR_INCOMPLETE', failure=failure, phases=[],
                      executions=[{'receipt': receipt, 'process_result': wait}], actual_waits=[wait],
                      joint_admission=evidence)
        self.assertEqual(owner.check(phase=True)['states']['short'], 'joined-terminal')
        self.assertEqual(admission.pinned_json(receipt), original)
        self.assertFalse(admission.child.replay_process_result(wait, require_context_verified=False)['context_verified'])

    def test_failed_short_projection_cannot_replace_its_original_execution(self):
        owner = self.owner()
        wait = self.retained_wait('synthetic-phase')
        failure = {'type': 'Refused', 'detail': 'synthetic guard stopped before projection'}
        original = {'job': self.members['short']['job'], 'result': None, 'process_result': wait,
                    'failure': failure, 'resource_errors': [failure]}
        receipt = admission.child.durable_json(self.root / 'synthetic-phase.execution.json', original)
        evidence = {'plan': self.plan_pin, 'initial': owner.initial,
                    'executor': owner.claims['short'], 'adoption': admission.pin(self.members['short']['adoption_path']),
                    'all_started_joined': True, 'started': [{'label': 'synthetic-phase', 'wait': wait}]}
        self.terminal(owner, status='FAIL_OR_INCOMPLETE', failure=failure,
                      phases=[dict(original, failure=None, execution_receipt=receipt)],
                      executions=[{'receipt': receipt, 'process_result': wait}], actual_waits=[wait],
                      joint_admission=evidence)
        with self.assertRaisesRegex(admission.Refused, 'projected phase differs from original execution'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'short.departure.json').exists())

    def test_missing_wait_cannot_be_excused_without_original_phase_failure(self):
        owner = self.owner('short')
        self.failed_compiler_terminal(owner, phase_changes={'failure': None, 'resource_errors': []})
        with self.assertRaisesRegex(admission.Refused, 'missing wait lacks original phase failure'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'compiler.departure.json').exists())

    def test_missing_wait_cannot_be_promoted_to_original_join_claim(self):
        owner = self.owner('short')
        self.failed_compiler_terminal(owner, evidence_changes={'all_started_joined': True})
        with self.assertRaisesRegex(admission.Refused, 'joined-wait claim differs'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'compiler.departure.json').exists())

    def test_failed_terminal_cannot_hide_live_unknown_or_new_peer_generation(self):
        owner = self.owner('short')
        live = copy.deepcopy(self.observations['compiler'])
        self.failed_compiler_terminal(owner)
        stopped = copy.deepcopy(self.observations['compiler'])
        for observed in (dict(stopped, empty=False), dict(stopped, main_pid=live['main_pid']),
                         dict(stopped, active='deactivating')):
            self.observations['compiler'] = observed
            self.assertEqual(owner.check()['states']['compiler'], 'terminal-pending')
            with self.assertRaisesRegex(admission.Refused, 'not yet joined'):
                owner.check(phase=True)
            self.assertFalse((self.state / 'compiler.departure.json').exists())
        self.observations['compiler'] = dict(stopped, invocation='9' * 32)
        with self.assertRaisesRegex(admission.Refused, 'invocation changed'):
            owner.check(phase=True)
        self.observations['compiler'] = live
        self.assertEqual(owner.check(phase=True)['states']['compiler'], 'live')
        self.assertFalse((self.state / 'compiler.departure.json').exists())

    def test_failed_peer_release_keeps_original_floor_and_current_resource_gate(self):
        owner = self.owner('short')
        initial = (self.state / 'initial.json').read_bytes()
        self.failed_compiler_terminal(owner, final=None)
        self.current['data']['bytes'] = owner.floor - 1
        with self.assertRaisesRegex(admission.Refused, 'healthy resource floor'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'compiler.departure.json').exists())
        self.current['data']['bytes'] = owner.floor
        self.assertEqual(owner.check(phase=True)['states']['compiler'], 'stopped-failed-terminal')
        self.assertEqual((self.state / 'initial.json').read_bytes(), initial)

    def test_run_phase_retains_guard_exception_wait_without_qualifying_context(self):
        owner = self.owner()
        original = admission.Refused('synthetic original observation failure')
        def fail(*unused, **kwargs):
            self.retained_wait('synthetic-guard')
            raise original
        context = mock.Mock()
        context.run.side_effect = fail
        with self.assertRaises(admission.Refused) as raised:
            owner.run_phase(context, ['/synthetic/child'], self.root, 'synthetic-guard', 1,
                            minimum_free=owner.floor)
        self.assertIs(raised.exception, original)
        failure = {'type': 'Refused', 'detail': str(original)}
        terminal = owner.terminal({'status': 'FAIL_OR_INCOMPLETE', 'failure': failure})
        self.assertEqual(terminal['failure'], failure)
        self.assertTrue(terminal['joint_admission']['all_started_joined'])
        self.assertFalse(terminal['joint_admission']['all_started_context_verified'])
        self.assertIsNone(terminal['joint_admission']['replay_error'])
        self.assertFalse((self.root / 'synthetic-guard.process-context.json').exists())

    def test_run_phase_preserves_original_exception_when_retained_wait_is_corrupt(self):
        owner = self.owner()
        original = admission.Refused('synthetic original observation failure')
        def fail(*unused, **kwargs):
            self.retained_wait('synthetic-guard')
            (self.root / 'synthetic-guard.stderr').write_bytes(b'changed synthetic bytes')
            raise original
        context = mock.Mock()
        context.run.side_effect = fail
        with self.assertRaises(admission.Refused) as raised:
            owner.run_phase(context, ['/synthetic/child'], self.root, 'synthetic-guard', 1,
                            minimum_free=owner.floor)
        self.assertIs(raised.exception, original)
        self.assertIsNotNone(owner.started[0]['wait'])
        self.assertIn('retained process output differs', owner.started[0]['replay_error']['detail'])
        terminal = owner.terminal({'failure': {'detail': str(original)}})['joint_admission']
        self.assertFalse(terminal['all_started_joined'])
        self.assertFalse(terminal['all_started_context_verified'])
        self.assertIn('retained process output differs', terminal['replay_error']['detail'])

    def read_member_memory(self, role, memory_events):
        """Exercise the actual USER/cgroup reader with explicit kernel-file seams."""
        member, observed = self.members[role], self.observations[role]
        output = '\n'.join(key + '=' + str(value) for key, value in {
            'Id': member['unit'], 'ActiveState': observed['active'],
            'LoadState': observed['load_state'], 'MainPID': observed['main_pid'],
            'InvocationID': observed['invocation'], 'ControlGroup': observed['cgroup']}.items()) + '\n'
        group = Path('/sys/fs/cgroup') / member['cgroup'].lstrip('/')
        files = {group / 'cgroup.events': 'populated ' + ('0' if observed['empty'] else '1') + '\n',
                 group / 'memory.max': str(member['memory_max']) + '\n',
                 group / 'memory.swap.max': '0\n', group / 'memory.events': memory_events}
        with mock.patch.object(admission.subprocess, 'run',
                return_value=subprocess.CompletedProcess([], 0, output, '')) as manager, \
                mock.patch.object(admission.Path, 'exists', autospec=True, side_effect=lambda path: path == group), \
                mock.patch.object(admission.Path, 'read_text', autospec=True,
                                  side_effect=lambda path, *args, **kwargs: files[path]), \
                mock.patch.object(admission, 'process', return_value=copy.deepcopy(observed['process'])):
            result = self.observe_member_original(member)
        self.assertEqual(manager.call_args.args[0][:3], ['/usr/bin/systemctl', '--user', 'show'])
        return result

    def test_stopped_failed_peer_oom_releases_slot_and_retains_original_counters(self):
        owner = self.owner('short')
        events = 'low 0\nhigh 0\nmax 1\noom 1\noom_kill 1\noom_group_kill 0\n'
        final = copy.deepcopy(self.current)
        final.update(cgroup=self.members['compiler']['cgroup'], memory_events=events)
        self.failed_compiler_terminal(owner, final=final)
        path = Path(self.members['compiler']['terminal_path'])
        original = (path.read_bytes(), path.stat().st_ino)
        self.observations['compiler'] = self.read_member_memory('compiler', events)
        self.assertEqual(owner.check(phase=True)['states']['compiler'], 'stopped-failed-terminal')
        departure = json.loads((self.state / 'compiler.departure.json').read_text())
        self.assertEqual(departure['first_manager_observation']['memory_events'], events)
        self.assertFalse(departure['release']['all_started_joined'])
        self.assertEqual(departure['release']['test_qualification'], 'not-performed')
        self.assertEqual((path.read_bytes(), path.stat().st_ino), original)
        context = mock.Mock()
        owner.run_phase(context, ['/synthetic/independent-body'], self.root,
                        'synthetic-after-peer-oom', 1, minimum_free=owner.floor)
        context.run.assert_called_once()
        self.assertFalse((self.root / 'synthetic-compiler.process-result.json').exists())

    def test_live_peer_oom_still_refuses_admission(self):
        owner = self.owner('short')
        def observe(member):
            if member['unit'] == self.members['compiler']['unit']:
                return self.read_member_memory('compiler', 'oom 1\noom_kill 1\n')
            return self.observe(member)
        context = mock.Mock()
        with mock.patch.object(admission, 'observe_member', side_effect=observe):
            with self.assertRaisesRegex(admission.Refused, 'joint peer OOM observed'):
                owner.run_phase(context, ['/synthetic/independent-body'], self.root,
                                'synthetic-live-peer-oom', 1, minimum_free=owner.floor)
        context.run.assert_not_called()
        self.assertFalse((self.state / 'compiler.departure.json').exists())

    def test_current_owner_oom_still_refuses_admission(self):
        owner = self.owner('short')
        def observe(member):
            if member['unit'] == self.members['short']['unit']:
                return self.read_member_memory('short', 'oom 1\noom_kill 0\n')
            return self.observe(member)
        context = mock.Mock()
        with mock.patch.object(admission, 'observe_member', side_effect=observe):
            with self.assertRaisesRegex(admission.Refused, 'joint peer OOM observed'):
                owner.run_phase(context, ['/synthetic/independent-body'], self.root,
                                'synthetic-own-oom', 1, minimum_free=owner.floor)
        context.run.assert_not_called()
        self.assertEqual(owner.started, [])

    def test_stopped_peer_oom_without_original_failure_is_a_contradiction(self):
        owner = self.owner()
        self.terminal(owner)
        self.observations['short'] = self.read_member_memory('short', 'oom 1\noom_kill 1\n')
        with self.assertRaisesRegex(admission.Refused, 'stopped peer OOM lacks original failure'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'short.departure.json').exists())

    def test_stopped_failed_peer_malformed_oom_counters_remain_unknown(self):
        owner = self.owner('short')
        self.failed_compiler_terminal(owner)
        for events in ('oom -1\n', 'oom unknown\n', 'oom 1\noom 0\n', '', 'high 0\n'):
            with self.assertRaisesRegex(admission.Refused, 'memory event evidence differs'):
                self.read_member_memory('compiler', events)
            self.assertFalse((self.state / 'compiler.departure.json').exists())

    def test_failed_final_error_cannot_hide_contradictory_resource_namespace(self):
        owner = self.owner('short')
        self.failed_compiler_terminal(owner, final={
            'error': {'type': 'Refused', 'detail': 'synthetic failed final read'},
            'cgroup': '/system.slice/synthetic-unrelated.service'})
        with self.assertRaisesRegex(admission.Refused, 'failed final resource evidence differs'):
            owner.check(phase=True)
        self.assertFalse((self.state / 'compiler.departure.json').exists())


if __name__ == '__main__':
    unittest.main()
