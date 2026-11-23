# Gonc Nearby Device Pairing and File Transfer Design

**Status** Desktop implementation in progress; protocol not yet reviewed. See [Implementation notes](#implementation-notes-2026-11-22) for where the code differs from this draft.

The goal is fast file sharing between devices on the same local network. The sender selects files and picks a nearby device. On their first transfer, the users compare a six-digit number shown on both devices. On later transfers, Gonc authenticates the devices automatically and the recipient only needs to accept the file request.

The `gonc` repository contains the shared Go implementation. Desktop and Android use it through `goncembed` and `mobilegonc`. The application owns device identity, persistent pairing records, and user decisions. The CLI keeps its existing dependency graph.

## Discovery and device identity

The application generates a cryptographically random installation ID and a 16-byte identity resolving key (IRK) on first run and saves both. It supplies them and a user-editable nickname when starting discovery. The Go library can expose random generators for both and fills in the protocol version and listening port. The application keeps the same ID and IRK across restarts, IP changes, and nickname changes.

### Advertisements

Advertisements are sent to UDP port 19731 on multicast group `239.255.255.250`. This is the group gonc LAN discovery already uses, but a different port. Manual passphrase sessions bind 19730 through `-p2p-with-lan`, and Windows does not let a second socket bind the same port without extra socket options, so the separate port keeps the device list live while a manual session runs.

An advertisement contains a resolvable tag, the nickname, the protocol version, and the TCP pairing port. It does not contain the installation ID, so a device that moves between networks cannot be tracked by a stable identifier. The tag is `r || trunc16(HMAC-SHA256(IRK, encode("gonc-nearby-v1", "adv-tag", r)))` with a fresh 16-byte random `r`. The application regenerates `r` on each start and at least every 15 minutes. Paired devices hold the advertiser's IRK and resolve the tag by trying each stored IRK; anyone else sees an unlinkable value. This follows the resolvable private address scheme in Bluetooth LE. [Bluetooth LE privacy](https://www.bluetooth.com/wp-content/uploads/Files/Specification/HTML/Core-60/out/host/security-manager-specification.html) The nickname remains visible to everyone on the LAN, and the visibility setting says so.

The application advertises only while it is open and the user has **Visible to nearby devices** turned on. On Android it advertises only while the app is in the foreground. A device that is not visible can still list and send to visible devices.

### Candidates and verification

The receiver uses the packet source address to form a candidate endpoint. A tag that resolves with a stored IRK selects that saved pairing record. The endpoint earns trust only by proving possession of that record's key. The application runs this proof in the background over the pairing port as soon as a resolved candidate appears, and the device list shows verified paired devices differently from unverified ones. Distinct endpoints resolving to the same record remain separate candidates until verification succeeds. The same key authenticates the device after a nickname change.

Advertisements are unauthenticated, so anyone on the LAN can send them. The library bounds the candidate table (for example to 64 entries, evicting the oldest unverified candidate first), expires candidates that miss several advertisement intervals, and ignores malformed or oversized packets.

### Relation to existing LAN discovery

The existing LAN discovery in `gonc/easyp2p/lan.go` locates a peer for a session whose two endpoints already share a secret. Nearby advertisements have their own message format so an application can discover candidates before pairing. The new feature can reuse the existing interface enumeration and multicast socket code where practical. Those helpers are unexported today, so they move to, or are exported from, a low-level package that both `easyp2p` and the new package import.

## First pairing with numeric comparison

A is the device that initiates pairing and B is its intended peer. In the send workflow, tapping an unpaired device starts an application-scoped TCP pairing session. Each device independently generates an ephemeral X25519 key pair and a fresh 32-byte random nonce. The pairing context contains a protocol version, a fresh request ID, and the advertisement tags of A and B in role order. Installation IDs are not known before pairing because advertisements omit them; they are exchanged after confirmation, as described below. The exchange uses the commitment and numeric comparison pattern from Bluetooth. The wire format and key schedule below are a Gonc proposal and require cryptographic review before release. [Bluetooth Numeric Comparison](https://www.bluetooth.com/wp-content/uploads/Files/Specification/HTML/Core-60/out/en/br-edr-controller/security-specification.html) · [Short authentication strings and commitments](https://www.rfc-editor.org/rfc/rfc6189.html#section-7)

### Commit and reveal

Each side computes its commitment as `SHA256(encode("gonc-pair-v1", "commit", context, role, publicKey, nonce))`. All hashed or authenticated fields use a canonical, unambiguous binary encoding with fixed field order and explicit lengths where needed.

| Order | Direction | Message and receiver action |
| --- | --- | --- |
| 1 | A → B | Send the pairing context and A's commitment `C_A`. B checks the protocol version and that the target tag is one it currently advertises or advertised in the previous rotation. |
| 2 | B → A | Send B's commitment `C_B` for the same context. A records it before revealing its values. |
| 3 | A → B | Reveal `P_A` and `N_A`, A's public key and nonce. B recomputes and validates `C_A` before revealing its values. |
| 4 | B → A | Reveal `P_B` and `N_B`, B's public key and nonce. A recomputes and validates `C_B`. |

Both commitments must be sent before either side reveals. Without them, an attacker between A and B could complete its exchange with B first, then try about a million ephemeral keys on A's side within seconds until the two six-digit numbers match. With them, each side's values are fixed before it learns the other side's nonce, so an attacker succeeds with probability about 10⁻⁶ per attempt. Implementations must not remove or reorder this step.

Both sides keep their ephemeral private keys local. They stop the exchange on a malformed message, unexpected state transition, mismatched commitment, or invalid X25519 result, including an all-zero shared secret. They exchange file metadata only after pairing and transfer authentication succeed. [X25519 shared-secret calculation](https://www.rfc-editor.org/rfc/rfc7748.html#section-6.1)

### Derive the number and pairing key

A computes `Z = X25519(private_A, P_B)` and B computes `Z = X25519(private_B, P_A)`. Both compute the same transcript hash `T` over the context, commitments, and reveals in the order shown below. They derive separate values from the exchange for the six-digit short authentication string (SAS), the stored pairing key, and the two confirmation messages. [HKDF](https://www.rfc-editor.org/rfc/rfc5869.html#section-3)

```text
T = SHA256(encode(context, C_A, C_B, P_A, N_A, P_B, N_B))
PRK = HKDF-Extract-SHA256(salt = 32 zero bytes, IKM = Z)
derive(label, length) = HKDF-Expand-SHA256(
    PRK, encode("gonc-pair-v1", label, T), length
)

SAS = zero_pad_6(uint64_big_endian(derive("sas", 8)) mod 1000000)
K_AB = derive("pair-key", 32)
confirm_A = derive("confirm-A", 32)
confirm_B = derive("confirm-B", 32)
identity_A = derive("identity-A", 32)
identity_B = derive("identity-B", 32)
pairID = SHA256(encode("gonc-pair-v1", "pair-id", T))
```

Each device displays its number locally, including leading zeros. The users compare the numbers on the two intended screens and confirm on both devices. Each device sends a role-specific HMAC over `encode("accept", role, T)` after its local confirmation. After it validates the peer's confirmation, each device sends its installation ID and IRK encrypted with AES-256-GCM under its role's identity key, with `T` as associated data. Neither side releases its identity before both users have confirmed. A device reports pairing success only after its user confirms, it validates the peer's confirmation, and it decrypts the peer's identity. The application then saves the peer's installation ID, IRK, `pairID`, and `K_AB` in protected local storage. `pairID` is a local record identifier and is never sent on the network. The six digits are only for human comparison and stay out of the file-transfer password derivation.

Cancellation, mismatch, and timeout end the attempt. New attempts use fresh ephemeral keys, nonces, and request IDs. Bound concurrent pairing attempts and retries so one user prompt corresponds to one exchange and an attacker cannot accumulate guesses through silent retries. A device shows at most one pairing prompt at a time and rate-limits pairing requests from each source address; extra requests are refused as busy without a prompt, so other devices on the LAN cannot flood the user with prompts. The review must assess the six-digit security bound for this exact protocol, including concurrent and interrupted attempts.

On the first transfer between two devices, B therefore sees two prompts: the number comparison, then the file request. The interface presents them as consecutive steps of one flow rather than as unrelated dialogs.

## Later authentication and file transfer

Each pairing record is specific to a device pair. If A also pairs with C, that exchange creates an independent `K_AC`. The same saved relationship supports transfers in either direction.

1. When A selects a paired B, the application loads B's record and calls the library with that credential. A opens a TCP connection to B's advertised pairing port and sends a transfer request containing a freshly generated resolvable tag for its own IRK, a fresh transfer ID, and a fresh nonce. The request carries no stable identifier. B resolves the tag against its stored IRKs to find A's record, loads its copy of the credential, and returns its own fresh nonce. If no stored IRK resolves the tag, or B is busy, B refuses the request without showing a prompt.
2. Each side derives role-specific proof keys and a temporary Gonc passphrase from `K_AB` using HKDF-SHA256. The derivations use distinct labels and bind the protocol version, `pairID`, transfer ID, ordered device roles and installation IDs, and both nonces. Each side verifies the other's HMAC over that transfer context before releasing its temporary passphrase to the transfer integration. Accept proof messages only for a live request in the expected state. The passphrase is encoded as `nb1-` followed by the lowercase unpadded base32 of a 32-byte derived value, so it always contains letters and digits and passes the application's existing strength check (`isWeakPassword` in `gonc-gui/app.go`).
3. The applications start the existing Gonc sender and receiver with the derived passphrase over the transport described below. The current mutually authenticated file connection carries the file list and content. B displays the request once it can read the file list. The user presses **Accept** to begin saving the offered files through the existing downloader. The offered list is fixed for the life of a Nearby session; unlike manual sessions, changes to A's selection are not synchronized to B.

### Transfer transport

A Nearby transfer never contacts MQTT signaling servers or STUN servers, and it does not use passphrase-keyed multicast discovery. Both devices are on the same LAN and A already holds an authenticated TCP connection to B. Transfers must work on a network with no internet access, and must not publish anything to public brokers.

The existing sender and receiver currently start through `StartP2PShareSourceWithOptions` and `StartP2PReceiveWithOptions` with `P2PWithLANMode`, which runs public signaling and LAN discovery together. `goncembed` gains sender and receiver entry points that take an already established connection, or the peer endpoint from the request, and go straight to the existing passphrase-derived TLS 1.3 negotiation and mux. Reusing gonc's `-lan` mode instead is not suitable: every such session binds UDP 19730, so two concurrent Nearby sessions, or one Nearby and one manual session, would conflict.

### Concurrent sessions

The desktop backend keeps one sender runner, one receiver runner, and one download for manual passphrase sessions. Nearby transfers run in their own sessions, each with its own runner and downloader, and never stop or replace a manual session. The first release allows one incoming and one outgoing Nearby transfer at a time. A device that is already receiving a Nearby transfer answers a new request as busy, and the sender shows "Device is busy". Android follows the same limits.

### Failures and stale records

The first transfer continues through the same authenticated transfer path once the applications have saved the new pairing. If authentication fails, show that the device could not be verified and keep the saved record. If B has lost its credential after a reinstall, B has a new installation ID and IRK, so A sees an unpaired device with the old nickname, and A's saved record for B no longer resolves any advertisement. A and B need to compare numbers again. Replace the saved credential only after a new pairing explicitly initiated by the user succeeds. The device list shows a saved record whose device has not been verified for a long time as inactive, and the user can remove any record with **Forget device**. Report a request that receives no response before its deadline as a timeout.

## Secret storage

The installation ID, own IRK, and each peer's IRK and `K_AB` are secrets of the application. The existing VPN profile protection is not sufficient on its own: it uses DPAPI on Windows, but on other platforms (`internal/vpnprofile/dpapi_other.go`) it stores bytes unchanged.

| Platform | Storage |
| --- | --- |
| Windows | DPAPI, current-user scope, as the VPN profiles already use |
| macOS | Keychain item owned by the application |
| Linux | Secret Service (for example GNOME Keyring or KWallet) through D-Bus. If no Secret Service is available, use a file with mode 0600 in the user configuration directory and state in settings that pairing keys are not protected by the operating system. |
| Android | AES-GCM key held in the Android Keystore wrapping the records in app-private storage |

Losing this storage has the same effect as a reinstall: devices need to compare numbers again.

## Changes to the Gonc repository

Put discovery, numeric comparison, and transfer request authentication in a dedicated Go package such as `internal/nearby`. The caller starts and stops discovery and supplies identity or credentials. The package holds only live session state and returns successful pairing results to the caller. The application owns installation identity, nickname settings, peer records, secure storage, and replacement or deletion of credentials.

| Package or application | Integration |
| --- | --- |
| `goncembed` | Expose APIs for starting and stopping nearby discovery, starting or responding to pairing, submitting local numeric confirmation, and authenticating transfer requests. Deliver device, pairing, and transfer events through callbacks. Add sender and receiver entry points that run over a supplied connection or endpoint without MQTT, STUN, or multicast discovery. |
| `mobilegonc` | Wrap the same APIs with gomobile-friendly session handles and a small callback interface. Pass events and decisions through the Android bridge. The Android app owns the Wi-Fi multicast lock and app lifecycle. |
| Desktop `gonc-gui` | Call `goncembed` from the Go backend. Keep pairing secrets in the platform storage listed above and pass the device list, verification state, comparison number, and request state to the frontend. Run Nearby transfers in their own sessions, separate from the manual sender, receiver, and download. Add the visibility setting and **Forget device**. |
| Android `gonc-gui` | Rebuild `mobilegonc.aar` using `android/update-mobilegonc-aar.bat`. Extend `MobileGoncBridge` to surface the same user actions and pass credentials from app-owned storage. |

The import direction is Desktop → `goncembed` → `internal/nearby` and Android → `mobilegonc` → `goncembed` → `internal/nearby`. The CLI root imports `apps`. Keep the new protocol out of the import graph rooted at the CLI, including `apps`, `easyp2p`, and other common packages. If multicast helpers are shared, keep their dependency direction toward network primitives. Add a check modeled on `main_dependency_test.go` using `go list -deps .`, and compare CLI executable size under identical build settings. The CLI's existing manual passphrase workflow continues through its current entry points.

## Review and verification

- Review transcript encoding, role and identity binding, commitment ordering, confirmation, the post-confirmation identity exchange, resolvable tags, and replay handling before fixing the wire format. Write out the exact `encode(...)` byte layout first. Prepare deterministic test vectors for advertisement tags, the comparison number, pairing key, identity encryption, transfer proofs, and the derived passphrase.
- Verify desktop and Android can pair in both directions. Compare the numbers once, accept the first transfer, restart both apps, and confirm that a later transfer requires only recipient acceptance.
- Exercise two endpoints claiming the same nickname or replaying the same advertisement tag, failed confirmation, cancellation, timeouts, and lost credentials. A failed check must preserve any prior pairing record and prevent the release of file metadata and installation identity.
- Pair and transfer on a router with no internet access, and capture traffic to confirm a Nearby transfer sends nothing to MQTT or STUN servers and nothing to UDP 19730.
- Run a manual passphrase send and a Nearby receive at the same time, and send two Nearby requests to one device at once; the second must be refused as busy without disturbing the first.
- Flood a device with advertisements and pairing requests from many source addresses; the candidate table must stay bounded and the user must see at most one pairing prompt.
- Confirm advertisement tags change on restart and after each rotation interval, still resolve for paired devices, and contain no installation ID.
- Confirm pairing records are stored through the platform storage on Windows, macOS, Linux, and Android, and that **Forget device** removes them.
- Verify that the desktop and Android builds use the same Go protocol and that the CLI dependency list and executable size do not grow because of the feature.

## Implementation notes (2026-11-22)

The first implementation lives on the `feat/nearby` branches of `gonc` and `gonc-gui`. It follows the design above except where noted here.

### Differences from the draft

- **Package location.** The protocol is the public package `github.com/threatexpert/gonc/v2/nearby`, following the precedent of `tunengine`, instead of `internal/nearby` behind `goncembed`. The desktop imports it directly. `main_dependency_test.go` now fails if the CLI links it.
- **Session authentication.** Steps 1 and 2 of [Later authentication and file transfer](#later-authentication-and-file-transfer) are implemented with TLS rather than hand-written HMAC proofs. The initiator sends a fresh resolvable tag, a 32-byte nonce, and its listening port; the responder resolves the tag and returns its own nonce. Both derive, from `K_AB` with HKDF-SHA256 (salt `pairID`) and a context binding the protocol version, `pairID`, the ordered installation IDs and both nonces, two independent values: a control key and the transfer passphrase (`nb1-` + base32). The control connection then runs TLS 1.3 with mutual authentication, both certificates derived from the control key exactly as `gonc -psk` does, using the existing `secure` package. A failed handshake is reported as "could not be verified". Offers, accept, decline, cancel and finish travel inside that TLS connection. Fresh nonces give every session its own passphrase, so no separate transfer ID enters the derivation.
- **Transfer transport.** `goncembed.StartDirectShareSource` and `StartDirectReceive` run the existing HTTP file share as `gonc -tls -tls13 -psk <passphrase>` over a direct TCP connection: the sender listens on the address of the interface that reaches the recipient, and the recipient dials it. No MQTT, STUN or multicast discovery is involved, and port 19730 is not used. The share reports readiness through `Callback.Ready` once it accepts connections, and the offer is sent only after that. Direct receives choose their own loopback HTTP port; gonc's process-wide reuse of the previous session's port otherwise stalled back-to-back receives for about 13 seconds.
- **Multicast helpers.** `nearby` has its own small multicast code (one socket bound to UDP 19731 with `SO_REUSEADDR` that both sends and receives) instead of refactoring `easyp2p`, so the CLI's LAN discovery code is untouched.
- **Add by address.** A TCP `hello` request returns a visible device's advertisement, for networks where multicast does not reach the device. The desktop saves these addresses and probes them again at startup. After a successful pairing or an authenticated session, the responder also learns the initiator's listening port, so a device added on one side only can still send back.
- **Receiving.** Files are downloaded into a staging folder inside the save folder, then each top-level item is moved into place under a name that does not overwrite anything (`name (1).ext`). The staging folder is removed on success, failure, decline and cancel.
- **Listener cancellation.** `startTCPListener` in `gonc/apps/nc.go` now closes its listener when the embedded session's context is cancelled. Before this change, stopping a direct share session hung.

### Status

Implemented and covered by automated tests:

- Commit-and-reveal pairing, including a matching code, rejection by either side, and a reveal that does not match its commitment.
- Resolvable tags, the canonical encoding, and per-session key derivation.
- Add by address, pairing, background verification, offer, accept, decline, finish, cancel, forget, rejection of mismatched pairing keys, and one-sided addresses becoming mutual after pairing.
- Direct share and receive, rejection of a wrong key on both sides, validation, and back-to-back receives.
- Desktop end to end, with two managers in one process: pair, send a file and a folder, accept, receive the same names again without overwriting, decline.
- DPAPI-encrypted store round trip, and refusal to replace an unreadable store.

Also checked by hand in the desktop UI (`wails dev`) against a headless second device on the same computer: add by address, pairing dialog and code, persistence of the pairing across restarts of both sides, sending text, the incoming-file dialog, and decline.

Not yet done or not verified:

- **Multicast discovery on a real network.** UDP was blocked on the development machine, even plain loopback, so automatic discovery has not been observed working. All other paths were tested over TCP.
- **The race detector.** No C compiler was available, so `-race` was not run.
- **macOS Keychain and Linux Secret Service storage.** Those platforms currently use a 0600 file, and the UI says so.
- **macOS and Linux desktop builds.** Not built on this branch.
- **Android.** No `mobilegonc` wrapper or UI yet.
- **Cryptographic review.** Still required before release, now covering the TLS-based session step as well.
- **Cross-network tracking.** The device nickname is still visible on the LAN.
- **A send during a manual passphrase share.** The Nearby page reuses the Send page's content list, so items added while a manual passphrase share is running are also synchronized to that share's receiver.

## Firewall traversal (2026-11-23)

Testing on a real network showed that Nearby failed when one device's firewall blocked incoming connections, although passphrase LAN mode connected quickly between the same devices. There were two causes. A firewalled device cannot receive multicast it did not ask for; it only receives unicast replies to its own multicast, and Nearby asked for replies only during the first five seconds, so the firewalled device soon lost every other device. And every Nearby connection was dialed in one direction: pairing and sessions dialed the responder's TCP port, and the receiver dialed the sender's share port, so any leg pointing at the firewalled device was dropped.

Passphrase LAN mode works because both sides beacon and answer every beacon by unicast to its source, which a stateful firewall treats as a response, and then both sides dial each other from their punch ports at the same time (`easyp2p/lan.go`, `Auto_P2P_TCP_NAT_Traversal`). Nearby now uses the same approach:

- **Discovery.** Every advertisement, and every bare query from a device that is not visible, asks for a reply, and visible devices answer each one by unicast. Devices advertise every 2 seconds (every second for 5 seconds after start, a refresh or becoming visible).
- **Connections.** Pairing, sessions and background verification race two paths and keep the first that succeeds:
  - **Direct.** A dial to the advertised TCP port.
  - **Rendezvous.** It starts 0.7 seconds later, or as soon as the direct dial fails. The initiator sends a knock (target tag, its own tag, a fresh nonce, and for sessions an HMAC under a key derived from `K_AB`). The knock goes by multicast, and by unicast to the source of the target's advertisements, repeated every second and immediately after each new advertisement from the target so that it passes the target's firewall as a reply. Both sides then run gonc's LAN discovery and TCP traversal with a key made for that knock: derived from `K_AB` for sessions, and public for a first pairing, which the numeric comparison protects. The resulting connection is served exactly like an accepted one, so the pairing and session protocols are unchanged.
  - **Limits.** Knocks are deduplicated by nonce; pairing knocks share the pairing rate limit; at most four rendezvous run at once.
- **Transfers.** The sender serves the files both on a direct listener and through a LAN-only gonc P2P session with the transfer passphrase, and the offer says so. The recipient tries the direct listener for 3 seconds, then switches to the LAN-only session. `apps.RunOptions.LANOnly` (and `goncembed.P2POptions.LANOnly`) configures a `-p2p` session like the LAN path of `-p2p-with-lan`, but active on both sides and with MQTT hello and wait disabled, so no MQTT broker or STUN server is contacted.
- **Concurrent LAN sessions.** gonc's LAN discovery now binds the shared multicast port with address reuse. Several LAN sessions (a passphrase transfer, a Nearby rendezvous and a Nearby transfer) can therefore run at once; each reply is also multicast, so every session sharing the port receives it. Before this change a second session failed to bind UDP 19730.
- **Send from the listening socket.** Both Nearby discovery and gonc LAN discovery send multicast from the socket bound to the discovery port, never from a separate ephemeral socket. Windows Defender Firewall admits a peer's multicast and unicast answers only on a socket that has itself sent to the group. A first build sent Nearby adverts from an ephemeral port, and two firewalled Windows machines never saw each other, while `gonc -lan` (which sends 19730 → 19730) worked on the same pair. A packet capture showed the difference.
- **Diagnostics.** The Nearby page has a Connection log with discovery events, knocks, which path won, and the LAN sessions' own gonc log lines.

Verification so far is limited to what works without UDP on the development machine: knock signatures, rendezvous keys, deduplication, LAN-only configuration (no MQTT flags), two LAN sessions binding at once, and the existing direct-path tests. The `easyp2p` package fails the same UDP-dependent tests before and after the change. The rendezvous and LAN-only transfer paths have not been run on a real network yet.

## Field-test fixes (2026-11-23)

Testing on two Windows machines behind Windows Defender Firewall found these problems:

- **Pairing ignored after a cancel.** The device being paired with allows six pairing requests a minute from each address. A knock is repeated every second until it is answered, and the limit was counted before repeats were filtered out, so one attempt used up the allowance and later attempts were silently dropped for a minute. Repeats are now filtered first. A device that is already showing a pairing prompt still meets the knock and answers "busy", so the other side fails at once instead of waiting 25 seconds.
- **Saved addresses used up the connection limit.** Addresses entered by hand or learned after pairing were checked over TCP every 2 seconds. That is 30 connections a minute, exactly the per-address limit, so pairing and transfer connections were refused. Checks now run every 5 seconds and are skipped while multicast keeps the device current, and the limit is 120 a minute.
- **Duplicate devices.** A device that restarts, or rotates its advertisement tag, appeared twice until the old entry expired after 16 seconds. An unpaired entry is now replaced at once when a new tag arrives from the same address, or from the same host with the same name. An unpaired advertisement never replaces a paired device.
- **Slow receive prompt.** The recipient connected to the sender's share and read the file list before asking the user. Offers now carry a summary (up to 40 top-level names, the file count and the total size), so the prompt appears as soon as the offer arrives and the connection is made while the user decides. The recipient still reads the real file list before downloading.
- **Extra rendezvous after pairing.** A newly paired device was verified again in the background, which on a firewalled network meant another LAN rendezvous at the same moment as the first transfer. Pairing and each authenticated session now count as verification. A device whose direct path failed recently is knocked on at once, without the 700 ms head start.
- **Cancel needed two clicks** on an accepted incoming transfer, because the first click was taken as an answer to the request. Answered requests no longer accept an answer.
- **"Not verified" label.** Testers read "Paired · Not verified yet" as a failed pairing. The device list now shows only **Paired** or **Not paired**. Verification still runs and still decides which candidates are evicted first, but it is no longer shown, since a failed proof already surfaces as an error when the user sends.

## Quick send without pairing (2026-11-23)

The sender may skip the number comparison for files that do not need protection, for example to send the same public file to several devices. The devices do not become paired.

- **Protocol.** The first message on the connection is `quick`, carrying the sender's advertisement tag, the target's tag, an ephemeral X25519 public key, a 32-byte nonce, and the sender's name, type and port. The recipient answers `quick-ok` with its own public key and nonce, or `quick-off` when quick receiving is turned off. Both derive, with HKDF-SHA256 over the shared secret and a context binding the protocol version, both tags, both public keys and both nonces, a control key and a transfer passphrase. From there the session is the same as a paired one: TLS 1.3 with certificates derived from the control key, then the offer.
- **What it protects.** The exchange is unauthenticated. It keeps the files from a device that only listens, but a device in the middle can complete one exchange with each side and read or replace the files. The recipient's prompt says the sender is not paired and that anyone on the network could be using that name. Quick offers have no peer ID and never create a pairing record.
- **Recipient control.** Quick receiving is on by default and can be turned off under **This device** (`RefuseQuick` in the settings file). Advertisements and hello answers carry `qr` when it is on, so senders only offer **Send** to devices that accept it. Quick requests are limited to ten a minute per address, and share the one-prompt-at-a-time rule with paired offers.
- **Firewalls.** A `quick` knock starts the LAN rendezvous for a firewalled recipient, with a public rendezvous key like pairing knocks. It is ignored when quick receiving is off and counts against the same per-address limit.

## Add-by-address removed from the UI (2026-11-23)

The "Can't see a device?" form and the address list under **This device** are gone. Where the other device is not listed, the Nearby page points to **Send Files** and **Receive Files**, which also work between networks. The library keeps `AddAddress` and the TCP hello, and addresses saved earlier are still checked, but the app no longer offers a way to add one.

## Discovery only while the Nearby page is open (2026-11-23)

- `nearby.Config.Paused` and `Service.SetDiscovering` stop announcing, answering queries, background verification and address probes. The service still listens, accepts connections and answers knocks, so transfers in progress and a device that already listed this one keep working. Turning discovery on announces at once and starts the fast phase. Device entries still expire as before.
- The app starts the service paused, and the frontend calls `NearbySetDiscovering` whenever the Nearby page opens or closes. The manager remembers the request if it arrives before the service has started.
- Send Files is the first page on a first run (`gonc.nearbyActive` defaults to off). Send Files has a "Send to nearby devices" link under the passphrase, and Receive Files has a "Receive from Nearby" button beside the start button.
- The Nearby page opens with a "Ready to receive as <name>" banner (or "Hidden" with a button to show the device), so it no longer reads as send-only and the name the sender asks about is in view. Its Rename link focuses the name field under This device.
- With nothing added, the Nearby page shows a one-line "Want to send something?" prompt with a Choose files button in place of the content card; the prompt also accepts dropped files. The full card returns as soon as something is added. Send Files keeps the full card.

## Android (2026-11-23)

- No separate screen. Send Files and Receive Files have a **Passphrase | Nearby** switch where the passphrase settings were (`NearbyController.modeSwitch`). In Nearby mode the Send passphrase area becomes the device list (Pair, Send, progress, Cancel) and the Receive passphrase area becomes the ready-to-receive banner (Rename, Show this device), the accept-from-unpaired switch and the paired devices (Forget). The choice is remembered per screen.
- The Go side is `nearbygo.NearbyNode` in gonc-gui's `android/nearbygo`, a gomobile-friendly wrapper around the desktop `nearbyapp.Manager`, so both apps share one implementation and gonc carries nothing Android-specific. The manager gained `SendSource` (send any `httpfileshare.FileSource`), `ExternalDownload` with `FinishReceive` (the app downloads from the reported endpoint), and store options; `nearbystore.Options` seals the file with an app-supplied key instead of the OS protection. Events reach Java as JSON on one listener (`state`, `pairing`, `transfer`, `log`).
- `mobilegonc.aar` is now built from gonc-gui (`android/update-mobilegonc-aar.bat` binds gonc's `mobilegonc` and `gonc-gui/android/nearbygo` together), so gonc-gui's `go.mod` records the gonc version inside the AAR.
- Receiving reuses the Receive screen: after Accept, Go connects to the sender (direct first, then LAN-only) and reports `ready` with the local endpoint; `ReceiveController.beginNearbyReceive` treats it as a connected session, lists and downloads everything with `HttpReceiver`, and reports the result with `FinishReceive`. Cancel there cancels on both devices.
- Discovery runs only while the app is in the foreground and the visible screen is in Nearby mode, with a Wi-Fi multicast lock held for that time. Running sends keep the foreground service up.
- Identity and pairings live in `noBackupFilesDir/nearby.bin`, sealed with AES-GCM under a 32-byte key that is stored wrapped by an Android Keystore key (`NearbyKey`). If the wrapped key cannot be unwrapped (for example preferences restored on another device), a new identity is created.
- Later cleanup discussed: move the other app-only wrappers (`mobilegonc`, `tunengine`, possibly `goncembed` with a documented `apps` embedding API) from gonc into gonc-gui. Needs agreement on the gonc side.
