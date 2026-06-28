import XCTest
@testable import StalkerShared

final class ModelTests: XCTestCase {
    func testDecodesSyncSnapshotFromGoAPIShape() throws {
        let raw = """
        {
          "device": {"id":"device-1","name":"Mac","platform":"darwin","last_seen":"2026-06-25T10:00:00Z"},
          "generated_at":"2026-06-25T10:00:01Z",
          "cursor":"abc:2",
          "totals":{"input_tokens":10,"output_tokens":20,"input_chars":100,"output_chars":200,"input_words":5,"output_words":10,"top":{"input":[],"output":[]},"top_words":{"input":[],"output":[]},"top_chars":{"input":[],"output":[]}},
          "live":{"window_seconds":60,"input_tokens":1,"output_tokens":2,"input_chars":10,"output_chars":20,"requests":1,"errors":0,"tokens_per_second":0.05,"characters_per_second":0.5,"requests_per_minute":1},
          "hourly":[{"key":"device-1:hourly:2026-06-25T10:00:00Z","granularity":"hourly","start":"2026-06-25T10:00:00Z","input_tokens":10,"output_tokens":20,"input_chars":100,"output_chars":200,"requests":1,"errors":0,"streams":1}],
          "daily":[]
        }
        """.data(using: .utf8)!

        let snapshot = try StalkerCoders.decoder().decode(SyncSnapshot.self, from: raw)

        XCTAssertEqual(snapshot.device.id, "device-1")
        XCTAssertEqual(snapshot.totals.totalTokens, 30)
        XCTAssertEqual(snapshot.hourly.first?.totalTokens, 30)
    }

    func testDecodesSyncHealthFromGoAPIShape() throws {
        let raw = """
        {
          "device": {"id":"device-1","name":"Mac","platform":"darwin","last_seen":"2026-06-25T10:00:00Z"},
          "generated_at":"2026-06-25T10:00:01Z",
          "status":"ok",
          "pending_token_jobs":2,
          "token_queue_size":1024
        }
        """.data(using: .utf8)!

        let health = try StalkerCoders.decoder().decode(SyncHealth.self, from: raw)

        XCTAssertEqual(health.device.id, "device-1")
        XCTAssertEqual(health.status, "ok")
        XCTAssertEqual(health.pendingTokenJobs, 2)
        XCTAssertEqual(health.tokenQueueSize, 1024)
    }
}
