"""Read retained R46 source evidence and write this investigation's receipt only."""
import collections
import datetime
import hashlib
import json
import struct
from pathlib import Path

diagnostic = Path('/mnt/data/sn-testnet/qualification/r46-provisional-resume-20260925/terminal-diagnostics-v3-repair-proof-20260926')
state = Path('/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4')
output = Path(__file__).parent


def witness(path):
    if not path.exists():
        return {'path': str(path), 'exists': False}
    raw = path.read_bytes()
    return {'path': str(path), 'exists': True, 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}


def slot_key(header):
    domain = header['domain']
    data = b'urnetwork/validator-evidence-slot/v1\0'
    data += struct.pack('>Q', domain['chain_id']) + bytes(domain['genesis_hash'])
    data += struct.pack('>H', domain['netuid']) + bytes(domain['coordinator'])
    data += bytes(domain['settlement_vault']) + bytes(domain['deployment_id_hash'])
    data += bytes(header['hotkey']) + struct.pack('>QQB', header['no_id'], header['epoch'], header['kind'])
    subject_hash = bytes(32)
    if header['kind'] == 2:
        subject = header['subject']
        subject_hash = hashlib.sha256(b'urnetwork/validator-evidence-audit-subject/v1\0' + struct.pack('>QQ', subject['observation_epoch'], subject['native_epoch'])).digest()
    return hashlib.sha256(data + subject_hash).hexdigest()


progress = json.loads((diagnostic / 'progress.json').read_text())
capture = next(check for check in progress['checks'] if check['id'] == 'validator-2/signed-source-capture')
sources = capture['evidence']['sources']
journal_path = next(diagnostic / item['artifact']['uri'] for item in sources if item['source']['kind'] == 'relay-journal')
journal = [json.loads(line) for line in journal_path.read_text().splitlines()]
action_entries = collections.defaultdict(list)
for entry in journal:
    action_entries[entry['action_id']].append({name: entry[name] for name in ('sequence', 'plan_hash', 'stage', 'entry_hash')})

handoff_path = state / 'policy-rollover/handoff.json'
handoff = json.loads(handoff_path.read_text())
slot_sources = {}
for item in sources:
    if item['source']['kind'] not in ('signed-evidence', 'signed-audit'):
        continue
    source_path = diagnostic / item['artifact']['uri']
    header = json.loads(source_path.read_text())['header']
    slot = slot_key(header)
    if slot in slot_sources:
        continue
    request_path = state / 'evidence-relay' / (slot + '.json')
    result_path = state / 'evidence-relay' / ('evidence.relay.' + slot + '.receipt.json')
    item = {
        'slot': slot, 'epoch': header['epoch'], 'kind': header['kind'], 'operator': header['no_id'],
        'subject': header['subject'], 'source': witness(source_path),
        'request': witness(request_path), 'result': witness(result_path),
        'journal_entries': action_entries['evidence.relay.' + slot],
        'policy_gap_paths': [str(path) for path in (state / 'evidence-relay/policy-gaps-v2').glob('*/' + slot + '.json')],
    }
    if request_path.exists():
        request = json.loads(request_path.read_text())
        item['request_header_matches'] = request['evidence']['Evidence']['header'] == header
    slot_sources[slot] = item

generation_edges = []
for validator in handoff['validators']:
    generation_edges.append({
        'validator_id': validator['validator_id'],
        'previous_state_dir': validator['previous_state_dir'],
        'successor_state_dir': validator['state_dir'],
        'files': [witness(Path(validator[key]) / name) for key, name in (
            ('previous_state_dir', 'settlement-closures-v2/602.json'),
            ('previous_state_dir', 'settlement-closures-v2/603.json'),
            ('previous_state_dir', 'evidence-publications/epoch-00000000000000000603.json'),
            ('state_dir', 'settlement-closures-v2/604.json'),
            ('state_dir', 'evidence-publications/epoch-00000000000000000604.json'),
        )],
    })

gap_summary = collections.defaultdict(set)
for path in (state / 'evidence-relay/policy-gaps-v2').glob('*/*.json'):
    record = json.loads(path.read_text())['record']
    gap_summary[(record['validator_id'], record['plan_hash'])].add(record['request']['Evidence']['header']['epoch'])

closed = [item for item in slot_sources.values() if item['kind'] == 1]
audits = [item for item in slot_sources.values() if item['kind'] == 2]
report = {
    'schema': 'r46-relay-historical-owner-investigation-v1',
    'observed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'read_only_source_investigation': True, 'rpc_calls': 0,
    'diagnostic': witness(diagnostic / 'progress.json'), 'retained_journal': witness(journal_path),
    'acceptance_window': progress['acceptance_window'],
    'checks': [check for check in progress['checks'] if check['id'] in ('validator-2/relay-publication-readback', 'validator-2/native-application-coverage') and check['id'].endswith('readback')],
    'handoff': {'file': witness(handoff_path), **{name: handoff[name] for name in ('plan_hash', 'source_plan_hash', 'activated', 'generation', 'ledger_continuity_claimed', 'cutoff_epoch', 'first_full_epoch')}},
    'generation_edges': generation_edges,
    'retained_gap_record_epochs': [{'validator_id': owner[0], 'plan_hash': owner[1], 'epochs': sorted(epochs)} for owner, epochs in sorted(gap_summary.items())],
    'summary': {'captured_publications': len(slot_sources), 'closed_census_slots': len(closed),
                'missing_closed_requests': sum(not item['request']['exists'] for item in closed),
                'missing_closed_results': sum(not item['result']['exists'] for item in closed),
                'closed_slots_with_no_journal_owner': sum(not item['journal_entries'] for item in closed),
                'closed_epoch_min': min(item['epoch'] for item in closed), 'closed_epoch_max': max(item['epoch'] for item in closed),
                'audit_slots': len(audits), 'audit_request_headers_match': all(item.get('request_header_matches', False) for item in audits)},
    'slots': list(slot_sources.values()),
    'conclusion': 'No historical owner lookup defect. The required request and journal owner are absent. The predecessor closed-census cursor cannot pass its missing epoch 603, while the approved independent successor begins at 604. Existing audit scheduling is independent and has both captured audit requests. Future generation scheduling can be repaired without advancing the unresolved predecessor; it cannot repair the retained R46 custody gap.',
}
path = output / 'retained-relay-owner.receipt.json'
path.write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({'receipt': str(path), 'summary': report['summary']}, indent=2))
