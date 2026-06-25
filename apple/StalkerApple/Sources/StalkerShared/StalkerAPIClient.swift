import Foundation

public struct StalkerAPIClient: Sendable {
    public var baseURL: URL
    public var session: URLSession

    public init(baseURL: URL = URL(string: "http://127.0.0.1:18080")!, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
    }

    public init(discovered service: DiscoveredStalker, session: URLSession = .shared) {
        self.init(baseURL: service.baseURL, session: session)
    }

    public func snapshot() async throws -> SyncSnapshot {
        let url = baseURL.appending(path: "/api/v1/sync/snapshot")
        let (data, response) = try await session.data(from: url)
        try validate(response)
        return try StalkerCoders.decoder().decode(SyncSnapshot.self, from: data)
    }

    public func snapshots() -> AsyncThrowingStream<SyncSnapshot, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let url = baseURL.appending(path: "/api/v1/sync/stream")
                    let (bytes, response) = try await session.bytes(from: url)
                    try validate(response)
                    for try await line in bytes.lines {
                        if Task.isCancelled { break }
                        guard line.hasPrefix("data:") else { continue }
                        let payload = line.dropFirst(5).trimmingCharacters(in: .whitespaces)
                        guard let data = payload.data(using: .utf8) else { continue }
                        continuation.yield(try StalkerCoders.decoder().decode(SyncSnapshot.self, from: data))
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func validate(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw URLError(.badServerResponse)
        }
    }
}
