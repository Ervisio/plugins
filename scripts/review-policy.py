#!/usr/bin/env python3
"""Conservative auto-merge policy. Read objects with git; never execute PR code."""
import json
import re
import subprocess
import sys


def git(*args):
    return subprocess.check_output(['git', *args], text=True).strip()


def read(ref, path):
    return json.loads(git('show', f'{ref}:{path}'))


def eligible(base, head):
    paths = git('diff', '--name-only', base, head).splitlines()
    if len(paths) != 1 or not re.fullmatch(r'plugins/[a-z][a-z0-9-]{1,39}\.json', paths[0]):
        return False
    path = paths[0]
    old, new = read(base, path), read(head, path)
    source = next((p for p in read(base, 'registry.json')['plugins'] if p['id'] == old['id']), None)
    if not source or source['trust'] != 'team' or source['repo'].split('/')[0].lower() != 'ervisio':
        return False
    if new['id'] != old['id'] or new['repo'] != old['repo'] or new['repo'] != source['repo']:
        return False
    version = re.compile(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')
    if not version.fullmatch(old['version']) or not version.fullmatch(new['version']):
        return False
    if tuple(map(int, new['version'].split('.'))) <= tuple(map(int, old['version'].split('.'))):
        return False
    # Compare complete JSON values, including future/unknown permission fields.
    # This deliberately sends even permission reductions to manual review.
    for field in ('capabilities', 'visibleTo', 'platforms', 'requires'):
        if old['manifest'].get(field) != new['manifest'].get(field):
            return False
    return True


if __name__ == '__main__':
    try:
        allowed = len(sys.argv) == 3 and eligible(*sys.argv[1:])
    except (KeyError, ValueError, StopIteration, subprocess.CalledProcessError):
        allowed = False
    print('Automatic team update' if allowed else 'Maintainer review required')
    sys.exit(0 if allowed else 1)
