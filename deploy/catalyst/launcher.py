"""Run the existing static Go app in the AppSail Linux managed environment.

This package is a temporary, fake-provider Development test. It has no API keys.
AppSail scratch files are ephemeral; durable data needs a shared database before
this package is used for persistent events.

Every reason it cannot start is printed to the AppSail logs (DevOps → Logs).
"""
import os
from pathlib import Path
import shutil
import stat
import sys


def log(msg):
    print("dubash launcher: " + msg, file=sys.stderr, flush=True)


def fail(msg):
    log(msg)
    raise SystemExit(1)


base = Path(__file__).resolve().parent
port = os.environ.get("X_ZOHO_CATALYST_LISTEN_PORT") or os.environ.get("PORT") or "9000"
token = os.environ.get("ADMIN_TOKEN", "")
if len(token) < 24:
    fail("ADMIN_TOKEN is %s: set it to an organiser access key of at least 24 characters "
         "in AppSail → Configuration → Environment Variables, then redeploy."
         % ("missing" if not token else "only %d characters" % len(token)))

# /tmp is the only writable place on AppSail; the package folder may be
# read-only or owned by another user.
data = Path("/tmp/dubash-data")
data.mkdir(mode=0o700, parents=True, exist_ok=True)
created = data / "events.yaml"
if not created.exists() and (base / "events.yaml").exists():
    try:
        shutil.copyfile(base / "events.yaml", created)
        created.chmod(0o600)
    except OSError as e:
        log("could not copy events.yaml (%s): starting with rooms.yaml only" % e)

os.environ["TRUST_PROXY"] = "on"
os.environ["CORRECTION"] = "rules"
os.environ["DUBASH_TEMPORARY_DATA"] = "on"
# This Development artifact cannot contact paid speech providers, even if a key
# was inadvertently configured in the service environment.
for name in ("SARVAM_API_KEY", "SARVAM_PROBE", "GEMINI_API_KEY", "DATABASE_URL", "REDIS_URL", "NODE_URL", "ROOM_IDS"):
    os.environ.pop(name, None)

binary = base / "dubash"
if not binary.exists():
    fail("the dubash binary is missing from the package (%s)" % base)
if not os.access(binary, os.X_OK):
    try:
        binary.chmod(0o755)
    except OSError:
        # Not ours to change: run a copy from /tmp instead.
        copy = Path("/tmp/dubash-bin/dubash")
        copy.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(binary, copy)
        copy.chmod(stat.S_IRWXU)
        binary = copy

os.chdir(base)  # the samples folder is served from here
log("starting on 0.0.0.0:%s" % port)
try:
    os.execv(str(binary), [str(binary), "-fake", "-addr", "0.0.0.0:" + port,
                           "-config", str(base / "rooms.yaml"), "-data", str(data)])
except OSError as e:
    fail("could not run %s: %s" % (binary, e))
