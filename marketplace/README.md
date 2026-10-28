# Next Moca Runtime Defense for AI Agents

Next Moca is a last-hop request-context control for agents routed through Tyk
AI Studio. At the `post_auth` hook, this plugin sends eligible context to the
hosted Next Moca service and applies the returned delivery decision before the
request reaches the configured LLM provider.

The OCI artifact contains only the compiled Tyk plugin adapter. It does not embed
the Next Moca selection engine, security rules, a model, application context,
or customer credentials.

## What It Does

- Selects relevant eligible context while preserving provider message order,
  roles, tool identifiers, and non-content fields.
- Applies hosted `selected`, `fallback`, `sanitized_fallback`, and `blocked`
  delivery outcomes according to the configured mode.
- In `enforce`, returns HTTP 403 for a hosted security block and prevents the
  upstream provider request.
- Emits content-free receipts with outcome, reason, token counters, engine
  latency, selection ID, and any failure class.

It is not output moderation or tool-execution authorization.

## Install

Install the published, digest-pinned artifact from AI Studio's Marketplace:

```text
oci://ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin@sha256:RELEASE_DIGEST
```

Then:

1. Store the Next Moca key in AI Studio's secret mechanism and set plugin
   `api_key` to the resulting `$SECRET/NAME` reference. Environment passthrough
   through `api_key_env` remains supported for existing deployments.
2. Configure an explicit operating point and begin with `mode: shadow`.
3. Attach the plugin to the intended LLM/provider route and push the gateway
   configuration. Installing a plugin does not place it in traffic.
4. Inspect content-free receipts, then promote a measured canary to `active`
   or `enforce`.

The plugin supports OpenAI-compatible chat requests with string message
content. See the [integration README](https://github.com/nextmoca/tyk-ai-studio-runtime-defense)
for modes, unsupported-shape handling, failures, and configuration details.

## Before You Install

- Eligible context leaves the gateway in every mode, including `shadow`: the
  final user prompt plus earlier user, assistant, and tool messages are sent
  to `api.nextmoca.com`. System and developer messages are not sent.
- In `active`, `quarantine`, and `enforce`, the hosted service can rewrite
  candidate message content before it reaches the configured LLM provider.
- Each supported chat request makes one synchronous hosted call. The default
  timeout is 3 seconds and the plugin uses a circuit breaker after repeated
  availability failures.
- The default is fail-open when the hosted API cannot be reached. Set
  `on_selection_error: block` in `quarantine` or `enforce` to fail closed.

## Marketplace Release Procedure

1. Publish a `vX.Y.Z` tag from `main`.
2. Read the immutable OCI index digest from the successful GitHub Actions release
   artifact.
3. Copy this directory to
   `plugins/nextmoca-runtime-defense/X.Y.Z/` in the
   [Tyk Community Marketplace](https://github.com/TykTechnologies/tyk-ai-studio-plugins-ce)
   fork.
4. Rename `manifest.yaml.tmpl` to `manifest.yaml` and replace every
   `{{VERSION}}`, `{{DIGEST}}`, and `{{RELEASED_AT}}` placeholder.
5. Open a Marketplace pull request. Do not edit its generated `index.yaml`.

## License

The plugin source is licensed under Apache-2.0. Next Moca and Needlepath names
and logos are trademarks of Next Moca; no trademark license is granted.
