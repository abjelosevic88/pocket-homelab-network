#!/usr/bin/env python3
"""Build internal/netscan/oui.txt.gz (MAC prefix -> vendor) from Wireshark's manuf file.

    curl -sSLo /tmp/manuf https://www.wireshark.org/download/automated/data/manuf
    python3 scripts/oui.py /tmp/manuf internal/netscan/oui.txt.gz

Each line: hex prefix (6, 7 or 9 digits for /24, /28, /36) TAB vendor.
"""
import gzip, re, sys

SUFFIX = re.compile(r"[,\s]+(co\.?,?\s*ltd\.?|co\.?|ltd\.?|limited|inc\.?|incorporated|corp\.?|corporation|gmbh|llc|l\.l\.c\.|s\.?a\.?|ag|b\.v\.|bv|oy|ab|a/s|as|plc|pty|pte|srl|s\.r\.l\.|sas|kg|kk|k\.k\.|company|technologies co|electronics co)\.?$", re.I)

def clean(name):
    name = name.strip().strip('"')
    for _ in range(4):
        new = SUFFIX.sub("", name).strip(" ,.")
        if new == name:
            break
        name = new
    return name

out = []
for line in open(sys.argv[1], encoding="utf-8", errors="replace"):
    if not line.strip() or line.startswith("#"):
        continue
    parts = line.rstrip("\n").split("\t")
    if len(parts) < 2:
        continue
    prefix = parts[0].strip()
    short = parts[1].strip()
    long_ = parts[2].strip() if len(parts) > 2 else short
    bits = 24
    if "/" in prefix:
        prefix, b = prefix.split("/")
        bits = int(b)
    hexp = prefix.replace(":", "").replace("-", "").upper()
    if bits not in (24, 28, 36):
        continue
    hexp = hexp[: bits // 4]
    name = clean(long_) or short
    out.append(f"{hexp}\t{name}")

with gzip.open(sys.argv[2], "wt", encoding="utf-8", compresslevel=9) as f:
    f.write("\n".join(out) + "\n")
print(len(out), "prefixes")
