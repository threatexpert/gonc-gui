# Receiver Endpoint Direct HTTP Design

## Goal

Ensure file-receive HTTP traffic reaches the local forwarded endpoint directly, even when Android or the desktop operating environment has an HTTP proxy configured. A proxy must never receive file-list, download, resume, repair, or manifest requests intended for the receiver endpoint.

## Root cause

Android `HttpReceiver` opens every URL with the no-argument `URL.openConnection()`. That overload consults the platform proxy selector, so an Android system proxy can capture requests for `127.0.0.1` and send them to a remote proxy instead of the local endpoint.

The desktop receiver similarly uses Go's default HTTP clients. Go currently bypasses loopback hosts in its environment-proxy implementation, so the observed Android failure is not normally reproduced on desktop. However, the receiver code does not explicitly own this invariant and could become proxy-dependent through a different environment or future transport customization.

## Direct-connection boundary

- Treat the HTTP endpoint exposed by an established receive connection as an internal transport endpoint, not as ordinary web traffic.
- Every request issued by the receive subsystem must use an explicit no-proxy connection.
- This includes directory listing, full-file download, retry and resume requests, range repair, and BLAKE3 manifest retrieval.
- Redirects followed by the receive HTTP client remain on the same direct transport and must not regain system proxy behavior.
- The rule applies for the lifetime of the receive request regardless of connection-state changes elsewhere in the UI.

## Android design

- Centralize direct connection creation in `HttpReceiver`'s existing HTTP-open helper.
- Open URLs with `Proxy.NO_PROXY`, then retain the existing connect and read timeouts.
- Do not change request headers, retry behavior, range handling, decompression, file writes, or endpoint resolution.
- Do not change `AndroidUpdateChecker`; update checks are external web traffic and should continue following the user's system proxy configuration.

## Desktop design

- Give `internal/httpdownload` a dedicated reusable HTTP client for receiver traffic.
- Clone Go's default transport to preserve its dialer, connection pooling, TLS, HTTP/2, and timeout defaults, then set its proxy function to `nil`.
- Use this client for both remote file listing and all download workers, including manifest and range requests.
- Do not modify `http.DefaultTransport`, `http.DefaultClient`, environment variables, or application-wide proxy configuration.
- Do not change the update-check client; external update requests continue following normal proxy behavior.

## Safety and compatibility

- The change is local to the file-receive subsystem and cannot disable proxies for unrelated application networking.
- A dedicated transport avoids global mutable state and is safe for concurrent download workers.
- Existing endpoint URLs, authentication semantics, redirects, retry timing, and transfer concurrency remain unchanged.
- Explicit direct transport makes desktop behavior deterministic instead of relying on Go's special-case loopback bypass.

## Verification

- Add an Android unit test that observes connection creation and confirms `Proxy.NO_PROXY` is supplied while existing timeouts remain configured.
- Add desktop tests confirming the receiver client has no proxy function and can perform receiver requests through its direct transport.
- Confirm update-check code still uses its separate normal HTTP client.
- Run focused Android receiver tests, focused Go HTTP-download tests, the full Go test suite, and the Android unit-test suite.

