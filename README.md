# tablet-gateway

On-demand Android screen streaming and browser control. One scrcpy capture per tablet, shared by viewers. The last viewer leaving stops capture; browser heartbeat expires after 15 seconds. No video capture, encoding or device video traffic while idle. ADB may remain connected.

## Run

Requires Android with authorized ADB debugging, network access to its ADB endpoint (or USB), Go 1.24+ and the **scrcpy 3.3.3** server. The Docker image includes ADB and that server. No desktop environment or FFmpeg required. H.264 is encoded on Android and packetized by Pion; audio is disabled.

Create `/etc/tablet-gateway/tablets.json`:

```json
[{"id":"hmi-1","label":"HMI 1","serial":"TABLET_IP:5555"}]
```

`serial` is an actual ADB device serial or authorized TCP endpoint. An empty list `[]` is supported. No automatic scanning or enabling debugging. Wireless pairing and authorization must be completed beforehand. Reboot persistence depends on the tablet's Android configuration.

```sh
export TABLET_GATEWAY_TOKEN='replace-with-a-random-secret-at-least-24-characters'
export TABLET_GATEWAY_CONFIG=/etc/tablet-gateway/tablets.json
export SCRCPY_SERVER=/path/to/scrcpy-server-v3.3.3
export WEBRTC_PUBLIC_IP=192.168.5.97
go run ./cmd/tablet-gateway
```

Open port 8080, enter the token, load tablets and open one. Select “Взять управление” to obtain exclusive control. Touch, dragging and Back are supported; audio, clipboard, arbitrary keyboard text and multi-touch are not implemented. Hidden browser tabs automatically close the stream. Tokens stay in browser memory, not localStorage or URLs.

Production: serve the viewer/API through an authenticated HTTPS reverse proxy. Do not publish ADB port 5037 or device debugging ports to users. The initial API uses a shared bearer token; integrate site identity/roles at the proxy before exposing it outside the trusted network. Health endpoint is public; device inventory and signaling require a token.

The service listens on `LISTEN_ADDR` (default `:8080`) and WebRTC UDP 50000–50100. `WEBRTC_PUBLIC_IP` advertises the reachable edge address. Use host networking or map this UDP range. LAN routing must allow browser-to-edge UDP. TURN is not configured by the executable in this first version; routed networks need a TURN/ICE configuration extension or direct VPN connectivity.

## API and library

- `GET /api/tablets`: safe tablet IDs/labels (not ADB endpoints).
- `POST /api/tablets/{id}/offer`: JSON RTCSessionDescription offer; returns complete answer with ICE candidates.
- `GET /healthz`: process health.
- WebRTC DataChannel `control`: JSON `ping`, `claim`, `release`, `touch` or `key` messages. See `gateway.Command` and `web/viewer.js`.

Go package `gateway` exports `NewManager`, `AndroidFactory`, `HTTP` and `Device`. The executable owns configuration, static viewer and graceful shutdown. Maximum 32 peers; negotiation expires after 20 seconds; slow viewers are disconnected rather than buffering unbounded video. Disconnection stops that peer immediately. Capture failures currently require reopening the viewer; they are not retried indefinitely.

## Validation

```sh
go test -race ./...
go vet ./...
go build ./cmd/tablet-gateway
```

Tests cover capture sharing/cleanup, exclusive control, framing bounds, auth, touch wire format and an actual local Pion offer/answer + DataChannel/disconnect cycle using a synthetic capture. A real Android device is required to validate MediaCodec profile support, protected screens, rotation, ADB reconnect after reboot and physical touch behavior. These are not implied by passing synthetic tests.

CI runs tests and builds a container. Main pushes publish `ghcr.io/yarkivaev/tablet-gateway:sha-COMMIT` and `latest`. Pin the commit tag in deployments. GHCR package visibility is separate from repository visibility: make the package public or configure a registry pull secret.

## Sokol integration

Deployment DSL lives in `sokol-scada/plant/tablet-gateway.yaml`, with renderer `bin/deploy-tablet-gateway.js`. It targets only edge-2. See the integration repository's `docs/runtime/tablet-gateway.md` for secrets and rollout steps.

## License

Apache-2.0. scrcpy is a separate Apache-2.0 project by Genymobile: https://github.com/Genymobile/scrcpy . Its server protocol is version-specific; keep client implementation and bundled server pinned together.
