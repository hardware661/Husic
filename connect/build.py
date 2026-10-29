"""Build the Husic Connect Chrome extension for one Husic origin (standard library only)."""
from pathlib import Path
import zipfile
import argparse
from urllib.parse import urlsplit

parser = argparse.ArgumentParser()
parser.add_argument('--origin', default='http://127.0.0.1:18788')
parser.add_argument('--keep', action='store_true', help='do nothing if a package was already built (used by npm run build)')
args = parser.parse_args()
origin = args.origin.rstrip('/')
url = urlsplit(origin)
if (url.scheme != 'https' and not (url.scheme == 'http' and url.hostname in ('localhost', '127.0.0.1', '::1'))) or not url.hostname or url.username or url.password or url.path or url.query or url.fragment:
    parser.error('Use an HTTPS origin or loopback HTTP origin')
url.port  # Reject invalid ports before writing anything.

root = Path(__file__).resolve().parent.parent
out = root / 'web/public/connect'
out.mkdir(exist_ok=True)
if args.keep and (out / 'Husic-Connect.zip').exists():
    raise SystemExit(0)
with zipfile.ZipFile(out / 'Husic-Connect.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
    for source in sorted((root / 'connect/chrome').iterdir()):
        if source.is_file():
            archive.writestr('Husic-Connect/' + source.name, source.read_text(encoding='utf-8').replace('http://127.0.0.1:18788', origin))
    archive.write(root / 'connect/README.md', 'Husic-Connect/README.md')
    archive.write(root / 'web/public/brand/husic-192.png', 'Husic-Connect/icon.png')
print('Built web/public/connect/Husic-Connect.zip')
