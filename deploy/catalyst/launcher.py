"""Run the existing static Go app in the AppSail Linux managed environment.

With SARVAM_API_KEY set in the AppSail environment it runs live (real
Sarvam recognition and translation); without it, scripted demo captions.
The package itself never contains a key.
AppSail scratch files are ephemeral; durable data needs a shared database before
this package is used for persistent events.

When the app cannot start, the launcher itself answers on the port with the
reason (never a secret), so the problem shows at the app's URL even without
AppSail logs.
"""
import http.server
import os
from pathlib import Path
import shutil
import secrets
import signal
import stat
import subprocess
import sys
import time

base = Path(__file__).resolve().parent
port = int(os.environ.get("X_ZOHO_CATALYST_LISTEN_PORT") or os.environ.get("PORT") or "9000")


def log(msg):
    print("dubash launcher: " + msg, file=sys.stderr, flush=True)


def explain(reason):
    """Serve the reason the app did not start, until AppSail stops us."""
    log(reason)
    body = ("Dubash did not start.\n\n" + reason + "\n").encode()

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(503)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        do_POST = do_HEAD = do_GET

        def log_message(self, *args):
            pass

    http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()


def prepare():
    token = os.environ.get("ADMIN_TOKEN", "")
    if not token:
        # Nobody has to type a secret: organisers sign in with Google, and
        # this run gets a strong random key no one ever sees. Sessions and
        # speaker links last until the app restarts.
        os.environ["ADMIN_TOKEN"] = secrets.token_urlsafe(32)
        log("ADMIN_TOKEN not set: generated a private one for this run" +
            ("; organisers sign in with Google" if os.environ.get("GOOGLE_CLIENT_ID") else
             " (set GOOGLE_CLIENT_ID and ORGANISER_EMAILS so organisers can sign in)"))
    elif len(token) < 24:
        return None, ("ADMIN_TOKEN is only %d characters long. Use at least 24 characters, "
                      "or remove it to sign in with Google only." % len(token))
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
    os.environ["DUBASH_TEMPORARY_DATA"] = "on"
    # Live with a Sarvam key (real recognition and translation, billed to
    # that key); scripted demo captions without one, or with DUBASH_DEMO=on.
    live = bool(os.environ.get("SARVAM_API_KEY")) and os.environ.get("DUBASH_DEMO", "") != "on"
    if live:
        os.environ.setdefault("ASR_PROVIDER", "sarvam")
        log("live mode: Sarvam recognition and translation")
    else:
        os.environ["CORRECTION"] = "rules"
        log("demo mode: scripted captions, no paid providers (set SARVAM_API_KEY to go live)")
    # One server, no shared database here: drop multi-server settings, and
    # in demo mode every provider key, so nothing paid is ever called.
    drop = ["SARVAM_PROBE", "DATABASE_URL", "REDIS_URL", "NODE_URL", "ROOM_IDS"]
    if not live:
        drop += ["SARVAM_API_KEY", "GEMINI_API_KEY"]
    for name in drop:
        os.environ.pop(name, None)
    binary = base / "dubash"
    if not binary.exists():
        return None, "The dubash program is missing from the package (looked in %s)." % base
    if not os.access(binary, os.X_OK):
        try:
            binary.chmod(0o755)
        except OSError:
            copy = Path("/tmp/dubash-bin/dubash")  # not ours to change: run a copy
            copy.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(binary, copy)
            copy.chmod(stat.S_IRWXU)
            binary = copy
    args = [str(binary)] + ([] if live else ["-fake"])
    return args + ["-addr", "0.0.0.0:%d" % port, "-config", str(base / "rooms.yaml"), "-data", str(data)], None


def main():
    try:
        cmd, problem = prepare()
    except Exception as e:  # anything unexpected: say what
        cmd, problem = None, "The launcher failed while preparing: %s: %s" % (type(e).__name__, e)
    if problem:
        explain(problem)
        return
    os.chdir(base)  # the samples folder is served from here
    log("starting on 0.0.0.0:%d" % port)
    errors = Path("/tmp/dubash-stderr.log")
    with open(errors, "wb") as err:
        try:
            child = subprocess.Popen(cmd, stderr=err)
        except OSError as e:
            explain("Could not run %s: %s (the runtime may not be Linux x86-64)." % (cmd[0], e))
            return
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, lambda s, _f: child.send_signal(s))
    code = child.wait()
    tail = errors.read_text(errors="replace")[-3000:]
    sys.stderr.write(tail)
    if code in (0, -signal.SIGTERM, -signal.SIGINT):
        return
    explain("The server exited with code %s. Its last messages:\n\n%s" % (code, tail))


main()
