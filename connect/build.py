"""Build the private connection extension using only the standard library."""
from pathlib import Path
import zipfile
import argparse
from urllib.parse import urlsplit

parser = argparse.ArgumentParser()
parser.add_argument('--origin', default='http://127.0.0.1:18788')
args = parser.parse_args()
origin = args.origin.rstrip('/')
url = urlsplit(origin)
if (url.scheme != 'https' and not (url.scheme == 'http' and url.hostname in ('localhost', '127.0.0.1', '::1'))) or not url.hostname or url.username or url.password or url.path or url.query or url.fragment:
    parser.error('Use an HTTPS origin or loopback HTTP origin')
url.port  # Reject invalid ports before writing anything.

root = Path(__file__).resolve().parent.parent
out = root / 'web/public/connect'
out.mkdir(exist_ok=True)
with zipfile.ZipFile(out / 'Music-Connect.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
    for source in sorted((root / 'connect/chrome').iterdir()):
        if source.is_file():
            archive.writestr('Music-Connect/' + source.name, source.read_text().replace('http://127.0.0.1:18788', origin))
    archive.write(root / 'web/public/brand/music-192.png', 'Music-Connect/icon.png')
print('Built web/public/connect/Music-Connect.zip')
