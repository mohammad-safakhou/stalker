import XCTest
@testable import StalkerShared

final class ModelTests: XCTestCase {
    func testDecodesSyncSnapshotFromGoAPIShape() throws {
        let raw = """
        {
          "device": {"id":"device-1","name":"Mac","platform":"darwin","last_seen":"2026-06-25T10:00:00Z"},
          "generated_at":"2026-06-25T10:00:01Z",
          "cursor":"abc:2",
          "totals":{"input_tokens":10,"output_tokens":20,"top":{"input":[],"output":[]}},
          "live":{"window_seconds":60,"input_tokens":1,"output_tokens":2,"requests":1,"errors":0},
          "hourly":[{"key":"device-1:hourly:2026-06-25T10:00:00Z","granularity":"hourly","start":"2026-06-25T10:00:00Z","input_tokens":10,"output_tokens":20,"requests":1,"errors":0,"streams":1}],
          "daily":[]
        }
        """.data(using: .utf8)!

        let snapshot = try StalkerCoders.decoder().decode(SyncSnapshot.self, from: raw)

        XCTAssertEqual(snapshot.device.id, "device-1")
        XCTAssertEqual(snapshot.totals.totalTokens, 30)
        XCTAssertEqual(snapshot.hourly.first?.totalTokens, 30)
    }
}
