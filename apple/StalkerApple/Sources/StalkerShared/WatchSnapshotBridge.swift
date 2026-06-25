import Foundation

#if canImport(WatchConnectivity)
import WatchConnectivity

@MainActor
public final class WatchSnapshotBridge: NSObject, WCSessionDelegate {
    public static let shared = WatchSnapshotBridge()

    public func activate() {
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    public func send(snapshot: SyncSnapshot) {
        guard WCSession.isSupported(), WCSession.default.activationState == .activated else { return }
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
