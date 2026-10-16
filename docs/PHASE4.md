# Phase 4: database events, room shards, failover and monitoring

Implementation is present. Offline unit/race checks can run without provider credentials. PostgreSQL/Redis/network integration and capacity acceptance must also pass in a socket-enabled environment before calling a real-event deployment validated. No live Sarvam request is required for any check below.

## Database source of truth

With `DATABASE_URL`, startup creates the event and transcript tables in PostgreSQL alongside the existing vocabulary bank. On the first boot only, `rooms.yaml` initializes the catalog. Later boots load the database even if the file has been removed. All fields, including ASR mode, prompt, always-translate languages and glossary, survive this round trip.

To replace the catalog explicitly:

```sh
go run ./cmd/events -import rooms.yaml
```

Set `DATABASE_URL` in the environment first. The command validates before an atomic replacement. Restart room owners after changing definitions. Without `DATABASE_URL`, the existing YAML catalog and local JSONL transcripts still work.

Existing JSONL transcripts are **not** automatically imported into PostgreSQL. Back them up and export them before changing storage backends. Database backups must include the event, transcript and vocabulary tables together. Switching back to file mode accesses the old local files, not a database export.

## Multiple servers

Set these on every node:

| Variable | Meaning |
| --- | --- |
| `DATABASE_URL` | Same PostgreSQL database on all nodes |
| `REDIS_URL` | Shared Redis (`redis://` or TLS `rediss://`, optional ACL credentials/database) |
| `NODE_URL` | Stable internal HTTP(S) origin reachable by other nodes |
| `ROOM_IDS` | Comma-separated room IDs owned by this node |
| `CLUSTER_NAMESPACE` | Redis lease prefix; default `dubash`, unique per deployment |

Every room is assigned to exactly one process. Nodes take token-protected, 30-second Redis leases before loading transcript history. Leases renew every five seconds with a four-second overall heartbeat deadline. A failed renewal immediately marks the node unavailable and cancels its pipelines. A second process cannot start with an overlapping shard. Keep Redis on a reliable network and do not configure eviction of ownership keys.

Requests for another node's room are reverse-proxied to its registered owner. This includes microphone WebSockets, SSE, exports, talk controls, corrections and event context. SSE IDs, language selection and `Last-Event-ID` pass through. The room list and admin status aggregate across owners. Metrics stay per node: scrape **every node directly**, not the load balancer. `/healthz` reports process liveness; `/readyz` checks ownership availability and the transcript database.

`deploy/compose.phase4.yml` provides two room owners (main-hall / hall-b), PostgreSQL, persistent Redis, nginx, Prometheus and provisioned Grafana. From the repository root:

```sh
export ADMIN_TOKEN='a-long-event-admin-token'
export POSTGRES_PASSWORD='a-url-safe-database-password'
export GRAFANA_PASSWORD='a-separate-dashboard-password'
docker compose -f deploy/compose.phase4.yml up --build
```

This stack defaults to `-fake`. It cannot call Sarvam. The public listener is port 8080; Grafana is available on localhost:3000 and Prometheus on localhost:9090. Grafana's username is `admin`. Internal service names must resolve and be reachable across nodes. The supplied network is a local Docker network; for multiple physical machines, use routable node URLs and shared database/Redis services. Put venue TLS at the public ingress.

