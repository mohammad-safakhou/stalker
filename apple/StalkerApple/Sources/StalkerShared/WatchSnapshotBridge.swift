import Foundation

#if canImport(WatchConnectivity)
@preconcurrency import WatchConnectivity

public final class WatchSnapshotBridge: NSObject, @preconcurrency WCSessionDelegate, @unchecked Sendable {
    public static let shared = WatchSnapshotBridge()

    public func activate() {
        guard WCSession.isSupported() else { return }
        #if os(iOS)
        guard WCSession.default.isPaired, WCSession.default.isWatchAppInstalled else { return }
        #endif
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    public func send(snapshot: SyncSnapshot) {
        guard WCSession.isSupported(), WCSession.default.activationState == .activated else { return }
        #if os(iOS)
        guard WCSession.default.isPaired, WCSession.default.isWatchAppInstalled else { return }
        #endif
        guard let data = try? StalkerCoders.encoder().encode(snapshot) else { return }
        try? WCSession.default.updateApplicationContext(["snapshot": data])
    }

    public func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {}

    #if os(iOS)
    public func sessionDidBecomeInactive(_ session: WCSession) {}
    public func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
    #endif
}
#else
@MainActor
public final class WatchSnapshotBridge {
    public static let shared = WatchSnapshotBridge()
    public func activate() {}
    public func send(snapshot: SyncSnapshot) {}
}
#endif
