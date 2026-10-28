# Changelog

## [{{VERSION}}] - {{RELEASED_AT}}

### Added

- Initial Community Marketplace release of Next Moca Runtime Defense for AI
  Agents.
- Digest-pinned, signed OCI image for `linux/amd64` and `linux/arm64`.
- Request-side selection, scan/quarantine, enforce-mode blocking, and
  content-free gateway receipts through Tyk AI Studio's `post_auth` hook.
- Oversized eligible messages are autochunked by default using the hosted
  structure-aware chunker. The plugin maps selected child excerpts in source
  order back into their original OpenAI-compatible message before delivery.
