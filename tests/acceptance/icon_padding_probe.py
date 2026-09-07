"""Check generated icon dimensions and visible bounds without changing images."""
import argparse,json,re
from pathlib import Path
from PIL import Image
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('iconset',type=Path)
parser.add_argument('--before',type=Path,required=True)
parser.add_argument('--report',type=Path,required=True)
args=parser.parse_args()
checks=[]
for p in sorted(args.iconset.glob('*.png')):
 m=re.fullmatch(r'icon_(\d+)x\d+(@2x)?\.png',p.name);assert m
 expected=int(m[1])*(2 if m[2] else 1)
 im=Image.open(p).convert('RGBA');assert im.size==(expected,expected)
 bounds=im.getchannel('A').point(lambda a:255 if a>=128 else 0).getbbox();assert bounds
 checks.append({'file':p.name,'pixels':expected,'bounds':list(bounds)})
assert len(checks)==10
before=Image.open(args.before).convert('RGBA');b=before.getchannel('A').point(lambda a:255 if a>=128 else 0).getbbox()
after=Image.open(args.iconset/'icon_512x512@2x.png').convert('RGBA');a=after.getchannel('A').point(lambda a:255 if a>=128 else 0).getbbox()
ratio=((a[2]-a[0])/after.width)/((b[2]-b[0])/before.width)
assert .845<ratio<.855 and .79<(a[2]-a[0])/after.width<.82
report={'passed':True,'sizeRatio':ratio,'before':{'imageSize':list(before.size),'bounds':list(b)},'after':{'imageSize':list(after.size),'bounds':list(a)},'representations':checks}
args.report.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