nginx forwards WebSocket Upgrade/Connection headers, disables SSE buffering and allows long connections. See the [official nginx WebSocket documentation](https://nginx.org/en/docs/http/websocket.html).

Scaling is **by room**, not by copying one room's in-memory hub across processes. One room's viewers are still limited by its owner. On process failure, other rooms keep running; restart the failed shard and let its lease expire before starting a replacement. Reconnection restores durable caption history. Exact message-ID replay is retained on the same live owner; after restart the existing client deduplicates the seeded history by segment ID. There is no automatic room migration, cross-process exact SSE cursor replay, or zero-interruption microphone takeover. Adding/removing rooms also requires updating the shard assignments; do not leave an event without an owner.

## Independent provider failover

Live deployments can configure:

| Variable | Backup |
| --- | --- |
| `ASR_BACKUP_URL` | Independent Whisper-compatible `/v1` base URL |
| `ASR_BACKUP_MODEL`, `ASR_BACKUP_API_KEY` | Optional backup speech model/token |
| `TRANSLATE_BACKUP_URL` | Independent chat-completions `/v1` base URL |
| `TRANSLATE_BACKUP_MODEL`, `TRANSLATE_BACKUP_API_KEY` | Optional backup translation model/token |

These backups are opt-in; normal single-provider deployments remain supported. Both can point to local model servers and must support the event languages. They must honor request cancellation. Configure and rehearse them before a real event: the code cannot create or certify a backup model service.

Translation gives the primary four seconds, then spends the remaining outer caption deadline on the backup. Three failed primary requests open a 30-second circuit; requests use the backup during that interval, followed by one half-open primary probe. Empty results count as failures. Caller cancellation does not start a backup request. The existing primary Sarvam chat-to-translate fallback is independent of this provider-outage mechanism. The failover wrapper translates lines individually instead of batching, to isolate deadlines and errors.

Speech switches once per transmission after a fatal exit, three consecutive recoverable errors, a stalled recognizer, or a stalled audio consumer. The backup receives only uncommitted PCM, capped at 15 seconds, and timestamps are offset onto the original audio clock. Older uncommitted audio loss is reported explicitly. A paused/idle microphone does not trigger the progress deadline. Backup failure ends the transmission with an error; a new transmission tries the primary again. The progress watchdog is a 20-second wall-clock deadline while audio is arriving; unusually long silence or an utterance without any interim text may also trip it. ASR vocabulary warnings are reported through the existing generic error channel and contribute to the consecutive-error threshold.

To opt the Docker stack into live providers, add `-f deploy/compose.live.yml`. This overlay removes `-fake` and requires both independent backup URLs. **It can incur provider charges; do not use it for offline testing.** No paid provider calls were made while implementing Phase 4.

## Load and failure tests

```sh
# Clears paid keys and probe flags; starts a temporary -fake server, then cleans up.
VIEWERS=200 DURATION=15s ./deploy/loadtest.sh

# Real PostgreSQL and Redis, fake/in-process provider failures only.
TEST_DATABASE_URL='postgres://user:password@localhost/test?sslmode=disable' \
TEST_REDIS_URL='redis://localhost:6379/0' \
SARVAM_API_KEY= SARVAM_PROBE= GEMINI_API_KEY= go test -race ./...
```

The load tool exercises concurrent real SSE connections plus a paced microphone WebSocket, requiring every viewer to get a source caption and translated caption. It emits JSON with connection count, caption/translation counts, failures and time to first caption p95. Change viewer count and duration to the expected venue peak; the default 200 is a reproducible smoke test, not a promised capacity. First-caption latency includes the fake recognizer's three seconds of audio and client connection setup. `cmd/loadtest` targets an existing server and does not verify its provider mode; use `deploy/loadtest.sh` for the guaranteed offline path.

Failure tests cover primary timeout/circuit recovery, both translation providers down, cancellation without a charged fallback, ASR replay boundaries and clock offsets, recoverable speech failures, RESP parsing, routing, duplicate Redis ownership, cross-node SSE/resume headers, lease-loss fencing, database catalog imports and transcript restart/revision protection. Integration tests use isolated PostgreSQL schemas and unique Redis namespaces. `.github/workflows/phase4.yml` supplies the services, runs the race suite and HTTP load smoke test, runs `deploy/test-cluster.sh` against two real fake-mode app processes (cross-node WebSocket/SSE load and durable owner restart), and validates Compose and Prometheus rules.

## Dashboards and alerts

Grafana automatically loads the Dubash live-event dashboard and Prometheus source. Panels include node availability, viewers, caption throughput, translation/commit latency, pipeline errors, dropped lines, provider failovers, audio level/age and estimated cost. New process-local counters are:

- `dubash_asr_failovers_total`
- `dubash_translation_fallbacks_total`
- `dubash_translation_failures_total`

Prometheus rules flag node loss, repeated pipeline errors, delayed translations, missing live audio, backup activation and dropped captions. Rules are evaluated and visible in Prometheus; outbound notifications require a separately configured Alertmanager and receiver. Backup-provider charges are not included in the existing Sarvam-based cost estimate. Monitor provider billing separately when failover occurs.

## Boundary with Phase 5

This phase does not add user accounts, change admin authentication, implement retention, or make a security-review claim. The other agent's Phase 5 changes must also cover PostgreSQL transcripts (not just JSONL files), authenticated `/metrics` scraping, cross-node control requests and transcript exports. Never expose `X-Dubash-Catalog` as an authorization mechanism: it only chooses local versus aggregated lists; the normal endpoint authorization remains in force.


## Local verification on 2026-10-16

- Every Go package compiled; `go vet ./...` passed.
- Provider failover, cancellation, fake-mode safety, RESP and room routing tests passed ten race-detector runs. The 1,000-reader isolation/replay test also passed ten race-detector runs.
- The existing bank, config, correction, export, language, room, script, file-store and ingest-command tests passed with the race detector. PostgreSQL tests skip unless their dedicated test URL is set.
- Both Compose configurations passed `docker compose config --quiet`; both shell scripts passed `bash -n`; formatting and whitespace checks passed.
- A process-local hub benchmark delivered/drained one message to 100 / 1,000 / 5,000 subscribers in approximately 18 / 78 / 342 microseconds respectively on an Apple M1. These figures exclude HTTP, WebSockets, provider latency and network costs and are not venue capacity measurements.
- The complete HTTP suite could not run locally because this execution sandbox rejects socket binding (`bind: operation not permitted`). Docker's daemon is also unavailable. The real Redis/PostgreSQL, two-process HTTP load/restart and Prometheus checks are provided in CI but were not executed here.
- No real Sarvam or Gemini API requests were made.
