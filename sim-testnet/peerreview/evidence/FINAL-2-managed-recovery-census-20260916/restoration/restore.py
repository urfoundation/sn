from pathlib import Path
import datetime
import hashlib
import json
import os
import re
import stat

capture = Path(__file__).resolve().parent
state = Path('/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4')
census = Path('/mnt/data/sn-testnet/qualification/managed-readiness-recovery-census-20260916-r1')
store = state / 'transactions'
os.umask(0o077)

def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()

def write(name, value):
    raw = value.encode() if isinstance(value, str) else value
    with (capture / name).open('xb') as out:
        out.write(raw)
        out.flush()
        os.fsync(out.fileno())

def sha(path):
    with path.open('rb') as src:
        return hashlib.file_digest(src, 'sha256').hexdigest()

def inventory():
    result = {}
    for path in sorted(store.glob('*.rlp')):
        assert re.fullmatch(r'[0-9a-f]{64}\.rlp', path.name)
        assert path.is_file() and not path.is_symlink()
        result[path.name] = sha(path)
    return result

def manifest(data):
    return ''.join(f'{digest}  {name}\n' for name, digest in sorted(data.items()))

write('body.started-at', now() + '\n')
write('owner.pid', str(os.getpid()) + '\n')
code = 1
created = []
try:
    request = json.loads((capture / 'REQUEST.json').read_text())
    for name, digest in request['source_inputs_sha256'].items():
        assert sha(Path(name)) == digest, name
    write('inputs.sha256', manifest({str(capture / 'REQUEST.json'): sha(capture / 'REQUEST.json'), str(Path(__file__)): sha(Path(__file__)), **request['source_inputs_sha256']}))
    for pid in (3628348, 3628509, 3676320):
        assert not Path(f'/proc/{pid}').exists(), f'former startup owner {pid} remains'
    supervisor = json.loads((state / 'supervisor.state.json').read_text())
    assert len(supervisor['processes']) == 33
    assert all(p['pid'] == 0 for p in supervisor['processes'])
    assert store.is_dir() and not store.is_symlink()
    candidates = json.loads((census / 'RESTORATION-CANDIDATES.json').read_text())
    watched_before = {name: sha(Path(name)) for name in candidates['watched_state_sha256']}
    assert watched_before == candidates['watched_state_sha256']
    write('state.before.sha256', manifest(watched_before))
    before = inventory()
    assert len(before) == 2272
    prior = Path('/mnt/data/sn-testnet/qualification/operator-signed-attempt-census-20260916-r1/restore/store.after.sha256')
    prior_map = {line.split(None, 1)[1]: line.split(None, 1)[0] for line in prior.read_text().splitlines()}
    assert before == prior_map
    write('store.before.sha256', manifest(before))
    entries = candidates['candidates']
    assert len(entries) == 14 and len({v['tx_hash'] for v in entries}) == 14
    prepared = []
    for item in entries:
        source = Path(item['candidate_path'])
        source_stat = source.lstat()
        assert source.parent == census / 'private/missing-rlp'
        assert stat.S_ISREG(source_stat.st_mode) and stat.S_IMODE(source_stat.st_mode) == 0o600
        assert source_stat.st_nlink == 1 and source_stat.st_size == item['candidate_bytes'] == 178
        assert re.fullmatch(r'[0-9a-f]{64}\.rlp', source.name)
        assert source.name == item['tx_hash'][2:] + '.rlp'
        raw = source.read_bytes()
        assert hashlib.sha256(raw).hexdigest() == item['candidate_sha256']
        assert item['chain_id'] == 945 and item['value_wei'] == '0'
        assert item['canonical_receipt_status'] == 1 and item['canonical_inclusion_block'] <= 8017664
        assert item['exact_source_and_canonical_receipt_identity_matches']
        target = store / source.name
        assert not target.exists() and not target.is_symlink()
        prepared.append((target, raw, item['candidate_sha256']))
    write('candidates.sha256', manifest({p.name: digest for p, _, digest in prepared}))
    for target, raw, digest in prepared:
        with target.open('xb') as out:
            out.write(raw)
            out.flush()
            os.fsync(out.fileno())
        created.append(target.name)
        assert sha(target) == digest
        assert stat.S_IMODE(target.stat().st_mode) == 0o600
    directory_fd = os.open(store, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory_fd)
    finally:
        os.close(directory_fd)
    after = inventory()
    assert all(after.get(name) == digest for name, digest in before.items())
    assert set(after) - set(before) == set(created)
    assert len(after) == 2286
    write('store.after.sha256', manifest(after))
    watched_after = {name: sha(Path(name)) for name in watched_before}
    write('state.after.sha256', manifest(watched_after))
    assert watched_after == watched_before
    write('RESULT.json', json.dumps({'restored_files': 14, 'restored_bytes': 2492, 'prior_2272_files_unchanged': True, 'watched_state_unchanged': True, 'chain_transactions': 0, 'journal_mutation': False, 'database_mutation': False, 'new_signatures_created': 0, 'restored_names': created}, indent=2) + '\n')
    code = 0
finally:
    write('created-files.json', json.dumps(created, indent=2) + '\n')
    write('body.exit', str(code) + '\n')
    write('body.finished-at', now() + '\n')
