import StalkerShared
import SwiftUI

#if canImport(WidgetKit)
import WidgetKit

public struct StalkerComplicationEntry: TimelineEntry {
    public let date: Date
    public let totalTokens: Int64
    public let isLive: Bool
}

public struct StalkerComplicationProvider: TimelineProvider {
    public init() {}

    public func placeholder(in context: Context) -> StalkerComplicationEntry {
        StalkerComplicationEntry(date: Date(), totalTokens: 12000, isLive: true)
    }

    public func getSnapshot(in context: Context, completion: @escaping (StalkerComplicationEntry) -> Void) {
        completion(placeholder(in: context))
    }

    public func getTimeline(in context: Context, completion: @escaping (Timeline<StalkerComplicationEntry>) -> Void) {
        completion(Timeline(entries: [placeholder(in: context)], policy: .after(Date().addingTimeInterval(300))))
    }
}

public struct StalkerComplicationView: View {
    public let entry: StalkerComplicationEntry

    public init(entry: StalkerComplicationEntry) {
        self.entry = entry
    }

    public var body: some View {
        VStack(spacing: 2) {
            Image(systemName: entry.isLive ? "bolt.fill" : "icloud")
            Text(TokenFormat.compact(entry.totalTokens))
                .font(.system(.caption, design: .rounded).weight(.semibold))
        }
        .containerBackground(.background, for: .widget)
    }
}
#endif
