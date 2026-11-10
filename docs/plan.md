# Tablet gateway implementation plan

Goal: on-demand Android screen access and exclusive browser control on edge-2.
Architecture: one Go service, pinned scrcpy 3.3.3 server over ADB forward, H.264 passthrough to Pion WebRTC. Authenticated HTTP signaling; per-peer control DataChannel. No audio. Empty tablet inventory is valid and starts no device processes.

- [ ] Test lifecycle: shared capture, last viewer cleanup, failed start cleanup.
- [ ] Implement bounded scrcpy framing, control messages and ADB process ownership.
- [ ] Implement authenticated signaling, bounded negotiation lifetime, exclusive control and browser viewer.
- [ ] Package pinned server and binary; CI tests and GHCR image build.
- [ ] Add edge-2 DSL plan, manifests, renderer and tests in sokol-scada.
- [ ] Run tests, build, inspect changes, publish repository.

Validation limits: real tablet codec, ADB authorization after reboot, LAN ICE reachability and live edge-2 deployment require site validation. Do not invent tablet addresses or credentials.
