# Changelog

## [{{VERSION}}] - {{RELEASED_AT}}

### Added

- Initial Community Marketplace release of Next Moca Runtime Defense for AI
  Agents.
- Digest-pinned, Cosign key-pair-signed OCI artifact index for `linux/amd64`
  and `linux/arm64`. Each platform artifact has exactly one binary layer for
  Tyk AI Studio to execute as a Go plugin.
- Request-side selection, scan/quarantine, enforce-mode blocking, and
  content-free gateway receipts through Tyk AI Studio's `post_auth` hook.
- Oversized eligible messages are autochunked by default using the hosted
  structure-aware chunker. The plugin maps selected child excerpts in source
  order back into their original OpenAI-compatible message before delivery.
- AI Studio `$SECRET/NAME` configuration support, optional literal-excerpt
  verification, and fail-open invalid startup configuration in `shadow` and
  `active` modes.
