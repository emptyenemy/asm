"""Copy README.md into a release archive with links that still work there.

The repository README uses relative paths, which GitHub resolves. Unpacked
from an archive, the images and the links to other files have nothing to
point at, so this rewrites them to the release commit on GitHub: images to
raw.githubusercontent.com, documents to github.com. In-page anchors and
absolute URLs are left alone.

    python archive_readme.py SOURCE TARGET OWNER/REPO COMMIT
"""
import re
import sys

source, target, repository, commit = sys.argv[1:5]
raw = f'https://raw.githubusercontent.com/{repository}/{commit}/'
blob = f'https://github.com/{repository}/blob/{commit}/'
local = r'(?![a-z][a-z0-9+.-]*:|#|/)'

with open(source, encoding='utf-8') as file:
    text = file.read()
text = re.sub(r'\b(src|srcset)="' + local + r'([^"]+)"', lambda m: f'{m[1]}="{raw}{m[2]}"', text)
text = re.sub(r'\bhref="' + local + r'([^"]+)"', lambda m: f'href="{blob}{m[1]}"', text)
text = re.sub(r'\]\(' + local + r'([^)\s]+)\)', lambda m: f']({blob}{m[1]})', text)
with open(target, 'w', encoding='utf-8', newline='\n') as file:
    file.write(text)
