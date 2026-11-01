"""Zip a directory with forward-slash entry names.

Compress-Archive on Windows writes backslash separators, which violate the ZIP
spec: unzip, 7-Zip and Python then see one flat filename instead of a tree.
Usage: zipdir.py <source-dir> <output-zip>
"""
import os
from pathlib import Path
import sys
import zipfile

source = Path(sys.argv[1]).resolve()
output = Path(sys.argv[2]).resolve()
output.parent.mkdir(parents=True, exist_ok=True)

# Fixed timestamp so the same inputs always produce the same archive.
stamp = (1980, 1, 1, 0, 0, 0)
with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
    for directory, subdirectories, files in os.walk(source):
        subdirectories.sort()
        base = Path(directory)
        for name in sorted(files):
            path = base / name
            entry = path.relative_to(source).as_posix()
            info = zipfile.ZipInfo(entry, date_time=stamp)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o644 << 16
            archive.writestr(info, path.read_bytes())
print(f"Packaged {output}")
