"""Fetch only the pinned, checksummed Microsoft SDK; never run package scripts."""
import hashlib
import json
import urllib.request
import zipfile
from pathlib import Path

root = Path(__file__).resolve().parent
package = json.loads((root / 'packages.lock.json').read_text())['Microsoft.Web.WebView2']
vendor = root / 'vendor'
vendor.mkdir(exist_ok=True)
archive = vendor / ('microsoft.web.webview2.' + package['version'] + '.nupkg')
if not archive.exists():
    with urllib.request.urlopen(package['url'], timeout=45) as response:
        content = response.read(32 << 20)
    assert hashlib.sha256(content).hexdigest() == package['sha256'], 'SDK checksum mismatch'
    archive.write_bytes(content)
assert hashlib.sha256(archive.read_bytes()).hexdigest() == package['sha256'], 'SDK checksum mismatch'
folder = vendor / ('webview2-' + package['version'])
allowed = {
    'lib/net462/Microsoft.Web.WebView2.Core.dll',
    'lib/net462/Microsoft.Web.WebView2.WinForms.dll',
    'build/native/x64/WebView2Loader.dll',
    'LICENSE.txt',
}
with zipfile.ZipFile(archive) as contents:
    assert allowed <= set(contents.namelist())
    for name in allowed:
        path = folder / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(contents.read(name))
print(json.dumps({'sdk': package['version'], 'checksumPassed': True, 'extractedFiles': len(allowed)}))
