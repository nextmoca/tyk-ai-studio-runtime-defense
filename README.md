# Next Moca Runtime Defense for AI Agents on Tyk AI Studio

This Tyk AI Studio Microgateway plugin calls the hosted Next Moca selection API at the request-side `post_auth` hook. It governs the context delivered to an LLM provider: it selects relevant eligible context and, in `quarantine` and `enforce`, applies the security verdict the hosted API returns. It can write a content-free receipt to the plugin log.

It is a last-hop request-context control. Version 1 does **not** authorize tool execution, inspect tool arguments after the model chooses a tool, or enforce output policy.

Read [Failure handling](#failure-handling) before relying on `enforce`: by default the plugin passes the original request through when it cannot get a verdict.

## Supported request shape

V1 supports OpenAI-compatible JSON requests whose `messages` values have string `content` fields.

- The final non-empty `user` message is the task and remains unchanged.
- Earlier `user` messages, `assistant` messages, and `tool` messages are eligible candidate context.
- `system` and `developer` messages are not selected or rewritten.
- Tool definitions, message order, message count, roles, IDs, names, and non-content fields are preserved.
- Anything else is not governed, and by default it is sent upstream unchanged. See [Request shapes that are not governed](#request-shapes-that-are-not-governed).

For the next LLM call after a tool result, the application normally includes that result as a `tool` message. The plugin then treats that message as eligible context before the model reads it.

## Configure the gateway

1. Build the plugin binary or image from this directory.
2. [Create a Needlepath API key](https://docs.nextmoca.com/authentication#getting-an-api-key), then set it as a secret in the Microgateway process environment. The configuration holds only the environment-variable name, never the key itself. Because Tyk runs a plugin as a child process, allow that exact variable through its environment allowlist on every process that can host the plugin:

   ```bash
   export NEXTMOCA_API_KEY="np_live_..."
   export PLUGIN_ENV_PASSTHROUGH="NEXTMOCA_API_KEY"
   ```

   In Docker Compose, add both variables to the AI Studio and Microgateway services that load the plugin. Do not put the API key in the plugin configuration, manifest, or container image.

3. Mount the architecture-matched binary in both Tyk services and register the relative command path. For a Docker deployment, mount the same artifact at `/opt/tyk-ai-studio/plugins` in AI Studio and `/opt/tyk-microgateway/plugins` in the Microgateway, then configure the command as `plugins/nextmoca-tyk-plugin-linux-amd64` or `plugins/nextmoca-tyk-plugin-linux-arm64`. This preserves Tyk's default plugin-path policy; no broad path allowlist is required.
4. Register the binary with Tyk AI Studio as a gateway plugin and attach it to the `post_auth` hook. Use the plugin's [`manifest.json`](manifest.json) and [`config.schema.json`](config.schema.json) when registering a local or marketplace artifact.
5. Start in `shadow` mode, inspect the content-free decision receipts and hosted Needlepath telemetry, then promote deliberately.

Example plugin configuration:

```json
{
  "api_key_env": "NEXTMOCA_API_KEY",
  "endpoint": "https://api.nextmoca.com",
  "operating_point": "np-2026-10-r8",
  "mode": "shadow",
  "max_context_tokens": 8192,
  "capacity_tokens": 128000,
  "timeout_ms": 3000,
  "telemetry_enabled": true,
  "on_selection_error": "pass",
  "unsupported_shape": "pass",
  "breaker_failures": 5,
  "breaker_cooldown_ms": 30000
}
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `operating_point` | none, required | The immutable Next Moca label to pin. |
| `mode` | `shadow` | Delivery policy. See [Delivery modes](#delivery-modes). |
| `timeout_ms` | `3000` | Per-call timeout for the hosted API. |
| `on_selection_error` | `pass` | `pass` or `block`. What `quarantine` and `enforce` do when the hosted call fails. |
| `unsupported_shape` | `pass` | `pass` or `block`. What `quarantine` and `enforce` do with a request the plugin cannot govern. |
| `breaker_failures` | `5` | Consecutive timeouts, transport errors, or 5xx responses that open the circuit breaker. |
| `breaker_cooldown_ms` | `30000` | How long the circuit stays open before one probe call is allowed. |
| `telemetry_enabled` | `false` | Write a content-free decision receipt to the plugin log for every request. |

`capacity_tokens` is the actual downstream model input room after the application reserves system instructions, the final task, conversation history, tools, output tokens, and a safety margin. It is deliberately independent of `max_context_tokens`.

## Delivery modes

| Mode | Selected context | `sanitized_fallback` | `blocked` | Security scanning requested |
| --- | --- | --- | --- | --- |
| `shadow` | Observed only; original request is sent | Original request | Original request | Audit only |
| `active` | Rewritten per original candidate message | Original request | Original request | No |
| `quarantine` | Rewritten per original candidate message | Sanitized single-candidate request; otherwise blocked | Observed; original request is sent | Yes |
| `enforce` | Rewritten per original candidate message | Sanitized single-candidate request; otherwise blocked | HTTP 403; upstream is not called | Yes |

In every mode, a normal fallback (the hosted API declined to select) sends the original request. Failures of the hosted call and request shapes the plugin cannot govern are covered below.

`active` is a selection-only mode. It sends `security_mode: "off"`, so the hosted API does not apply security labels, and the plugin never blocks. Use `quarantine` or `enforce` when you want security verdicts applied.

### What a `sanitized_fallback` does

The hosted API returns one rendered packet for a `sanitized_fallback`. The plugin applies it only when there is exactly one eligible candidate message, replacing that message's content with the packet. In `quarantine` and `enforce` the plugin never sends the original request after a `sanitized_fallback`. It blocks (HTTP 403) when:

- there is more than one candidate message, because one packet cannot be split across messages without concatenating them;
- the rendered packet is empty;
- the request cannot be rewritten.

### What happens to unselected messages

When the hosted API selects context, the plugin rewrites the request in place and keeps the message count, order, roles, IDs, and other fields. For each eligible candidate message:

- if the API selected it, its `content` is replaced with the selected excerpt;
- if the API did not select it, its `content` is replaced with the fixed marker `[Next Moca omitted nonessential context for this request.]`.

The marker is the only text the plugin adds. System and developer messages and the final user message are never changed. If the API returns selections that match no candidate, the original request is sent.

### `blocked` in `quarantine`, and how `enforce` differs from the SDKs

In `quarantine` the plugin observes a hosted `blocked` outcome and sends the original request; only `enforce` acts on it. Treat `quarantine` as selection plus sanitized delivery, not as a block.

In `enforce`, a hosted `blocked` outcome makes the plugin return HTTP 403 with a `context_security_blocked` error body and the upstream provider is not called. The Next Moca SDKs and other integrations behave differently: they report the result as not applied and leave the host's own request to proceed, they do not refuse it. Decide deliberately whether a gateway 403 is the behavior you want.

## Failure handling

The plugin fails open by default. In `enforce`, as in every other mode, **a failed call to the hosted API sends the original request upstream unless `on_selection_error` is `block`.** That includes a rejected API key, an exhausted quota, rate limiting, and a timeout. A gateway whose key has expired is not enforcing anything until the key is fixed.

An invalid or incomplete plugin startup configuration is different: the plugin remains loaded and returns HTTP 503 before the upstream LLM for every governed request until Tyk applies a valid configuration. It never becomes a silent pass-through because `operating_point` or `api_key_env` was omitted. This also lets Tyk safely probe the plugin metadata before applying its configured values.

Each failure has a content-free class, which the plugin always writes to the process log, even when `telemetry_enabled` is false:

| Class | Cause |
| --- | --- |
| `auth` | HTTP 401 or 403: the API key was rejected. |
| `quota` | HTTP 402: the account is out of quota. |
| `rate_limit` | HTTP 429. |
| `oversize` | The request or the response exceeded 6 MB, or HTTP 413. |
| `timeout` | The call exceeded `timeout_ms`. |
| `transport` | The connection failed. |
| `server` | An HTTP 5xx response. |
| `contract` | Any other HTTP status, or a response the plugin could not use. |
| `request` | The plugin could not build the call. |
| `circuit_open` | The circuit breaker is open, so no call was made. |
| `canceled` | The gateway cancelled the request. |

The log line carries the class and the plugin's reason code. It never carries request content or the API key.

### Hosted response contract

For every hosted selection response that it accepts, the plugin requires a
non-empty `outcome`, a boolean `fallback_used`, and a `request_id` that exactly
matches the ID it sent. A missing field or mismatched ID is a `contract`
selection error. It follows the configured `on_selection_error` policy: pass
through by default, or HTTP 503 in `quarantine` and `enforce` when configured
to `block`.

Set `on_selection_error` to `block` to fail closed in `quarantine` and `enforce`. The plugin then returns HTTP 503 with a `context_security_unavailable` error body and does not call the upstream provider. `shadow` and `active` always pass the request through, whatever this setting says.

### Timeout and circuit breaker

The default timeout is 3 seconds. The plugin makes one attempt per request, does not retry, and does not follow redirects (a redirect response is a `contract` failure). After `breaker_failures` consecutive timeouts, transport errors, or 5xx responses, the circuit opens for `breaker_cooldown_ms`. While it is open, requests skip the hosted call and are handled according to `on_selection_error`. After the cool-down, one probe call is allowed; if it succeeds the circuit closes, and if it fails the circuit stays open for another cool-down. Responses such as 401 or 429 show the API is reachable and do not count toward opening the circuit.

## Request shapes that are not governed

The plugin governs OpenAI-compatible chat requests whose messages have string `content`. In every mode, including `enforce`, these requests are **not** governed and by default are sent upstream unchanged:

- A request in which any message has array `content` (multimodal or typed parts). One such message leaves the whole request ungoverned.
- A request with no non-empty `user` message to use as the task, or with a message that is not an object with a string `role`.
- A request that repeats a JSON key at the top level or inside a message. Parsers disagree on which value wins, so the plugin cannot tell which content the provider will read.
- Requests with no `messages` array, non-POST calls, and bodies that are not valid JSON. These are not chat requests and are never blocked.

Messages with the legacy `function` role are never sent for selection and are never rewritten, even when the rest of the request is governed. Their content reaches the provider as sent.

Set `unsupported_shape` to `block` to refuse, in `quarantine` and `enforce`, any chat request in the first three groups or containing a `function`-role message. The plugin then returns HTTP 422 with a `context_security_unsupported_request` error body and does not call the upstream provider. Use it only if your clients send plain string content.

## Fail-open and security boundaries

An explicit hosted `blocked` outcome is authoritative only in `enforce`. A `sanitized_fallback` is the exception to fail-open in `quarantine` and `enforce`: because the hosted API has said the raw content is unsafe, the plugin never falls back to the original request after it.

The plugin only makes a request-side context decision. Pair it with a tool/action authorization layer for side-effecting tool calls and a response guardrail for output enforcement.

## Receipts and privacy

When `telemetry_enabled` is true, the plugin writes one line per request to its process log. A receipt contains:

- delivery outcome and reason class
- failure class, when the hosted call failed
- token counters and engine latency
- selection identifier

When `telemetry_enabled` is false, the plugin logs only the failure class and reason code of a failed hosted call, as described in [Failure handling](#failure-handling).

## Build and test

```bash
make test
make build-linux-amd64
make build-linux-arm64
docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin:dev .
```

The module uses the public Tyk AI Studio SDK. Its current public SDK module metadata references a private Tyk server-only module that the plugin API does not import; [`third_party/ai-studio-enterprise-placeholder`](third_party/ai-studio-enterprise-placeholder) is a local empty-module replacement that makes the standalone plugin build without any enterprise source or runtime dependency.

## Marketplace release bundle

The source-controlled [Marketplace release bundle](marketplace/) contains the
versioned manifest template, public documentation, changelog, and icon for a
Tyk Community Marketplace submission. Publish a signed OCI image first, then
replace the bundle's release placeholders with the resulting immutable digest.
The container itself contains only this compiled plugin adapter; it does not
contain the Next Moca selection engine, policy rules, models, or credentials.

## License

This Tyk AI Studio plugin is licensed under [Apache License 2.0](LICENSE). The license applies only to the code in this directory. The hosted Next Moca / Needlepath service, its APIs, and its underlying selection and security technology remain proprietary and are governed by their applicable service terms.
