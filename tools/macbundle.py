# Monta "RACK MONITOR BY DON.app" (binário universal Intel + Apple Silicon) e o zip.
import struct, os, sys, io, zipfile, shutil, time
from PIL import Image, ImageDraw

amd64, arm64, outdir, version, logo = sys.argv[1:6]

def has_codesig(path):
    d = open(path, 'rb').read()
    magic = struct.unpack('<I', d[:4])[0]
    assert magic == 0xfeedfacf, hex(magic)
    ncmds = struct.unpack('<I', d[16:20])[0]
    off = 32
    for _ in range(ncmds):
        cmd, size = struct.unpack('<II', d[off:off+8])
        if cmd == 0x1d: return True  # LC_CODE_SIGNATURE
        off += size
    return False

def fat(paths_arch, out):
    datas = [open(p, 'rb').read() for p, _ in paths_arch]
    align = 1 << 14
    offs, cur = [], align
    for d in datas:
        offs.append(cur); cur = (cur + len(d) + align - 1) // align * align
    hdr = struct.pack('>II', 0xCAFEBABE, len(datas))
    for (p, (ct, cs)), d, o in zip(paths_arch, datas, offs):
        hdr += struct.pack('>IIIII', ct, cs, o, len(d), 14)
    buf = bytearray(cur)
    buf[:len(hdr)] = hdr
    for d, o in zip(datas, offs): buf[o:o+len(d)] = d
    open(out, 'wb').write(bytes(buf))

def icns(out):
    src = Image.open(logo).convert('RGBA')
    def img(n):
        bg = Image.new('RGBA', (n, n), (0, 0, 0, 0))
        dr = ImageDraw.Draw(bg)
        m = int(n * .09); r = int(n * .2)
        im = src.copy(); im.thumbnail((n, n), Image.LANCZOS)
        bg.paste(im, ((n - im.width) // 2, (n - im.height) // 2), im)
        b = io.BytesIO(); bg.save(b, 'PNG'); return b.getvalue()
    ents = b''
    for t, n in [(b'icp4', 16), (b'icp5', 32), (b'ic11', 32), (b'ic12', 64), (b'ic07', 128), (b'ic13', 256), (b'ic08', 256), (b'ic14', 512), (b'ic09', 512), (b'ic10', 1024)]:
        d = img(n); ents += t + struct.pack('>I', 8 + len(d)) + d
    open(out, 'wb').write(b'icns' + struct.pack('>I', 8 + len(ents)) + ents)

for p in (amd64, arm64): print(os.path.basename(p), 'assinatura ad-hoc:', has_codesig(p))

def plist(name, ident, env):
    envxml = '<key>LSEnvironment</key><dict><key>RACKMON_DEMO</key><string>1</string></dict>' if env else ''
    return f'''<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>{name}</string>
<key>CFBundleDisplayName</key><string>{name}</string>
<key>CFBundleIdentifier</key><string>{ident}</string>
<key>CFBundleVersion</key><string>{version}</string>
<key>CFBundleShortVersionString</key><string>{version}</string>
<key>CFBundleExecutable</key><string>RackMonitorByDon</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSMinimumSystemVersion</key><string>11.0</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
<key>NSHumanReadableCopyright</key><string>BY DON</string>
{envxml}
</dict></plist>
'''

tmp = os.path.join(outdir, '_mac'); shutil.rmtree(tmp, ignore_errors=True); os.makedirs(tmp)
uni = os.path.join(tmp, 'RackMonitorByDon')
fat([(amd64, (0x01000007, 3)), (arm64, (0x0100000C, 0))], uni)
ic = os.path.join(tmp, 'AppIcon.icns'); icns(ic)
zpath = os.path.join(outdir, f'RACK_MONITOR_BY_DON_V{version}_Mac.zip')
z = zipfile.ZipFile(zpath, 'w', zipfile.ZIP_DEFLATED)
def add(arc, data, mode):
    zi = zipfile.ZipInfo(arc, time.localtime()[:6]); zi.create_system = 3
    zi.external_attr = (mode & 0xFFFF) << 16; zi.compress_type = zipfile.ZIP_DEFLATED
    z.writestr(zi, data)
def adddir(arc):
    zi = zipfile.ZipInfo(arc.rstrip('/') + '/', time.localtime()[:6]); zi.create_system = 3
    zi.external_attr = (0o40755 << 16) | 0x10; z.writestr(zi, b'')
for app, ident, env in [('RACK MONITOR BY DON', 'com.bydon.rackmonitor', False), ('RACK MONITOR (demonstração)', 'com.bydon.rackmonitor.demo', True)]:
    root = f'RACK MONITOR BY DON V{version}/{app}.app/Contents'
    for d in [root, root + '/MacOS', root + '/Resources']: adddir(d)
    add(root + '/Info.plist', plist(app, ident, env).encode(), 0o100644)
    add(root + '/PkgInfo', b'APPL????', 0o100644)
    add(root + '/MacOS/RackMonitorByDon', open(uni, 'rb').read(), 0o100755)
    add(root + '/Resources/AppIcon.icns', open(ic, 'rb').read(), 0o100644)
add(f'RACK MONITOR BY DON V{version}/LEIA-ME MAC.txt', open(os.path.join(outdir, 'LEIA-ME MAC.txt'), 'rb').read(), 0o100644)
z.close()
shutil.copyfile(uni, os.path.join(outdir, 'RackMonitorByDon_mac')); os.chmod(os.path.join(outdir, 'RackMonitorByDon_mac'), 0o755)
shutil.rmtree(tmp)
print('zip', zpath, os.path.getsize(zpath))
