import Foundation

#if canImport(CloudKit)
import CloudKit

public actor CloudKitSyncEngine {
    public let container: CKContainer
    public let database: CKDatabase

    public init(containerIdentifier: String = "iCloud.com.mohammad-safakhou.stalker") {
        container = CKContainer(identifier: containerIdentifier)
        database = container.privateCloudDatabase
    }

    public func push(snapshot: SyncSnapshot) async throws {
        var records: [CKRecord] = []
        records.append(deviceRecord(snapshot.device))
        records.append(liveSnapshotRecord(snapshot))
        records.append(contentsOf: snapshot.hourly.map { bucketRecord($0, type: "StatsBucketHourly", deviceID: snapshot.device.id) })
        records.append(contentsOf: snapshot.daily.map { bucketRecord($0, type: "StatsBucketDaily", deviceID: snapshot.device.id) })
        records.append(contentsOf: tokenRecords(snapshot.totals.top.input + snapshot.totals.top.output))

        let operation = CKModifyRecordsOperation(recordsToSave: records, recordIDsToDelete: nil)
        operation.savePolicy = .changedKeys
        operation.isAtomic = false
        try await database.modifyRecords(operation)
    }

    private func deviceRecord(_ device: SyncDevice) -> CKRecord {
        let record = CKRecord(recordType: "Device", recordID: CKRecord.ID(recordName: device.id))
        record["name"] = device.name as CKRecordValue
        record["platform"] = device.platform as CKRecordValue
        record["lastSeen"] = device.lastSeen as CKRecordValue
        return record
    }

    private func liveSnapshotRecord(_ snapshot: SyncSnapshot) -> CKRecord {
        let record = CKRecord(recordType: "LiveSnapshot", recordID: CKRecord.ID(recordName: snapshot.device.id))
        record["deviceID"] = snapshot.device.id as CKRecordValue
        record["inputTokens"] = snapshot.totals.inputTokens as CKRecordValue
        record["outputTokens"] = snapshot.totals.outputTokens as CKRecordValue
        record["liveInputTokens"] = snapshot.live.inputTokens as CKRecordValue
        record["liveOutputTokens"] = snapshot.live.outputTokens as CKRecordValue
        record["requests"] = snapshot.live.requests as CKRecordValue
        record["errors"] = snapshot.live.errors as CKRecordValue
        record["cursor"] = snapshot.cursor as CKRecordValue
        record["updatedAt"] = snapshot.generatedAt as CKRecordValue
        return record
    }

    private func bucketRecord(_ bucket: StatsBucket, type: String, deviceID: String) -> CKRecord {
        let record = CKRecord(recordType: type, recordID: CKRecord.ID(recordName: bucket.key))
        record["deviceID"] = deviceID as CKRecordValue
        record["start"] = bucket.start as CKRecordValue
        record["inputTokens"] = bucket.inputTokens as CKRecordValue
        record["outputTokens"] = bucket.outputTokens as CKRecordValue
        record["requests"] = bucket.requests as CKRecordValue
        record["errors"] = bucket.errors as CKRecordValue
        record["streams"] = bucket.streams as CKRecordValue
        return record
    }

    private func tokenRecords(_ tokens: [TokenBurn]) -> [CKRecord] {
        tokens.map { token in
            let id = token.side + ":" + token.tokenHash
            let record = CKRecord(recordType: "TokenTotal", recordID: CKRecord.ID(recordName: id))
            record["side"] = token.side as CKRecordValue
            record["token"] = token.token as CKRecordValue
            record["tokenHash"] = token.tokenHash as CKRecordValue
            record["occurrences"] = token.occurrences as CKRecordValue
            return record
        }
    }
}

private extension CKDatabase {
    func modifyRecords(_ operation: CKModifyRecordsOperation) async throws {
        try await withCheckedThrowingContinuation { continuation in
            operation.modifyRecordsResultBlock = { result in
                continuation.resume(with: result)
            }
            add(operation)
        }
    }
}
#else
public actor CloudKitSyncEngine {
    public init(containerIdentifier: String = "iCloud.com.mohammad-safakhou.stalker") {}
    public func push(snapshot: SyncSnapshot) async throws {}
}
#endif
