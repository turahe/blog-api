# Realtime Notifications (SSE)

## Overview

The backend exposes an authenticated, user-scoped Server-Sent Events (SSE) endpoint at
`GET /api/v1/me/notifications/stream` that delivers realtime notification events plus
stream lifecycle controls to browser and native EventSource-compatible clients. This
document covers endpoint usage, wire format, connection lifecycle, security posture,
client integration, testing, and scaling guidance.

**Canonical contracts**:

- OpenAPI REST: [paths/notifications.yaml](../../paths/notifications.yaml#L41-L153) (SSE path definition)
- AsyncAPI stream binding: [contracts/asyncapi.yaml](../../contracts/asyncapi.yaml) channel `notifications.user.{user_id}.stream`
- HTTP behaviour rules: [docs/backend/api.md](../backend/api.md#L73-L88)
- Streaming event catalogue: [docs/backend/events.md](../backend/events.md#L103-L155)

## Implementation status

What the server does today (`handlers/notifications_stream.go`, `adapters/inbound/realtime`,
`adapters/outbound/notificationbus`). The rest of this chapter is the target design.

| Topic | Implemented |
| --- | --- |
| Auth | Bearer token only (`AuthRequired`); no cookie or CSRF path |
| Handshake | `retry: 5000`, then `event: stream.opened` with `stream_id`, `user_id`, `server_ts`, `retry_ms`, `channels`, `replay_applied: false`, `replay_count: 0` |
| Events | `notification.created` (`id:` is the notification id; `data` has `id`, `type`, `title`, `body`, `preview`, `data`, `actor_id`, `created_at`), `ping` every `SSE_PING_INTERVAL` (default `15s`), `error` with `fanout.buffer_full` and `dropped_count`, `stream.closed` with `code: shutdown` and `retry_ms: 15000` |
| Limits | `SSE_MAX_CONCURRENT_PER_USER` (default 3) per API process; the next stream gets `429 notifications.stream_limit` with `Retry-After: 60` |
| Slow clients | 512 queued events per stream; further events are dropped for that stream only and reported in the next `error` frame |
| Cleanup | The stream ends when the client disconnects (request context done) or a write fails; `app serve` shutdown sends `stream.closed` to every stream first |
| Fan-out | Requires `MESSAGE_BROKER`. Each process publishes to `notifications.created` and reads it back through its own subscription (Kafka without a consumer group, a per-process auto-delete RabbitMQ queue, or a per-process Pub/Sub subscription that expires after a day unused). Without a broker the stream returns `503 notifications.stream_unavailable` |
| Not implemented | `Last-Event-ID` / `replay_after` replay, the Redis replay ring, the half-open socket watchdog, `notification.read` / `notification.dismissed` / `session.*` events, Redis-backed connection limits and jail, Prometheus counters for dropped events |

After reconnecting, clients should call `GET /api/v1/me/notifications` to fill any gap.

### Proxy notes

- Nginx: the response sets `X-Accel-Buffering: no`; also keep `proxy_http_version 1.1`, an empty
  `Connection` header, and `proxy_read_timeout` well above `SSE_PING_INTERVAL`.
- Load balancers: idle timeouts must exceed the ping interval (AWS ALB defaults to 60s).
- Do not enable response compression on this path; it buffers frames.
- The HTTP server has no write timeout, so streams are not cut; request metrics record a
  stream's full duration.

## 1. Endpoint Usage

| Property | Value |
|----------|-------|
| Method + Path | `GET /api/v1/me/notifications/stream` |
| Transport | HTTP/1.1 or HTTP/2 (recommended for large concurrency) |
| Content-Type | `text/event-stream; charset=utf-8` (always) |
| Content-Encoding | identity (never gzip; let HTTP/2 handle per-stream compression) |
| Cache-Control | `no-cache` (always) |
| Auth | JWT bearer token in `Authorization: Bearer <jwt>` header, or valid http-only session cookie issued by `/api/v1/auth/login` |
| CSRF | browser callers with session cookies must send `X-CSRF-Token` header if CSRF is enabled for the origin; non-browser API tokens in the allowlist are exempt |
| Idle keep-alive | server emits `event: ping` every `SSE_PING_INTERVAL_SECONDS` (default `15`) |
| Reconnect hint | server emits `retry: 5000` (ms) immediately after the initial handshake; EventSource automatically honours it |
| Per-user concurrency | default `SSE_MAX_CONCURRENT_PER_USER=3`; 4th connection returns 429 with `Retry-After` |
| Replay ring | bounded `sse_replay:user:<id>` Redis ZSET keyed by monotonic event id, TTL `SSE_REPLAY_TTL_SECONDS` (default `600`), max `SSE_REPLAY_MAX_EVENTS_PER_USER` (default `1000`) |
| Fan-out buffer | per-connection Go channel of size `SSE_PER_CONN_BUFFER` (default `512`); overflow drops the oldest buffered event, increments a stat, and emits a single `event: error` control frame |

### Query Parameters

| Name | Kind | Required | Description |
|------|------|----------|-------------|
| `replay_after` | query string (uuid or opaque id string) | no | Equivalent to `Last-Event-ID` header; useful for embedded WebViews that cannot set custom headers. If both are provided, the header wins. |
| `channels` | comma-separated enum string in `[default, moderation, marketing]` | no | Opt-in filter for fine-grained notification channels; defaults to `default`. Server-side filtered at fan-out, never client-side. |

### Success Response (200 OK)

The 200 response has no JSON body; instead the server immediately writes:

```
retry: 5000

event: stream.opened
id: 0000000000000000001-0001
data: {"stream_id":"8aa9b3f4-…","user_id":"9c7a48d2-…","server_ts":"2026-07-29T09:12:01Z","retry_ms":5000,"channels":["default"]}

event: ping
id: 0000000000000000001-0002
data: {"ts":"2026-07-29T09:12:16Z"}

event: notification.created
id: 0000000000000000001-0003
data: {"notification_id":"c6c3e1e7-…","type":"comment.reply","title":"Reply to your comment","preview":"Someone just replied to …","read":false,"created_at":"2026-07-29T09:12:33Z","channel":"sse","channels":["default"]}
```

## 2. Supported Event Formats

All frames follow the WHATWG Server-Sent Events specification. Each logical event is
composed of one or more `field: value\n` lines terminated by an empty newline.

| Field | Presence | Purpose |
|-------|----------|---------|
| `event: <name>` | required on every frame except pure keep-alive comment lines | Names the event. Clients listen via `source.addEventListener(name, handler)`. |
| `data: <json>` | required on every named frame | Single-line compact JSON. Multiline payloads are never used so a single `data:` line always suffices. |
| `id: <opaque>` | always present on `notification.*` / `session.*` / `stream.opened` / `error` events; omitted from `ping` to save bytes | Monotonically increasing string id of the form `<unix-nano>-<nodeid>` suitable for `Last-Event-ID` replay. |
| `retry: <ms>` | sent exactly once in the initial handshake | Tells EventSource what reconnect delay to use after a network drop. |
| `: <comment>` | sent when intermediate proxies are suspected to be aggressive; equivalent to `ping` but never surfaces as a DOM event | Keep-alive bytes on the wire. |

### Named Events

| Event name | `id:` present | `data:` shape |
|------------|---------------|---------------|
| `stream.opened` | yes | `{ stream_id: uuid, user_id: uuid, server_ts: datetime, retry_ms: int, channels: string[], replay_applied: bool, replay_count: int }` |
| `stream.closed` | yes | `{ code: enum, message: string, retry_ms: int?, reason_hint?: string }` |
| `ping` | no | `{ ts: datetime }` |
| `error` | yes | `{ code: enum, message: string, dropped_count?: int }` |
| `notification.created` | yes | Exactly mirrors the REST `Notification` schema + `channel: "sse"` + `channels: string[]` |
| `notification.read` | yes | `{ ids: uuid[], read_at: datetime, bulk: bool }` |
| `notification.dismissed` | yes | `{ id: uuid, dismissed_at: datetime }` |
| `session.invalidated_family` | yes | `{ reason: enum, logout_everywhere: bool }` |

## 3. Connection Lifecycle

The standardised sequence diagram below models the full connection lifecycle of `GET /api/v1/me/notifications/stream. Read it left-to-right: browser EventSource client → edge WAF / CORS / rate-limit / auth middleware → SSE endpoint → SSE hub + Redis replay ring → Watermill notification consumer. The lower shaded (amber/red text markers ( [OK] / [WARN] / [ERR] ) accompany every status outcome so that colour alone is never the sole cue (per WCAG 1.4.1 Use of Color).

```mermaid
%% @owner @turahe-core
%% @version 1.0.0
%% @anchor sse_lifecycle_connect sse_lifecycle_reconnect sse_watchdog sse_fanout sse_close
%% @max-nodes 80
%%{init: {
  "theme": "base",
  "themeVariables": {
    "primaryColor":        "#eff6ff",
    "primaryBorderColor":  "#2563eb",
    "lineColor":            "#475569",
    "fontFamily":           "Inter, ui-sans-serif, system-ui, sans-serif",
    "fontSize":             "13px",
    "actorBkg":             "#eef2ff",
    "actorBorder":          "#2563eb",
    "noteBkgColor":        "#fff7ed",
    "noteBorderColor":     "#d97706",
    "activationBorderColor":"#1d4ed8",
    "activationBkgColor":  "#dbeafe",
    "sequenceNumberColor": "#ffffff"
  },
  "sequence": {
    "showSequenceNumbers": true,
    "mirrorActors":        true,
    "wrap":                true,
    "width":               140,
    "height":               50
  }
}}%%
sequenceDiagram
    autonumber
    actor Browser as Browser EventSource Client
    box rgb(248,250,252) Edge & Middleware
      participant WAF as Cloudflare WAF + Nginx ingress
      participant CORS as CORS origin whitelist
      participant Rate as Redis rate limiter + concurrent limit
      participant Auth as AuthMiddleware JWT/session cookie
      participant CSRF as CSRF gate (session-cookie callers)
    end
    participant SSE as SSE GET /api/v1/me/notifications/stream
    participant Hub as SSEHub[user_id] process-local fan-out
    participant Redis as Redis ZSET sse_replay:user:<id>
    participant WM as Watermill outbox poller + pub

    rect rgb(219,234,254)
      Note over Browser,WM: SLO-critical connect handshake:  p95 < 40 ms
      Browser->>WAF: GET /api/v1/me/notifications/stream\nAuthorization, Last-Event-ID, cookie, X-CSRF-Token
      activate WAF
      WAF->>CORS: forward (origin allowlist check)
      activate CORS
      alt origin not in whitelist
        CORS-->>Browser: [ERR] 403 EnvelopeError code=cors.origin_not_allowed
        deactivate CORS
        deactivate WAF
      else origin allowed
        CORS->>Rate: per-user concurrent + reconnect check
        activate Rate
        alt too many connections / reconnect-rate jail
          Rate-->>Browser: [ERR] 429 Retry-After: 60\n(sse.stream.too_many_connections / jailed)
          deactivate Rate
          deactivate CORS
          deactivate WAF
        else within limits
          Rate->>Auth: validate JWT/session
          activate Auth
          alt missing/invalid token or session revoked
            Auth-->>Browser: [ERR] 401 UnauthorizedError
            deactivate Auth
            deactivate Rate
            deactivate CORS
            deactivate WAF
          else identity valid
            Auth->>CSRF: validate X-CSRF-Token for browser callers
            activate CSRF
            alt CSRF invalid
              CSRF-->>Browser: [ERR] 403 csrf.invalid
              deactivate CSRF
              deactivate Auth
              deactivate Rate
              deactivate CORS
              deactivate WAF
            else CSRF OK or API-token allowlist
              deactivate CSRF
              CSRF-->>SSE: forward authenticated request (user_id bound)
              deactivate Auth
              deactivate Rate
              deactivate CORS
              deactivate WAF
              activate SSE
              SSE-->>Browser: [OK] 200 OK response headers\nContent-Type: text/event-stream\nCache-Control: no-cache\nConnection: keep-alive\nX-Accel-Buffering: no
              SSE->>Hub: Register connection stream_id + user channel (1/3 per user)
              activate Hub
              Hub->>Redis: Enqueue replay from bounded ZSET (TTL 600s, max 1000 evts)
              activate Redis
              Redis-->>Hub: pending events (or empty set)
              deactivate Redis
              SSE-->>Browser: [OK] retry: 5000 + event: stream.opened\nreplay_applied=false, retry_ms=5000, channels=[default]
            end
          end
        end
      end
    end

    Note over Browser,SSE: long-lived keep-alive connection ...

    opt Watchdog ping (every 15 s)
      SSE-->>Browser: [OK] event: ping (defends proxy idle-timeout)
    end

    par Fan-out on domain event (post mutation, comment created, etc)
      WM->>Redis: Watermill outbox poller reads txn outbox row
      activate WM
      WM->>Hub: Redis PUBLISH notifications.user.{uid}.stream JSON
      Hub-->>Browser: [OK] event: notification.created/read/dismissed\n(buffer 512; overflow -> [ERR] fanout.buffer_full + drop + Prometheus counter)
      deactivate WM
    end

    Note over Browser,Hub: Clean close path

    Browser->>SSE: close EventSource / TCP FIN
    SSE->>Hub: Gin ctx.Done() fires
    Hub->>Hub: Unregister: close channel, release 512-slot buffer; decr sse_active:user:<id>
    deactivate Hub
    deactivate SSE

    break network blip: transport EOF / TCP RST between ping and close
      Browser->>Browser: EventSource client-enforced exponential backoff\n(honours retry:5000, up to 120 s cap)
      Browser->>WAF: Reconnect GET + same headers\nLast-Event-ID: <last seen id> (or ?replay_after=)
      activate WAF
      activate SSE
      activate Hub
      activate Redis
      WAF->>SSE: forward (same 401/403/429 gates apply on reconnect)
      rect rgb(222,255,222)
        Note over SSE,Redis: SLO-critical replay: gap fill p95 < 150 ms
        SSE->>Redis: ZRANGEBYSCORE sse_replay:user:<id> min (id > last_id)
        Redis-->>SSE: N missed events in monotonic order
      end
      SSE-->>Browser: [OK] stream.opened replay_applied=true + replay_count=N\nfollowed immediately by replayed frames
      deactivate Redis
      deactivate Hub
      deactivate SSE
      deactivate WAF
    end
```

**Callouts**: (1) ALL gate outcomes (401, 403, 429) are written via the standard JSON EnvelopeError payload; the stream is closed cleanly so the client receives a complete error before the connection is terminated. (2) The per-user concurrent-connection limit is 3 (SSE_MAX_CONCURRENT_PER_USER=3); the 4th active connection receives 429 with Retry-After and the response is counted in the `sse_dropped_connects_total counter before the stream is closed. (3) After a reconnect with Last-Event-ID that is older than the Replay ring TTL (600s, 1000 ev) the stream still opens with replay_applied=false and the client is expected to immediately call GET /api/v1/me/notifications to fill the gap. (4) Half-open TCP sockets (no FIN/RST) are reclaimed by the SSEHub watchdog every 30s after 2 missed pings with explicit Unregister. (5) The slow-consumer overflow branch writes a single error: error {code="fanout.buffer_full} in-band and drops the oldest buffered event, incrementing the sse_dropped_events_total Prometheus counter — the hub NEVER stalls globally for any single slow connection.

### Automatic Cleanup

- **Per-request context done**: the Gin SSE handler blocks in a `select { case <-c.Request.Context().Done(): ... case ev := <-conn.Ch: ... case <-ticker.C: write ping }`. When the client disconnects, the HTTP context is cancelled by Gin inside `c.Stream()` and the handler goroutine returns within one ticker cycle.
- **Heartbeat watchdog**: a single `SSEHub.watchdog()` goroutine wakes every `SSE_WATCHDOG_INTERVAL` (default `30s`) and closes any connection whose `lastWriteAt` is older than `2 * SSE_PING_INTERVAL_SECONDS + 10s` (i.e. 2 missed pings plus slack). This catches half-open TCP sockets where neither side sent a FIN/RST.
- **Graceful shutdown**: during `app serve` SIGINT/SIGTERM handling, `SSEHub.Shutdown()` iterates every registered connection, writes a single `event: stream.closed { code: "shutdown", retry_ms: 15000 }` frame, drains the write channel, then cancels per-connection contexts. All goroutines should exit within the configured shutdown grace period (default 10s).
- **Buffer overflow**: per-connection `chan` is sized conservatively. If a slow consumer causes the write to block longer than `SSE_WRITE_TIMEOUT` (default 250ms), the server drops the current event, increments the `sse_dropped_events_total` Prometheus counter by 1, writes a single `event: error { code: "fanout.buffer_full", dropped_count: N }` error frame, and moves on. It **never** blocks globally for a single slow client.

### Reconnect / Retry Semantics

- The server always writes `retry: 5000` on connect. Native `EventSource` uses this automatically for the initial reconnect delay after a clean drop.
- Exponential backoff is **client enforced**. For browser clients using the companion client example below, the wrapper doubles the delay on each consecutive reconnect failure up to 120s, and resets back to `retry_ms` the moment a `stream.opened` frame arrives.
- `Last-Event-ID` (or `?replay_after=`) replay: on reconnect, the server queries `sse_replay:user:<id>` with a zrangebyscore of ids strictly greater than the client's last seen id, replays them in order before any new fan-out, and sets `stream.opened.replay_applied=true` + `replay_count=N`.
- If the `Last-Event-ID` is older than the replay ring TTL or larger than the newest id, the server still opens the stream and sets `replay_applied=false`; the client should immediately call `GET /api/v1/me/notifications` to fill any gap.
- `429 Too Many Requests` during connect: `Retry-After` seconds header is present and MUST be obeyed by clients. Aggressive reconnect loops (more than 5 failures within 60s per `ip:user_id` tuple) trigger a short `SSE_JAIL_TTL_SECONDS` (default 120) Redis rate-limit entry that responds with 429 until the jail elapses.

## 4. Security Measures

### Authentication + Authorization

- Every request passes through the same `AuthMiddleware` used for other `/api/v1/me/*` endpoints.
- Valid JWT bearer or valid http-only session cookie is required; anonymous requests return `401 UnauthorizedError` via the standard envelope.
- `user_id` of the JWT `sub` claim is bound into the SSE connection; the connection can **only** receive messages published to channel keyed by that `user_id`. There is no server-side way for client code to upgrade the connection or subscribe to another user's fan-out.
- If present, the `X-2FA-Verified` or step-up headers are ignored for reads (stream never writes on behalf of the user), but are still logged for audit.

### CORS Configuration

- The SSE endpoint shares the same CORS whitelist middleware as all other `/api/v1` routes. Origin must match a configured allowlist (never `*`).
- `Access-Control-Allow-Credentials: true` is returned for whitelisted origins so browser clients can pass the session cookie.
- `Access-Control-Allow-Headers` must include `Authorization`, `Last-Event-ID`, `X-CSRF-Token` for browser preflight `OPTIONS` responses.
- Non-whitelisted origins fall through to the standard error path: HTTP 403 with `code = "cors.origin_not_allowed"`.

### Rate Limiting

Three independent Redis-backed rate limits protect `/api/v1/me/notifications/stream`:

| Bucket | Scope | Default limit | 429 code |
|--------|-------|---------------|----------|
| Per-user concurrent SSE connections | `user:<id>` | 3 active streams | `sse.stream.too_many_connections` |
| Reconnect attempts (connects per window) | `ip:user:<id>` | 10 per 60 seconds | `sse.stream.reconnect_rate` |
| Jail for aggressive re-connects | `ip:user:<id>` | ≥5 failures in 60s → 120s jail | `sse.stream.jailed` |

Concurrent connections are tracked in a Redis counter (`sse_active:user:<id>`) with the same TTL as the proxy idle timeout, plus a process-local in-memory hub map for quick enforcement. Counters are decremented by `hub.Unregister` on clean disconnect, and by the watchdog for half-open sockets.

### Additional Hardening

- **No query-string JWTs**: tokens must always be in `Authorization` header or session cookie. Never accept `?access_token=…` to avoid leaking into access logs, browser history, or `Referer` headers.
- **CSRF defence for session-cookie callers**: if the request arrived with a session cookie and passes a non-empty `Origin` header matching a browser-origin, validate the `X-CSRF-Token` header; failure → 403 `csrf.invalid`.
- **Response headers**: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`. `Content-Security-Policy` is set globally by the browser security middleware; SSE connections do not relax it.
- **Sanitize every `data:` payload before writing**: even though the server originates all payloads, the write path passes the JSON string through the same markdown+HTML allowlist sanitizer used for comment bodies so that user-controlled notification titles/previews cannot contain raw control sequences that might confuse naive parsers in older EventSource polyfills.
- **Audit log**: connect / disconnect / reconnect-with-replay / too-many-connections / jailed-reconnect events are all written to the structured application log with `actor_id`, `stream_id`, `correlation_id`, `source_ip`, `user_agent_hash`. The sensitive `user_agent` is never stored in full; only SHA-256 (peppered) is retained per the PII retention policy.

## 5. Client-Side Integration (EventSource)

### Minimal Example (Native EventSource)

```typescript
// browser / Next.js client-side component
import { useCallback, useEffect, useRef, useState } from "react";

type StreamOpenedData = {
  stream_id: string;
  user_id: string;
  retry_ms: number;
  replay_applied: boolean;
  replay_count: number;
  channels: string[];
};

type NotificationCreatedData = {
  notification_id: string;
  type: string;
  title: string;
  preview: string | null;
  read: boolean;
  created_at: string;
};

type ClientStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed" | "unsupported";

type InAppNotification = {
  id: string;
  type: string;
  title: string;
  preview: string | null;
  read: boolean;
  createdAt: string;
  fromSse: boolean;
};

const API_BASE = process.env.NEXT_PUBLIC_API_BASE ?? "/api";
const STREAM_PATH = "/api/v1/me/notifications/stream";

/**
 * EventSource wrapper that:
 *   - avoids query-string auth (uses header via fetch + polyfill if needed)
 *   - tracks Last-Event-ID for replay on reconnect
 *   - enforces exponential backoff cap independent of native EventSource defaults
 *   - dedupes notification.created by notification_id (at-least-once → exactly-once UI)
 *   - detects SSE-unsupported browsers and falls back to REST polling
 */
export function useNotificationStream(jwt: string | null) {
  const sourceRef = useRef<EventSource | null>(null);
  const lastIdRef = useRef<string | null>(null);
  const backoffRef = useRef<{ attempt: number; timer: ReturnType<typeof setTimeout> | null }>({
    attempt: 0,
    timer: null,
  });
  const [status, setStatus] = useState<ClientStatus>("idle");
  const [notifications, setNotifications] = useState<InAppNotification[]>([]);
  const [lastError, setLastError] = useState<{ code: string; message: string } | null>(null);

  const fetchHistory = useCallback(async () => {
    const r = await fetch(`${API_BASE}/api/v1/me/notifications?page=1&per_page=50`, {
      headers: jwt ? { Authorization: `Bearer ${jwt}` } : undefined,
      credentials: "include",
    });
    if (!r.ok) return;
    const body = (await r.json()) as { data?: { items?: Array<{ id: string; type: string; title: string; preview?: string | null; read: boolean; created_at: string }> } };
    const items = body.data?.items ?? [];
    setNotifications(items.map((n) => ({
      id: n.id,
      type: n.type,
      title: n.title,
      preview: n.preview ?? null,
      read: n.read,
      createdAt: n.created_at,
      fromSse: false,
    })));
  }, [jwt]);

  const connect = useCallback(() => {
    if (typeof EventSource === "undefined") {
      setStatus("unsupported");
      void fetchHistory();
      const poll = setInterval(() => void fetchHistory(), 60_000);
      return () => clearInterval(poll);
    }

    if (!jwt) {
      setStatus("closed");
      return undefined;
    }

    setStatus((prev) => (prev === "open" || prev === "reconnecting" ? "reconnecting" : "connecting"));

    // EventSource does not support custom headers natively. For bearer-token auth we use
    // a fetch + ReadableStream based polyfill-style wrapper that exposes the same API, or
    // rely on the http-only session cookie via { withCredentials: true } when a cookie is set.
    // Here we demonstrate the cookie path (works out-of-the-box in Next.js SSR apps)
    // and include a note below for the bearer-token alternative.
    const url = new URL(`${API_BASE}${STREAM_PATH}`);
    url.searchParams.set("channels", "default");

    const source = new EventSource(url.toString(), { withCredentials: true });
    sourceRef.current = source;

    source.addEventListener("stream.opened", (ev: MessageEvent<string>) => {
      const data = JSON.parse(ev.data) as StreamOpenedData;
      backoffRef.current.attempt = 0;
      setStatus("open");
      setLastError(null);
      if (!data.replay_applied) {
        void fetchHistory();
      }
      // Note: Last-Event-ID is automatically persisted by the browser EventSource implementation
      // and resent on reconnect. We also mirror it into the ref for REST polling fallbacks.
      if (ev.lastEventId) lastIdRef.current = ev.lastEventId;
    });

    source.addEventListener("stream.closed", (ev: MessageEvent<string>) => {
      const data = JSON.parse(ev.data) as { code: string; message: string; retry_ms?: number };
      setLastError({ code: data.code, message: data.message });
      source.close();
      if (data.code === "too_many_connections" || data.code === "auth.expired" || data.code === "session.revoked") {
        setStatus("closed");
      } else {
        scheduleReconnect(data.retry_ms ?? 5000);
      }
    });

    source.addEventListener("error", (_ev) => {
      // EventSource fires a plain 'error' for any transport failure, including 4xx/5xx.
      // We map it to our reconnect schedule with exponential backoff.
      const baseDelay = 5000;
      const jitter = Math.random() * 1200;
      const capped = Math.min(baseDelay * Math.pow(2, Math.min(backoffRef.current.attempt, 5)), 120_000);
      scheduleReconnect(capped + jitter);
      setStatus((prev) => (prev === "open" ? "reconnecting" : prev));
    });

    source.addEventListener("notification.created", (ev: MessageEvent<string>) => {
      const data = JSON.parse(ev.data) as NotificationCreatedData;
      if (ev.lastEventId) lastIdRef.current = ev.lastEventId;
      setNotifications((prev) => {
        if (prev.some((n) => n.id === data.notification_id)) return prev; // dedupe (at-least-once)
        return [
          {
            id: data.notification_id,
            type: data.type,
            title: data.title,
            preview: data.preview,
            read: data.read,
            createdAt: data.created_at,
            fromSse: true,
          },
          ...prev,
        ];
      });
    });

    source.addEventListener("notification.read", (ev: MessageEvent<string>) => {
      const data = JSON.parse(ev.data) as { ids: string[] };
      setNotifications((prev) => prev.map((n) => (data.ids.includes(n.id) ? { ...n, read: true } : n)));
      if (ev.lastEventId) lastIdRef.current = ev.lastEventId;
    });

    source.addEventListener("notification.dismissed", (ev: MessageEvent<string>) => {
      const data = JSON.parse(ev.data) as { id: string };
      setNotifications((prev) => prev.filter((n) => n.id !== data.id));
      if (ev.lastEventId) lastIdRef.current = ev.lastEventId;
    });

    source.addEventListener("session.invalidated_family", () => {
      // Tear down all local auth state and route back to login so SSR headers refresh.
      source.close();
      setStatus("closed");
      window.dispatchEvent(new CustomEvent("auth:logout-required"));
    });

    const scheduleReconnect = (ms: number) => {
      if (backoffRef.current.timer) clearTimeout(backoffRef.current.timer);
      backoffRef.current.attempt += 1;
      backoffRef.current.timer = setTimeout(() => {
        if (source.readyState === EventSource.OPEN || source.readyState === EventSource.CONNECTING) return;
        // EventSource will reconnect on its own once the underlying TCP fails. scheduleReconnect
        // only fires when we explicitly closed the source, so we call connect() again here.
        void connect();
      }, ms);
    };

    return () => {
      if (backoffRef.current.timer) clearTimeout(backoffRef.current.timer);
      source.close();
      if (sourceRef.current === source) sourceRef.current = null;
      setStatus("closed");
    };
  }, [jwt, fetchHistory]);

  useEffect(() => {
    const cleanup = connect();
    return cleanup;
  }, [connect]);

  const markRead = useCallback(async (id: string) => {
    const res = await fetch(`${API_BASE}/api/v1/me/notifications/${id}/read`, {
      method: "POST",
      headers: jwt ? { Authorization: `Bearer ${jwt}` } : undefined,
      credentials: "include",
    });
    return res.ok;
  }, [jwt]);

  return { status, notifications, markRead, lastEventId: lastIdRef.current, lastError };
}
```

### Bearer-Token Alternative (Custom ReadableStream Parser)

Native `EventSource` does not permit custom `Authorization` headers. For clients that must
use bearer tokens instead of cookies, replace the wrapper above with a `fetch` +
`ReadableStream` parser that understands the `text/event-stream` wire format. A minimal
reference parser:

```typescript
async function openSseWithToken(url: string, token: string, handlers: { onEvent: (name: string, data: string, id: string) => void }) {
  const res = await fetch(url, {
    method: "GET",
    headers: { Authorization: `Bearer ${token}`, Accept: "text/event-stream" },
    credentials: "omit",
  });
  if (!res.ok || !res.body) throw new Error(`SSE connect failed: ${res.status}`);
  const reader = res.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buf = "";
  let event = "message";
  let id = "";
  let dataLines: string[] = [];
  const flush = () => {
    if (dataLines.length) handlers.onEvent(event, dataLines.join("\n"), id);
    event = "message";
    id = "";
    dataLines = [];
  };
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf("\n")) !== -1) {
      const line = buf.slice(0, idx);
      buf = buf.slice(idx + 1);
      if (line === "") { flush(); continue; }
      if (line.startsWith(":")) continue; // comment/ping
      const colon = line.indexOf(":");
      const field = colon === -1 ? line : line.slice(0, colon);
      const value = colon === -1 ? "" : line.slice(colon + (line[colon + 1] === " " ? 2 : 1));
      switch (field) {
        case "event": event = value; break;
        case "data": dataLines.push(value); break;
        case "id": if (!value.includes("\0")) id = value; break;
        case "retry": /* client uses its own backoff; ignore server hint or pass to scheduler */ break;
      }
    }
  }
  flush();
}
```

## 6. Testing Procedures

All SSE tests live alongside the other HTTP/integration tests. The areas below are required
in the `testing.md` plan (see the "Realtime SSE stream" block appended there) and are
described here at the procedure level.

### 6.1 Successful Connection Establishment

- **Unit**: `SSEHub.Register` returns a non-nil `*Connection` bound to the correct user id, increments the in-memory `activeCount` map, and `hub.GetActive()` reflects it. `Unregister` decrements it.
- **Integration**: Start a `httptest` server, call `GET /api/v1/me/notifications/stream` with a valid test JWT, set `:end-of-stream-wait-ms=1200` in a test-only header. Assert:
  - response status 200, `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-cache`
  - body contains `retry: 5000` exactly once and an `event: stream.opened` frame
  - `stream.opened.replay_applied == false` on first connect with no Last-Event-ID
- **Contract**: run Redocly lint against the OpenAPI path and confirm all required headers
  are declared; confirm response examples match the protocol.

### 6.2 Realtime Event Delivery to All Connected Clients (Fan-Out)

- **Integration**: Register `N` connections for the same user in the same process `SSEHub`. Publish a single `blog.comment.created` event via the Watermill in-process test publisher. Assert every connection received a corresponding `notification.created` frame with matching `notification_id`; assert the `id:` sequence is monotonically increasing across frames and identical `notification.created` id values are emitted to all N clients.
- **Multi-process fan-out** (requires Redis test instance): Start two `httptest` servers sharing the Redis backend. Publish on process A's Watermill bus; assert both process A and process B clients received the same event frames within `MAX_FANOUT_LATENCY_MS` (default 500ms wall clock).
- **User isolation**: Register 1 connection for user A, 1 for user B. Publish a user-A notification. Assert user B's read buffer stays empty for at least 300ms (never leaks across users).

### 6.3 Reconnection Behaviour After Network Interruptions

- **Unit**: Simulate a server write sequence then disconnect by cancelling the request context. Reconnect with the exact last id received before disconnect via `Last-Event-ID` header; assert the server replays only events strictly greater than the supplied id (no duplicates, no gaps within ring TTL).
- **Chaos (integration)**: Use a middleware gzip layer or test TCP killer that drops connections every ~50 events over a 10 000 event publish run. After all reconnects converge, collect every notification id received by the client. Assert the union of ids across reconnects + final REST history call covers all 10 000 ids (at-least-once + replay).
- **Retry hint**: Assert reconnect wrapper doubles the delay each consecutive transport failure, reaching a 120s cap; resets to `retry_ms` immediately after the next `stream.opened` event.

### 6.4 Graceful Degradation for Non-SSE Clients

- In a jsdom-like test that does not implement `EventSource`, assert the hook falls back to
  60s REST polling and successfully hydrates the initial 50 notifications from
  `GET /api/v1/me/notifications`.
- In an HTTP/1.0 test client that only does connection-close semantics, assert the endpoint
  either returns 200 OK and writes the initial handshake before the close (graceful drain)
  OR returns `400 sse.protocol_upgrade_required` depending on configured strictness; never
  returns 500 or leaks a goroutine (track with `runtime.NumGoroutine()` delta).

### 6.5 Security + Rate Limit Behaviour

- Assert valid CORS whitelist origin passes; non-whitelisted → 403 `cors.origin_not_allowed`.
- Assert `Authorization: Bearer <malformed>` → 401 UnauthorizedError with no leak of frame bytes.
- Assert JWT for user X with an attempt to read frames from a Redis-side stream keyed to user Y cannot happen; use a test-only `X-Debug-Force-User-Id` header and confirm the middleware strips it before fan-out registration.
- Open 3 connections for the same user (inside 1s). Assert 4th connect returns 429 with `Retry-After: 30` and body code `sse.stream.too_many_connections`.
- Run 12 connects over a 60s window for the same `ip:user` tuple. Assert the last 2 respond with 429 jail `sse.stream.jailed` and `Retry-After: 120`.
- Buffer overflow: attach a consumer that reads 1 frame/sec. Write 1000 events as fast as possible. Assert `event: error { code: "fanout.buffer_full" }` is emitted exactly once with an integer `dropped_count`; assert the hub keeps running and non-slow clients continue unblocked.

## 7. Error Codes

The following machine-readable codes appear in 4xx/5xx JSON envelopes as `EnvelopeError.code`
or inside `event: error` / `event: stream.closed` frames:

| Code | HTTP or stream frame | Meaning | Retry-After / `retry_ms` |
|------|----------------------|---------|---------------------------|
| `unauthorized` | 401 envelope | missing or invalid auth | n/a |
| `forbidden` | 403 envelope | CSRF / CORS origin / scope missing | n/a |
| `cors.origin_not_allowed` | 403 envelope | origin outside allowlist | n/a |
| `csrf.invalid` | 403 envelope | CSRF token missing / tampered | n/a |
| `rate_limit.exceeded` | 429 envelope | generic Redis limiter trip | seconds in header |
| `sse.stream.too_many_connections` | 429 envelope + 429 header | user hit `SSE_MAX_CONCURRENT_PER_USER` | seconds in header |
| `sse.stream.reconnect_rate` | 429 envelope + 429 header | reconnect burst within 60s | seconds in header |
| `sse.stream.jailed` | 429 envelope + 429 header | aggressive reconnect pattern → jail | 120s default in header |
| `sse.protocol_upgrade_required` | 400 envelope | client cannot accept `text/event-stream` | n/a |
| `fanout.buffer_full` | `event: error` | single consumer dropped frames | current event only |
| `auth.expired` | `event: stream.closed` | session / JWT timed out during a long-lived stream | 15 000 ms |
| `session.revoked` | `event: stream.closed` | admin revoked or user logged out everywhere | 15 000 ms |
| `shutdown` | `event: stream.closed` | server shutting down gracefully | 15 000 ms |
| `maintenance` | `event: stream.closed` | deployment slot swap or controlled drain | 60 000 ms |
| `sse.channel_unknown` | `event: error` | `?channels=` contained an unsupported channel string | current frame only |
| `internal.server_error` | 5xx envelope / `event: error` | unexpected panics recovered by the SSE writer middleware | 30 000 ms jittered |

## 8. Implementation Best Practices — Scaling to High Concurrency

This section is written for the Go backend implementation. All knobs are exposed as env vars
with sensible defaults; override per-environment in `configs/<env>.env`.

### 8.1 Runtime / Process

- **Use HTTP/2** in production (TLS ALPN negotiated or behind a TLS-terminating LB running h2). HTTP/2 multiplexes SSE streams over a single TCP conn per browser and removes the per-browser 6-conn HTTP/1.1 limit; this alone typically raises ceiling by 10× for the same LB tier.
- **Run 1 `SSEHub` per process** with its own worker goroutines. For multi-process deployments, inter-process fan-out MUST happen over Redis pub/sub (or Kafka, via Watermill): whenever a domain event triggers a notification, the Watermill consumer publishes to both `notifications.user.{id}.local` (in-process hub) and `notifications.user.{id}.bus` (Redis channel). Each process subscribes to `notifications.user.*.bus` and forwards matches to its local hub.
- **Prefer small write buffers with aggressive drops** over unbounded memory. 512 slots per connection is plenty; anything more and you are masking a slow-consumer problem that should be solved at the client (coalesce UI updates, not server).
- **Set `GOGC=50` (aggressive GC)** in processes whose only job is SSE fan-out; steady state will produce lots of short-lived `[]byte` write frames and the GC should keep RSS low.

### 8.2 Middleware Stack

Put middlewares on the SSE route in this order:

1. Request ID + structured access logger (avoid logging body; SSE never has one)
2. CORS whitelist → short-circuit 403 immediately
3. Rate limiter (concurrent + reconnect) → 429 short-circuit
4. Auth middleware (validate JWT / session)
5. CSRF gate (for browser-origin session cookie requests)
6. `SSEHub.Register` / defer `Unregister` handler

If you need server-timing / OpenTelemetry spans on SSE, keep them minimal: emit a single span per
connect, a counter event per fan-out write; never a per-frame span (cardinality will explode).

### 8.3 Reverse Proxy / Load Balancer Configuration

| Setting | Nginx / OpenResty | Envoy | AWS ALB |
|---------|-------------------|-------|---------|
| Buffering | `proxy_buffering off;` + send `X-Accel-Buffering: no` from app | Set `route.per_request_buffer_limit_bytes: 0` for the SSE route; use direct response passthrough | ALB by default streams; disable Lambda content processing if attached |
| Idle timeout | `proxy_read_timeout 240s;` keep higher than `2 * ping interval + slack` | `idle_timeout: 240s` on the route `action` | idle timeout 350s max; set to 240s |
| Upstream keepalive | `keepalive 256;` upstream with `http_version 1.1` + `Connection: ""` upstream | Use H2 upstream pool if app speaks HTTP/2 | N/A (ALB multiplexes) |
| gzip | Turn off for `text/event-stream`; proxy `Content-Encoding` direct | Disable decompressor on the route | ALB: no compression for this MIME |
| HTTP/2 | `listen 443 ssl http2;` | Configure h2 on downstream listener + upstream cluster | ALB: `Protocol: HTTPS` with HTTP/2 enabled listener |
| Origin stickiness | Not required; any process can serve any user because fan-out is via Redis bus | Same | Use least-outstanding-requests routing; avoid session cookies here |

### 8.4 Replay Ring (Redis)

- Key pattern: `sse_replay:user:<user_id>`. Data structure: Sorted Set, `score = int64 parse of the monotonic id prefix`, `member = <full event id>\t<json>` (tab-separated so we can reconstruct an exact frame on replay).
- Two background maintenance operations per process:
  1. Every 5 minutes: for every user key touched in the last 10 min, `ZREMRANGEBYRANK` to keep only the most recent `SSE_REPLAY_MAX_EVENTS_PER_USER` (1000).
  2. Every minute: scan with cursor pattern and TTL-expire keys older than `SSE_REPLAY_TTL_SECONDS` (Redis TTL is also set on each `ZADD` so keys disappear automatically; the scan is belt-and-suspenders).
- **Do not use Redis Streams** unless you need consumer groups. A ZSET replay ring is simpler, cheaper, and matches "last N ids per user" semantics exactly.

### 8.5 Metrics and Observability

Expose the following Prometheus counters/gauges via the `/metrics` endpoint alongside other
backend metrics:

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `sse_connections_active` | gauge | `user_id_bucket`, `pid`, `host` | Total active per process; feed HPA at target 80% of per-process cap |
| `sse_connect_total` | counter | `host`, `status=[ok,rejected_unauth,rejected_cors,rejected_rate]` | Connect success / failure mix |
| `sse_reconnect_with_replay_total` | counter | `host` | Replay logic being exercised; alarm if it spikes unexpectedly |
| `sse_events_fanout_total` | counter | `event_name`, `host` | Per-event write rate (at-least-once, so it is `N clients × 1 notification`) |
| `sse_dropped_events_total` | counter | `host`, `code=[buffer_full,write_timeout,user_not_found]` | Alarm on upward trend → slow clients or undersized buffers |
| `sse_watchdog_closed_total` | counter | `host` | Half-open sockets caught per watchdog cycle |
| `sse_write_duration_seconds` | histogram | `host`, `event_name` | p95 per-frame write latency to `ResponseWriter`; includes flush cost |

Set alerts on:

- `sse_connections_active` / process cap ≥ 0.9 sustained for 5 min → add capacity.
- `sse_dropped_events_total` rate > 10/sec per host → investigate slow clients.
- Rejected rate (`sse_connect_total{status!="ok"}` / total) > 5% → CORS misconfig, session invalidation storm, or aggressive reconnect jail is too wide.
- Watchdog-closed rate > normal baseline (e.g. > 1% of connects) → proxy idle timeout too aggressive.

### 8.6 Graceful Degradation Strategy

When SSE is unavailable (health check reports unhealthy, or the region is failing):

1. The server-side route responds with a static 503 + `Retry-After: 60` and closes without leaking frames.
2. Client `useNotificationStream` wrapper detects the 503 and flips to a polling mode (30s).
3. Backend `app doctor` (Cobra doctor command) includes a dedicated `SSE subsystem` check that:
   - connects to local hub over loopback test JWT
   - writes a synthetic event and reads it back within 300ms
   - pings Redis replay ring write + read round trip
4. Kubernetes probes route `/health/ready` marks the pod NotReady if the SSE check fails so new connections are not routed to the bad pod until it recovers.
