#!/usr/bin/env python3
"""Archive explicitly listed peripheral resources; never execute vendor code."""
import argparse
import hashlib
import html
from html.parser import HTMLParser
import json
from pathlib import Path
import subprocess
import urllib.parse

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / 'docs/hardware/peripherals/resources-manifest.json'


def sha256(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


class Page(HTMLParser):
    def __init__(self):
        super().__init__()
        self.text = []
        self.links = []
        self.skip = 0

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag in ('script', 'style'):
            self.skip += 1
        if tag in ('p', 'div', 'li', 'h1', 'h2', 'h3', 'tr', 'br', 'pre'):
            self.text.append('\n')
        if tag in ('td', 'th'):
            self.text.append(' | ')
        for attr in ('href', 'src'):
            if attrs.get(attr) and not attrs[attr].startswith(('data:', '#')):
                self.links.append(attrs[attr])

    def handle_endtag(self, tag):
        if tag in ('script', 'style'):
            self.skip = max(0, self.skip - 1)

    def handle_data(self, data):
        if not self.skip:
            self.text.append(data)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('url')
    parser.add_argument('destination', help='relative path under reference/peripherals')
    parser.add_argument('--note', default='')
    args = parser.parse_args()
    target = (ROOT / args.destination).resolve()
    if not target.is_relative_to(ROOT / 'reference/peripherals'):
        parser.error('destination must be inside reference/peripherals')
    target.parent.mkdir(parents=True, exist_ok=True)
    part = target.with_name(target.name + '.part')
    if not target.exists():
        subprocess.run(['curl', '-fLsS', '-A', 'Mozilla/5.0', '--retry', '3', '--connect-timeout', '20',
                        '--max-time', '180', '-C', '-', '-o', str(part), args.url], check=True)
        part.replace(target)
    records = json.loads(MANIFEST.read_text()) if MANIFEST.exists() else []
    record = {'path':str(target.relative_to(ROOT)), 'url':args.url,
              'retrieved':'2026-09-16', 'bytes':target.stat().st_size,
              'sha256':sha256(target), 'note':args.note}
    if target.suffix == '.html':
        p = Page()
        p.feed(target.read_text(errors='replace'))
        readable = '\n'.join(s.strip() for s in ''.join(p.text).splitlines() if s.strip())
        if ('滑动验证' in readable or 'Just a moment' in readable) and len(readable) < 3000:
            raise RuntimeError('Downloaded challenge page; do not treat as documentation')
        target.with_suffix('.txt').write_text('SOURCE: '+args.url+'\n\n'+readable+'\n')
        links = sorted(set(urllib.parse.urljoin(args.url, x) for x in p.links))
        target.with_suffix('.links.json').write_text(json.dumps(links, ensure_ascii=False, indent=2)+'\n')
    records = [x for x in records if x['path'] != record['path']] + [record]
    MANIFEST.parent.mkdir(parents=True, exist_ok=True)
    MANIFEST.write_text(json.dumps(sorted(records, key=lambda x:x['path']), ensure_ascii=False, indent=2)+'\n')
    print(record['path'], record['bytes'], record['sha256'])


if __name__ == '__main__':
    main()
