# Xcode setup

Xcode 26.4 can open this package directly:

```bash
xed apple/StalkerApple
```

Use the package targets as source modules, then create the signed app targets in
Xcode so Apple capabilities are attached to real bundle identifiers.

## Required targets

Create these native targets in one Xcode project:

- macOS App: `StalkerMacApp`
- iOS App: `StalkerPhoneApp`
- watchOS App: `StalkerWatchApp`
- Widget Extension: `StalkerWidgetsExtension`

Add the Swift package at `apple/StalkerApple` to the project and link:

- `StalkerMac` into the macOS app target.
- `StalkerPhone` and `StalkerShared` into the iOS app target.
- `StalkerWatch` and `StalkerShared` into the watchOS app target.
- `StalkerWidgets` and `StalkerShared` into the Widget extension target.

## Bundle identifiers

Use one Apple Developer team and bundle identifiers under one prefix, for
example:

- `com.mohammad-safakhou.stalker.mac`
- `com.mohammad-safakhou.stalker.ios`
- `com.mohammad-safakhou.stalker.watch`
- `com.mohammad-safakhou.stalker.widgets`

## Capabilities

Enable these capabilities in Xcode on each relevant target:

- iCloud, CloudKit: macOS app, iOS app, watchOS app, Widget extension
- CloudKit container: `iCloud.com.mohammad-safakhou.stalker`
- App Groups if later sharing local widget cache between the iOS app and widget
- Background Modes only if a target later does periodic background refresh

Keep the CloudKit container private-database only. The sync models intentionally
exclude raw request bodies, response bodies, previews, and authorization
headers.

## CloudKit schema

Use `CloudKitSchema.md` as the record schema checklist. In development mode,
run the macOS/iOS app once after signing and CloudKit capability setup so the
records can be created from the app. Then promote the schema in CloudKit
Dashboard before TestFlight builds.

## First local run

1. Install the Go proxy as a background runner:

   ```bash
   stalker install --runner launchd --service codex
   ```

2. Confirm it is running:

   ```bash
   stalker runner status
   curl http://127.0.0.1:18080/api/v1/sync/snapshot
   curl http://127.0.0.1:18081/api/v1/sync/snapshot
   ```

3. In Xcode, select your team for all targets.
4. Build and run the macOS app.
5. Build and run the iOS app on the same iCloud account.
6. Pair and run the watchOS app from the iOS scheme.

The iOS app prefers Bonjour-discovered local sync when the Mac is reachable and
falls back to CloudKit snapshots when remote.
