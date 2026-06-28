import Combine
import Foundation

@MainActor
public final class StatsViewModel: ObservableObject {
    @Published public private(set) var snapshot: SyncSnapshot?
    @Published public private(set) var isLive = false
    @Published public private(set) var errorMessage: String?

    private var client: StalkerAPIClient?
    private var liveTask: Task<Void, Never>?

    public init(client: StalkerAPIClient? = StalkerAPIClient()) {
        self.client = client
    }

    deinit {
        liveTask?.cancel()
    }

    public func refresh() async {
        guard let client else {
            errorMessage = "Waiting for Stalker on your local network"
            return
        }
        do {
            snapshot = try await client.snapshot()
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    public func use(client: StalkerAPIClient) {
        self.client = client
    }

    public func showStatus(_ message: String) {
        errorMessage = message
    }

    public func connectLive() {
        guard let client else {
            isLive = false
            errorMessage = "Waiting for Stalker on your local network"
            return
        }
        liveTask?.cancel()
        liveTask = Task {
            do {
                for try await next in client.snapshots() {
                    snapshot = next
                    isLive = true
                    errorMessage = nil
                }
                isLive = false
            } catch {
                isLive = false
                errorMessage = error.localizedDescription
            }
        }
    }
}

public enum TokenFormat {
    public static func compact(_ value: Int64) -> String {
        let absValue = abs(value)
        let sign = value < 0 ? "-" : ""
        switch absValue {
        case 1_000_000_000...:
            return sign + String(format: "%.1fB", Double(absValue) / 1_000_000_000)
        case 1_000_000...:
            return sign + String(format: "%.1fM", Double(absValue) / 1_000_000)
        case 1_000...:
            return sign + String(format: "%.1fK", Double(absValue) / 1_000)
        default:
            return "\(value)"
        }
    }
}
