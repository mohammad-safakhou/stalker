# Stalker Apple

Native Apple surfaces for Stalker.

## Targets

- `StalkerShared`: Codable sync models, localhost API client, live SSE client, CloudKit writer, WatchConnectivity bridge, shared SwiftUI dashboard.
- `StalkerMac`: SwiftUI menu bar app that reads from the Go proxy and can push snapshots to CloudKit.
- `StalkerPhone`: iPhone SwiftUI dashboard source.
- `StalkerWatch`: Apple Watch dashboard source.
- `StalkerWidgets`: WidgetKit complication source.

The first CloudKit container identifier is:

```text
iCloud.com.mohammad-safakhou.stalker
```

The Go proxy remains the capture engine. Native apps consume only aggregate sync data from:

```text
GET /api/v1/sync/snapshot
GET /api/v1/sync/stream
```

Raw prompts, responses, previews, headers, and body paths are intentionally not part of the sync contract.

## Local Checks

```bash
swift test
swift build --target StalkerMac
```

App Store/TestFlight packaging still needs an Xcode project with your Apple Developer team, app groups, CloudKit capability, and watch/widget embedding configured.

See `CloudKitSchema.md` for record types and idempotent record names.
