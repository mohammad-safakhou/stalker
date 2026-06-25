import Foundation

public struct SyncSnapshot: Codable, Equatable, Sendable {
    public var device: SyncDevice
    public var generatedAt: Date
    public var cursor: String
    public var totals: TokenTotals
    public var live: LiveStats
    public var hourly: [StatsBucket]
    public var daily: [StatsBucket]
}

public struct SyncDevice: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var platform: String
    public var lastSeen: Date
}

public struct LiveStats: Codable, Equatable, Sendable {
    public var windowSeconds: Int
    public var inputTokens: Int64
    public var outputTokens: Int64
    public var requests: Int64
    public var errors: Int64
}

public struct StatsBucket: Codable, Equatable, Sendable, Identifiable {
    public var key: String
    public var granularity: String
    public var start: Date
    public var inputTokens: Int64
    public var outputTokens: Int64
    public var requests: Int64
    public var errors: Int64
    public var streams: Int64

    public var id: String { key }
    public var totalTokens: Int64 { inputTokens + outputTokens }
}

public struct TokenTotals: Codable, Equatable, Sendable {
    public var inputTokens: Int64
    public var outputTokens: Int64
    public var top: TokenBurns

    public var totalTokens: Int64 { inputTokens + outputTokens }
}

public struct TokenBurns: Codable, Equatable, Sendable {
    public var input: [TokenBurn]
    public var output: [TokenBurn]
}

public struct TokenBurn: Codable, Equatable, Sendable, Identifiable {
    public var side: String
    public var token: String
    public var tokenHash: String
    public var occurrences: Int64

    public var id: String { side + ":" + tokenHash }
}

public enum StalkerCoders {
    public static func decoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        decoder.dateDecodingStrategy = .custom { decoder in
            let value = try decoder.singleValueContainer().decode(String.self)
            if let date = makeISO8601Formatter(fractional: true).date(from: value) ?? makeISO8601Formatter(fractional: false).date(from: value) {
                return date
            }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Invalid date \(value)"))
        }
        return decoder
    }

    public static func encoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.keyEncodingStrategy = .convertToSnakeCase
        encoder.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(makeISO8601Formatter(fractional: true).string(from: date))
        }
        return encoder
    }

    private static func makeISO8601Formatter(fractional: Bool) -> ISO8601DateFormatter {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = fractional ? [.withInternetDateTime, .withFractionalSeconds] : [.withInternetDateTime]
        return formatter
    }
}
